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

// TestATableNameCollidesWithAnIndex is the mirror of the test above and reaches
// the other arm of the same finding. Document order decides which: the walk
// claims each object as it meets it, so declaring the INDEX first - on table a,
// which comes first - makes the table the second claimant and the index the
// prior one. Reverse the two and TestIndexNameCollidesWithATableName's arm
// fires instead, with a message naming a table where this one names an index.
func TestATableNameCollidesWithAnIndex(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		a.Indexes = []model.Index{{Name: "b", Columns: []string{"n"}}}
	})

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `is called "b", a name already used by index b on table a`) {
		t.Errorf("finding = %v, want it to name the index that holds the name", got[0])
	}
}

// TestAPrimaryKeyNameCollidesWithAnEarlierName covers the primary key's arm of
// the same rule. PostgreSQL creates an index behind every PRIMARY KEY, in the
// same namespace as the tables, so a key named after an existing table is a
// refusal rather than a curiosity - and the message has to say what took the
// name, because that is what tells the author which of the two to rename.
func TestAPrimaryKeyNameCollidesWithAnEarlierName(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		b.PrimaryKey = &model.PrimaryKey{Name: a.Name, Columns: []string{"id"}}
	})

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `declares primary key "a", a name already used by table a`) {
		t.Errorf("finding = %v, want it to name the table that holds the name", got[0])
	}
}

// TestAUniqueKeyNameCollidesWithAnEarlierName covers the last of the three
// arms. It collides with an INDEX rather than with a table on purpose: the
// prior claimant is rendered into the message, so the two renderings - a table
// and an index - are both worth seeing once, and this is the cheaper of the two
// places to see the index form.
func TestAUniqueKeyNameCollidesWithAnEarlierName(t *testing.T) {
	doc := twoTablesSharing(t, func(a, b *model.Table) {
		a.Indexes = []model.Index{{Name: "shared", Columns: []string{"n"}}}
		b.UniqueKeys = []model.UniqueKey{{Name: "shared", Columns: []string{"n"}}}
	})

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `declares unique key "shared", a name already used by index shared on table a`) {
		t.Errorf("finding = %v, want it to name the index that holds the name", got[0])
	}
}

// TestOneNameCollisionIsReportedOnce holds the rule both dialects' walks keep a
// set for: three objects sharing a name is one mistake to fix, not two
// findings. Without it the author would rename the second and be told about the
// third, and clearing one refusal would take as many runs as there are
// duplicates.
//
// Both dialects are here because the two walks keep their own sets, in two
// files, and neither would notice the other losing its.
func TestOneNameCollisionIsReportedOnce(t *testing.T) {
	t.Run("three tables of one name", func(t *testing.T) {
		doc := twoTablesSharing(t, func(a, b *model.Table) { b.Name = a.Name })
		third := doc.Tables[1]
		// A distinct key name, so that the only thing repeated is the table
		// name.
		third.PrimaryKey = &model.PrimaryKey{Name: "pk_c", Columns: []string{"id"}}
		doc.Tables = append(doc.Tables, third)

		if got := Check(doc); len(got) != 1 {
			t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
		}
	})

	t.Run("three foreign keys of one name under MySQL", func(t *testing.T) {
		doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, b *model.Table) {
			for _, tbl := range []*model.Table{a, b} {
				tbl.ForeignKeys = []model.ForeignKey{{
					Name:       "fk_parent",
					Columns:    []string{"id"},
					References: model.Reference{Table: "a", Columns: []string{"id"}},
				}}
			}
		})
		third := doc.Tables[1]
		third.Name = "c"
		third.PrimaryKey = &model.PrimaryKey{Name: "pk_c", Columns: []string{"id"}}
		doc.Tables = append(doc.Tables, third)

		if got := Check(doc); len(got) != 1 {
			t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
		}
	})
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

func TestAcceptRefusesAnAbsentDBMS(t *testing.T) {
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

// TestAcceptRefusesAnUnsupportedDBMS names MariaDB rather than any dbms value
// a later commit might start writing, so that the case stays a refusal for as
// long as the enum has a value jjf does not write.
func TestAcceptRefusesAnUnsupportedDBMS(t *testing.T) {
	doc := loadDoc(t, "minimal.json")
	doc.Database.DBMS = model.DBMSMariaDB

	err := Accept(doc)
	if err == nil {
		t.Fatal("Accept accepted a MariaDB document")
	}
	if want := `ddl export supports ` + dialectNames() + `; this document names "MariaDB"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to carry %q", err, want)
	}
	if got := exitcode.Of(err); got != exitcode.InvalidInput {
		t.Errorf("exit code = %d, want %d", got, exitcode.InvalidInput)
	}
}

// TestAcceptChecksDBMSBeforeFindings pins the diagnosis order: a document
// naming a target jjf does not write is told exactly that, not lectured about
// PostgreSQL's index namespace, because the remedy is a different one - change
// the target or change the tool, not the contents of the document.
func TestAcceptChecksDBMSBeforeFindings(t *testing.T) {
	doc := loadDoc(t, "refused.json")
	doc.Database.DBMS = model.DBMSMariaDB

	err := Accept(doc)
	var re *RefusedError
	if errors.As(err, &re) {
		t.Fatalf("Accept reported findings for a MariaDB document: %v", err)
	}
	if !strings.Contains(err.Error(), "ddl export supports ") {
		t.Errorf("error = %q, want the dbms message", err)
	}
}

// TestCheckAddsNothingForADBMSWithNoDialect is the other half of the same
// rule, on the pure function rather than on Accept: asked what is wrong with a
// document whose target jjf has no dialect for, Check answers with what is
// wrong with the DOCUMENT and adds not one word about any database.
func TestCheckAddsNothingForADBMSWithNoDialect(t *testing.T) {
	doc := loadDoc(t, "refused.json")
	doc.Database.DBMS = model.DBMSSQLite

	want := check.Document(doc)
	got := Check(doc)
	if len(got) != len(want) {
		t.Fatalf("Check reported %d finding(s), want internal/check's %d alone: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d = %v, want internal/check's %v", i, got[i], want[i])
		}
	}
}

// TestRefusedErrorNamesItsDialect pins the one fact the error type gained when
// this package stopped being one dialect's: a refusal says which database it
// was refused for, and cmd/jjf prints that sentence without composing it.
func TestRefusedErrorNamesItsDialect(t *testing.T) {
	err := Accept(loadDoc(t, "refused.json"))
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("Accept returned %v, want a *RefusedError", err)
	}
	if re.Dialect != "PostgreSQL" {
		t.Errorf("the refusal names the dialect %q, want PostgreSQL", re.Dialect)
	}
	if want := "PostgreSQL DDL generation"; !strings.Contains(re.Error(), want) {
		t.Errorf("summary = %q, want it to carry %q", re.Error(), want)
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
	for _, name := range append(allFixtures(), "refused.json", "mysql/refused.json") {
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

// twoTablesSharing builds the smallest consistent two-table PostgreSQL document
// and hands both tables to mutate, so that a test states only the mistake it is
// about.
func twoTablesSharing(t *testing.T, mutate func(a, b *model.Table)) *model.Document {
	t.Helper()
	return twoTablesSharingFor(t, model.DBMSPostgreSQL, mutate)
}

// twoTablesSharingFor is the same document under a named target, for the tests
// that assert an inversion: the same mistake is a finding under one dialect and
// nothing under the other, and a test that could only build one of the two
// would have to state half its claim in prose.
func twoTablesSharingFor(t *testing.T, dbms model.DBMS, mutate func(a, b *model.Table)) *model.Document {
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
		Database:      model.Database{Name: "t", DBMS: dbms},
		Tables:        []model.Table{table("a"), table("b")},
	}
	mutate(&doc.Tables[0], &doc.Tables[1])
	return doc
}

// strp returns a pointer to s, for model.Column's optional default.
func strp(s string) *string { return &s }

// ---------------------------------------------------------------------------
// The MySQL preconditions
// ---------------------------------------------------------------------------

// TestMySQLCheckReportsEveryPrecondition pins the wording of every MySQL
// refusal, in the order they come out: everything internal/check reports first,
// then the MySQL-specific findings in document order.
func TestMySQLCheckReportsEveryPrecondition(t *testing.T) {
	checkGolden(t, "mysql/refused.txt", findingLines(Check(loadDoc(t, "mysql/refused.json"))))
}

// TestMySQLForeignKeyNamesAreSchemaWide asserts the inversion in both
// directions at once, so that neither half can be changed alone. InnoDB keeps
// foreign key names in a per-database namespace and answers a collision with
// "Duplicate foreign key constraint name"; PostgreSQL keeps them in
// pg_constraint, which is unique per table, and accepts both.
func TestMySQLForeignKeyNamesAreSchemaWide(t *testing.T) {
	share := func(a, b *model.Table) {
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
	}

	got := Check(twoTablesSharingFor(t, model.DBMSMySQL, share))
	if len(got) != 1 {
		t.Fatalf("MySQL reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "one namespace per schema") {
		t.Errorf("finding = %v, want it to say why MySQL refuses", got[0])
	}
	if got := Check(twoTablesSharingFor(t, model.DBMSPostgreSQL, share)); len(got) != 0 {
		t.Errorf("PostgreSQL reported %v for the same document, want nothing", got)
	}
}

// TestMySQLIndexNamesMayRepeatAcrossTables is the other half of the same
// inversion. An index name lives in the table in MySQL and in the schema in
// PostgreSQL, so the document that is legal for one is illegal for the other.
func TestMySQLIndexNamesMayRepeatAcrossTables(t *testing.T) {
	share := func(a, b *model.Table) {
		a.Indexes = []model.Index{{Name: "ix_created", Columns: []string{"id"}}}
		b.Indexes = []model.Index{{Name: "ix_created", Columns: []string{"id"}}}
	}

	if got := Check(twoTablesSharingFor(t, model.DBMSMySQL, share)); len(got) != 0 {
		t.Errorf("MySQL reported %v, want nothing: an index name is per table there", got)
	}
	if got := Check(twoTablesSharingFor(t, model.DBMSPostgreSQL, share)); len(got) != 1 {
		t.Errorf("PostgreSQL reported %d finding(s) for the same document, want 1: %v", len(got), got)
	}
}

// TestMySQLTableNamesAreSchemaWide covers the one namespace rule the two
// dialects agree on, so that the agreement is pinned as deliberate rather than
// left to look like a gap in the MySQL walk.
func TestMySQLTableNamesAreSchemaWide(t *testing.T) {
	doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, b *model.Table) { b.Name = a.Name })

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "second table this document calls") {
		t.Errorf("finding = %v, want it to name the duplicate table", got[0])
	}
}

// TestMySQLAutoIncrementPreconditions covers all four rules and the shapes that
// must NOT produce a finding. Each was run against a live MySQL 8.0 first: the
// first two are ERROR 1075, the third is ERROR 1067, and the fourth is the
// silent one, where the server takes the statement and stores the column NOT
// NULL.
func TestMySQLAutoIncrementPreconditions(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *model.Table)
		want    string
		wantLen int
	}{
		{
			name: "the leading column of the primary key",
			mutate: func(tb *model.Table) {
				tb.Columns[0].AutoIncrement = true
			},
			wantLen: 0,
		},
		{
			name: "the leading column of a composite primary key",
			mutate: func(tb *model.Table) {
				tb.Columns[0].AutoIncrement = true
				tb.PrimaryKey.Columns = []string{"id", "n"}
			},
			wantLen: 0,
		},
		{
			name: "the leading column of a unique key",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement = true
				tb.UniqueKeys = []model.UniqueKey{{Name: "uq_n", Columns: []string{"n", "id"}}}
			},
			wantLen: 0,
		},
		{
			name: "the second column of a key, which leads nothing",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement = true
				tb.PrimaryKey.Columns = []string{"id", "n"}
			},
			want:    "leads no key",
			wantLen: 1,
		},
		{
			name: "a column no key names at all",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement = true
			},
			want:    "leads no key",
			wantLen: 1,
		},
		{
			// MySQL itself would take this in one statement, and the generated
			// script cannot, because the index is created a phase after the
			// table. The message says so, and this case is why.
			name: "a column only an index leads",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement = true
				tb.Indexes = []model.Index{{Name: "ix_n", Columns: []string{"n"}}}
			},
			want:    "an entry in indexes cannot serve",
			wantLen: 1,
		},
		{
			name: "a second AUTO_INCREMENT column",
			mutate: func(tb *model.Table) {
				tb.Columns[0].AutoIncrement = true
				tb.Columns[1].AutoIncrement = true
			},
			want:    "one AUTO_INCREMENT column per table",
			wantLen: 1,
		},
		{
			name: "one that also declares a default",
			mutate: func(tb *model.Table) {
				tb.Columns[0].AutoIncrement, tb.Columns[0].Default = true, strp("1")
			},
			want:    "refuses a default on an AUTO_INCREMENT column",
			wantLen: 1,
		},
		{
			name: "one declared nullable",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement, tb.Columns[1].Nullable = true, true
				tb.UniqueKeys = []model.UniqueKey{{Name: "uq_n", Columns: []string{"n"}}}
			},
			want:    "makes an AUTO_INCREMENT column NOT NULL",
			wantLen: 1,
		},
		{
			// Two rules broken by one column is one authoring mistake, the rule
			// internal/check's checkColumnDefault already follows.
			name: "nullable and leading no key at once",
			mutate: func(tb *model.Table) {
				tb.Columns[1].AutoIncrement, tb.Columns[1].Nullable = true, true
			},
			want:    "makes an AUTO_INCREMENT column NOT NULL",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, _ *model.Table) { tt.mutate(a) })
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
		})
	}
}

// TestMySQLRefusesAKeyOverATextColumn walks the whole TEXT, BLOB and JSON
// family through all three kinds of key. MySQL answers the first two with ERROR
// 1170 and JSON with ERROR 3152, and neither has a remedy the document format
// can express - a prefix length and a generated column both need a field the
// schema does not have.
func TestMySQLRefusesAKeyOverATextColumn(t *testing.T) {
	// keys are the three places a column may be named, each of which reaches a
	// different walk in myKeyFindings.
	keys := []struct {
		name   string
		attach func(tb *model.Table)
	}{
		{"a primary key", func(tb *model.Table) { tb.PrimaryKey = &model.PrimaryKey{Columns: []string{"n"}} }},
		{"a unique key", func(tb *model.Table) {
			tb.PrimaryKey = nil
			tb.UniqueKeys = []model.UniqueKey{{Name: "uq_n", Columns: []string{"n"}}}
		}},
		{"an index", func(tb *model.Table) {
			tb.PrimaryKey = nil
			tb.Indexes = []model.Index{{Name: "ix_n", Columns: []string{"n"}}}
		}},
	}
	types := []struct {
		name    string
		refused bool
	}{
		{"TEXT", true},
		{"TINYTEXT", true},
		{"MEDIUMTEXT", true},
		{"LONGTEXT", true},
		{"BLOB", true},
		{"TINYBLOB", true},
		{"MEDIUMBLOB", true},
		{"LONGBLOB", true},
		{"JSON", true},
		// Written in the case a hand-authored document is likely to use, to
		// pin that the comparison folds ASCII case and nothing else.
		{"longtext", true},
		{"VARCHAR", false},
		{"BIGINT", false},
	}

	for _, key := range keys {
		for _, ty := range types {
			t.Run(key.name+" over "+ty.name, func(t *testing.T) {
				doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, _ *model.Table) {
					a.Columns[1].Type = ty.name
					a.Columns[1].Length = intp(64)
					key.attach(a)
				})
				got := Check(doc)
				if !ty.refused {
					if len(got) != 0 {
						t.Fatalf("Check reported %v for a %s column, want nothing", got, ty.name)
					}
					return
				}
				if len(got) != 1 {
					t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
				}
				if !strings.Contains(got[0].Message, ty.name) {
					t.Errorf("finding = %v, want it to name the type", got[0])
				}
				if got[0].Where != "table a" {
					t.Errorf("finding is about %q, want the table that declares the key", got[0].Where)
				}
			})
		}
	}
}

// TestMySQLReportsOneFindingPerKey holds the walk to one finding for a
// composite key over two impossible columns: that is one key to redesign, not
// two mistakes to fix.
func TestMySQLReportsOneFindingPerKey(t *testing.T) {
	doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, _ *model.Table) {
		a.Columns[0].Type = "TEXT"
		a.Columns[1].Type = "BLOB"
		a.PrimaryKey = &model.PrimaryKey{Columns: []string{"id", "n"}}
	})
	if got := Check(doc); len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
}

// TestMySQLRefusesALengthlessVarchar covers R1's half of M8: a bare VARCHAR is
// ERROR 1064 rather than a column of some default width, and mysqldump always
// writes the length, so the refusal is unreachable from an imported document
// and cannot break the round trip.
func TestMySQLRefusesALengthlessVarchar(t *testing.T) {
	tests := []struct {
		name    string
		ty      string
		length  *int
		refused bool
	}{
		{"VARCHAR with no length", "VARCHAR", nil, true},
		{"VARBINARY with no length", "VARBINARY", nil, true},
		{"varchar in lower case", "varchar", nil, true},
		{"VARCHAR with a length", "VARCHAR", intp(255), false},
		// The types with a server default of their own, which is why the list
		// names two spellings rather than every type whose parenthesis is
		// usually written.
		{"CHAR with no length", "CHAR", nil, false},
		{"BINARY with no length", "BINARY", nil, false},
		{"BIT with no length", "BIT", nil, false},
		{"DECIMAL with no precision", "DECIMAL", nil, false},
		// The other half of M8: every ENUM column is in this state, including
		// one the importer has just produced, so refusing would make the round
		// trip fail by construction.
		{"ENUM with no value list", "ENUM", nil, false},
		{"SET with no value list", "SET", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, _ *model.Table) {
				a.Columns[1].Type = tt.ty
				a.Columns[1].Length = tt.length
			})
			got := Check(doc)
			if !tt.refused {
				if len(got) != 0 {
					t.Fatalf("Check reported %v, want nothing", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
			}
			if !strings.Contains(got[0].Message, "no default length") {
				t.Errorf("finding = %v, want it to say why", got[0])
			}
		})
	}
}

// TestMySQLDoesNotRefuseSetDefault exists so that a well-meaning later change
// has to argue with a test.
//
// SET DEFAULT reads like a refusal waiting to be written, and the MySQL manual
// says InnoDB rejects it. MySQL 8.0.46 does not: it takes the clause, records
// it, and mysqldump writes it back, so a document jjf itself imported can carry
// one - and refusing it would refuse a document jjf wrote. What InnoDB declines
// to do is perform the action at run time, which is a fact about the engine
// rather than about the DDL, and design/ddl-export.md records it as drift under
// the MySQL table.
func TestMySQLDoesNotRefuseSetDefault(t *testing.T) {
	for _, action := range []model.ReferentialAction{model.ActionSetDefault, model.ActionCascade, model.ActionRestrict, model.ActionSetNull, model.ActionNoAction} {
		t.Run(string(action), func(t *testing.T) {
			doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, _ *model.Table) {
				a.Columns[1].Nullable = true
				a.ForeignKeys = []model.ForeignKey{{
					Name:       "fk_a_b",
					Columns:    []string{"n"},
					References: model.Reference{Table: "b", Columns: []string{"id"}},
					OnUpdate:   action,
					OnDelete:   action,
				}}
			})
			if got := Check(doc); len(got) != 0 {
				t.Errorf("Check reported %v for ON DELETE %s, which MySQL accepts", got, action)
			}
		})
	}
}

// TestMySQLRefusesAHashInADefault is the MySQL half of the comment introducers
// internal/check refuses for every system, and it is here rather than there for
// the reason M5 gives: "#" is a comment in MySQL and MariaDB alone. PostgreSQL
// reads it as an operator, so a package that speaks about the document alone
// refusing it would refuse a legal PostgreSQL document - stricter than the
// target in one direction and wrong in the other at the same time.
func TestMySQLRefusesAHashInADefault(t *testing.T) {
	tests := []struct {
		name string
		def  string
		want bool
	}{
		{name: "a hash opens a comment", def: "1 #", want: true},
		// The commented-out text is a number rather than a word on purpose:
		// "1 # fixme" would also draw internal/check's bare-word finding,
		// which is the right answer for PostgreSQL - where "fixme" really is
		// a column reference - and would say nothing about this rule.
		{name: "a hash with text of its own", def: "1 # 2", want: true},
		{name: "a hash after a string literal", def: "'a' #", want: true},
		// The everyday false positive this must not produce: a colour is a
		// perfectly good default and starts with the byte in question.
		{name: "a hash inside a string literal is text", def: "'#ff0000'", want: false},
		{name: "a doubled quote does not end the string early", def: "'it''s #1'", want: false},
		{name: "a default with no hash at all", def: "1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			with := func(dbms model.DBMS) []check.Finding {
				return Check(twoTablesSharingFor(t, dbms, func(a, _ *model.Table) {
					def := tt.def
					a.Columns[1].Default = &def
				}))
			}

			got := with(model.DBMSMySQL)
			if tt.want {
				if len(got) != 1 || !strings.Contains(got[0].Message, `"#" starts a comment`) {
					t.Errorf("MySQL reported %v for default %q, want the comment finding alone", got, tt.def)
				}
			} else if len(got) != 0 {
				t.Errorf("MySQL reported %v for default %q, which it accepts", got, tt.def)
			}

			// The other direction of the same claim: PostgreSQL says nothing
			// about any of these, because none of them is a comment there.
			if pg := with(model.DBMSPostgreSQL); len(pg) != 0 {
				t.Errorf("PostgreSQL reported %v for default %q, where # is an operator", pg, tt.def)
			}
		})
	}
}

// TestMySQLAcceptsWhatPostgreSQLRefusesAndBack is the claim of the dialect axis
// in one test: one document, two targets, two different answers. Without the
// axis one of the two would be wrong.
func TestMySQLAcceptsWhatPostgreSQLRefusesAndBack(t *testing.T) {
	// Each half is legal in one dialect and not in the other: an index name
	// repeated across tables is PostgreSQL's collision alone, a foreign key
	// name repeated across tables is MySQL's alone.
	both := func(a, b *model.Table) {
		a.Indexes = []model.Index{{Name: "ix_created", Columns: []string{"id"}}}
		b.Indexes = []model.Index{{Name: "ix_created", Columns: []string{"id"}}}
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
	}

	pg := Check(twoTablesSharingFor(t, model.DBMSPostgreSQL, both))
	my := Check(twoTablesSharingFor(t, model.DBMSMySQL, both))
	if len(pg) != 1 || !strings.Contains(pg[0].Message, `index "ix_created"`) {
		t.Errorf("PostgreSQL reported %v, want the repeated index name alone", pg)
	}
	if len(my) != 1 || !strings.Contains(my[0].Message, `foreign key "fk_parent"`) {
		t.Errorf("MySQL reported %v, want the repeated foreign key name alone", my)
	}
}

// TestMySQLRefusedErrorNamesItsDialect is TestRefusedErrorNamesItsDialect for
// the second dialect: the summary line a user reads says which database refused,
// and cmd/jjf prints that sentence without composing it.
func TestMySQLRefusedErrorNamesItsDialect(t *testing.T) {
	err := Accept(loadDoc(t, "mysql/refused.json"))
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("Accept returned %v, want a *RefusedError", err)
	}
	if re.Dialect != "MySQL" {
		t.Errorf("the refusal names the dialect %q, want MySQL", re.Dialect)
	}
	if want := "problem(s) prevent MySQL DDL generation"; !strings.Contains(re.Error(), want) {
		t.Errorf("summary = %q, want it to carry %q", re.Error(), want)
	}
}

// TestMySQLNamespacesDoNotBleedIntoEachOther covers the one interaction two
// separate namespaces make possible: a table and a foreign key may share a
// name, and a collision reported in one namespace must not silence a collision
// in the other. One shared "reported" set would pass every other test in this
// file and fail this one.
func TestMySQLNamespacesDoNotBleedIntoEachOther(t *testing.T) {
	doc := twoTablesSharingFor(t, model.DBMSMySQL, func(a, b *model.Table) {
		// A foreign key named after a table, which MySQL allows.
		a.ForeignKeys = []model.ForeignKey{{
			Name:       "b",
			Columns:    []string{"id"},
			References: model.Reference{Table: "b", Columns: []string{"id"}},
		}}
		b.ForeignKeys = []model.ForeignKey{{
			Name:       "b",
			Columns:    []string{"id"},
			References: model.Reference{Table: "a", Columns: []string{"id"}},
		}}
	})

	got := Check(doc)
	if len(got) != 1 {
		t.Fatalf("Check reported %d finding(s), want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "foreign key") {
		t.Errorf("finding = %v, want the foreign key collision rather than a table one", got[0])
	}
}
