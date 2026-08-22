package ddl

import (
	"errors"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// findingLines renders a finding list the way cmd/jjf does, one per line, so
// that a golden pins the exact wording of every message.
func findingLines(findings []check.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	return b.String()
}

// TestCheckReportsEveryPrecondition pins the wording of every refusal, in the
// order they come out: everything internal/check reports first, then the
// PostgreSQL-specific findings in document order.
func TestCheckReportsEveryPrecondition(t *testing.T) {
	checkGolden(t, "refused.txt", findingLines(Check(loadDoc(t, "refused.json"))))
}

// TestCheckPassesThroughInternalCheck pins the concatenation contract rather
// than any message: whatever internal/check says comes out first, element for
// element, and this package only ever appends.
func TestCheckPassesThroughInternalCheck(t *testing.T) {
	doc := loadDoc(t, "refused.json")

	want := check.Document(doc)
	got := Check(doc)
	if len(got) < len(want) {
		t.Fatalf("Check returned %d finding(s), fewer than internal/check's %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %v, want internal/check's %v", i, got[i], want[i])
		}
	}
	if len(got) == len(want) {
		t.Error("Check added no PostgreSQL findings for the fixture that carries one of each")
	}
}

// TestForeignKeyNamesMayRepeatAcrossTables pins the one place the schema-wide
// namespace stops. Foreign key names live in pg_constraint, which is unique per
// TABLE, so PostgreSQL accepts two tables each carrying a constraint called
// fk_parent. Without this test a later tidying of the walk would quietly turn a
// legal document into a refusal.
func TestForeignKeyNamesMayRepeatAcrossTables(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		a.ForeignKeys = []model.ForeignKey{{
			Name:       "fk_parent",
			Columns:    []string{"id"},
			References: model.Reference{Table: "b", Columns: []string{"id"}},
		}}
		b.ForeignKeys = []model.ForeignKey{{
			Name:       "fk_parent",
			Columns:    []string{"id"},
			References: model.Reference{Table: "a", Columns: []string{"id"}},
		}}
	})
	if got := Check(doc); len(got) != 0 {
		t.Errorf("Check reported %v, want nothing: a foreign key name is per table in PostgreSQL", got)
	}
}

func TestDuplicateTableNameIsReported(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) { b.Name = a.Name })

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "second table this document calls") {
		t.Errorf("finding = %v, want it to name the duplicate table", got[0])
	}
}

// TestIndexNameCollidesWithATableName proves the namespace really is one
// namespace rather than a list per kind: an index may not take a name a table
// already has.
func TestIndexNameCollidesWithATableName(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		b.Indexes = []model.Index{{Name: a.Name, Columns: []string{"id"}}}
	})

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "already used by table a") {
		t.Errorf("finding = %v, want it to name the table that holds the name", got[0])
	}
}

func TestIndexNamesMayNotRepeatAcrossTables(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		a.Indexes = []model.Index{{Name: "ix_id", Columns: []string{"id"}}}
		b.Indexes = []model.Index{{Name: "ix_id", Columns: []string{"id"}}}
	})
	if got := Check(doc); len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
}

// TestUnnamedConstraintsDoNotCollide covers the case an empty name would break
// if the walk claimed one: the schema leaves a primary key and a unique key
// nameable, and PostgreSQL invents a name for those that have none.
func TestUnnamedConstraintsDoNotCollide(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		for _, t := range []*model.Table{a, b} {
			t.PrimaryKey = &model.PrimaryKey{Columns: []string{"id"}}
			t.UniqueKeys = []model.UniqueKey{{Columns: []string{"id"}}}
		}
	})
	if got := Check(doc); len(got) != 0 {
		t.Errorf("Check reported %v, want nothing", got)
	}
}

func TestIdentityColumnPreconditions(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *model.Column)
		want    string
		wantLen int
	}{
		{
			name:    "an identity column that also declares a default",
			mutate:  func(c *model.Column) { c.AutoIncrement, c.Default = true, strp("1") },
			want:    "also declares a default",
			wantLen: 1,
		},
		{
			name:    "an identity column declared nullable",
			mutate:  func(c *model.Column) { c.AutoIncrement, c.Nullable = true, true },
			want:    "declared nullable",
			wantLen: 1,
		},
		{
			// Both at once is one authoring mistake and gets one finding, the
			// rule internal/check's checkColumnDefault already follows.
			name:    "both at once",
			mutate:  func(c *model.Column) { c.AutoIncrement, c.Nullable, c.Default = true, true, strp("1") },
			want:    "also declares a default",
			wantLen: 1,
		},
		{
			name:    "an ordinary identity column",
			mutate:  func(c *model.Column) { c.AutoIncrement = true },
			wantLen: 0,
		},
		{
			// A default without an identity is ordinary and is internal/check's
			// business alone.
			name:    "a default without an identity",
			mutate:  func(c *model.Column) { c.Default = strp("1") },
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := twoTablesSharing(t, func(a, _ *model.Table) { tt.mutate(&a.Columns[1]) })
			got := Check(doc)
			if len(got) != tt.wantLen {
				t.Fatalf("Check reported %d finding(s), want %d: %v", len(got), tt.wantLen, got)
			}
			if tt.wantLen == 0 {
				return
			}
			if !strings.Contains(got[0].Message, tt.want) {
				t.Errorf("finding = %v, want it to mention %q", got[0], tt.want)
			}
			if got[0].Where != "column n on table a" {
				t.Errorf("finding is about %q, want the column", got[0].Where)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The dbms guard
// ---------------------------------------------------------------------------

func TestAcceptRefusesAMissingDBMS(t *testing.T) {
	doc := loadDoc(t, "minimal.json")
	doc.Database.DBMS = ""

	err := Accept(doc)
	if err == nil {
		t.Fatal("Accept accepted a document that names no dbms")
	}
	if want := `add "dbms": "PostgreSQL"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to carry %q", err, want)
	}
	if got := exitcode.Of(err); got != exitcode.InvalidInput {
		t.Errorf("exit code = %d, want %d", got, exitcode.InvalidInput)
	}
}

func TestAcceptRefusesANonPostgreSQLDBMS(t *testing.T) {
	doc := loadDoc(t, "minimal.json")
	doc.Database.DBMS = model.DBMSMySQL

	err := Accept(doc)
	if err == nil {
		t.Fatal("Accept accepted a MySQL document")
	}
	if want := `ddl export supports PostgreSQL only; this document names "MySQL"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to carry %q", err, want)
	}
	if got := exitcode.Of(err); got != exitcode.InvalidInput {
		t.Errorf("exit code = %d, want %d", got, exitcode.InvalidInput)
	}
}

// TestAcceptChecksDBMSBeforeFindings pins the diagnosis order: a MySQL document
// is told its target is unsupported, not lectured about PostgreSQL's index
// namespace, because the remedy is a different one.
func TestAcceptChecksDBMSBeforeFindings(t *testing.T) {
	doc := loadDoc(t, "refused.json")
	doc.Database.DBMS = model.DBMSMySQL

	err := Accept(doc)
	var re *RefusedError
	if errors.As(err, &re) {
		t.Fatalf("Accept reported findings for a MySQL document: %v", err)
	}
	if !strings.Contains(err.Error(), "supports PostgreSQL only") {
		t.Errorf("error = %q, want the dbms message", err)
	}
}

func TestAcceptCarriesEveryFinding(t *testing.T) {
	doc := loadDoc(t, "refused.json")

	err := Accept(doc)
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("Accept returned %v, want a *RefusedError", err)
	}
	if want := Check(doc); len(re.Findings) != len(want) {
		t.Errorf("the error carries %d finding(s), want %d", len(re.Findings), len(want))
	}
	if got := exitcode.Of(err); got != exitcode.InvalidInput {
		t.Errorf("exit code = %d, want %d", got, exitcode.InvalidInput)
	}
	if !strings.Contains(err.Error(), "problem(s) prevent PostgreSQL DDL generation") {
		t.Errorf("summary = %q, want the one-line refusal", err)
	}
}

// ---------------------------------------------------------------------------
// Totality and determinism
// ---------------------------------------------------------------------------

// TestCheckIsDeterministic runs each fixture twice. The namespace walk keeps a
// map, and this is what would catch it leaking into the order of the findings.
func TestCheckIsDeterministic(t *testing.T) {
	for _, name := range append(fixtures, "refused.json") {
		t.Run(name, func(t *testing.T) {
			doc := loadDoc(t, name)
			first, second := Check(doc), Check(doc)
			if len(first) != len(second) {
				t.Fatalf("two runs reported %d and %d finding(s)", len(first), len(second))
			}
			for i := range first {
				if first[i] != second[i] {
					t.Errorf("finding %d differs between runs: %v vs %v", i, first[i], second[i])
				}
			}
		})
	}
}

// TestCheckIsTotal holds this package to the promise internal/check makes: a
// document that never passed JSON Schema validation may not panic here.
func TestCheckIsTotal(t *testing.T) {
	docs := map[string]*model.Document{
		"empty":                   {},
		"a table with no columns": {Tables: []model.Table{{Name: "a"}}},
		"a nameless table":        {Tables: []model.Table{{Columns: []model.Column{{Name: ""}}}}},
		"empty column lists": {Tables: []model.Table{{
			Name:        "a",
			ForeignKeys: []model.ForeignKey{{Name: "fk", References: model.Reference{Table: "a"}}},
			PrimaryKey:  &model.PrimaryKey{},
			UniqueKeys:  []model.UniqueKey{{}},
			Indexes:     []model.Index{{}},
		}}},
		"an identity column with nothing else": {Tables: []model.Table{{
			Name:    "a",
			Columns: []model.Column{{AutoIncrement: true, Nullable: true}},
		}}},
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) { Check(doc) })
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// twoTablesSharing builds the smallest consistent two-table document and hands
// both tables to mutate, so that a test states only the mistake it is about.
func twoTablesSharing(t *testing.T, mutate func(a, b *model.Table)) *model.Document {
	t.Helper()

	table := func(name string) model.Table {
		return model.Table{
			Name:        name,
			LogicalName: name,
			Columns: []model.Column{
				{Name: "id", LogicalName: "id", Type: "BIGINT"},
				// A second column outside the primary key, so that a test
				// about a column can say only what it is about: a nullable
				// id would trip internal/check's C5 as well.
				{Name: "n", LogicalName: "n", Type: "INTEGER"},
			},
			PrimaryKey: &model.PrimaryKey{Name: "pk_" + name, Columns: []string{"id"}},
		}
	}
	doc := &model.Document{
		FormatVersion: model.CurrentFormatVersion,
		Database:      model.Database{Name: "t", DBMS: model.DBMSPostgreSQL},
		Tables:        []model.Table{table("a"), table("b")},
	}
	mutate(&doc.Tables[0], &doc.Tables[1])
	return doc
}

// strp returns a pointer to s, for model.Column's optional default.
func strp(s string) *string { return &s }
