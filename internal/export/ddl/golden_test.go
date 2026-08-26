package ddl

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

// pgFixtures are the PostgreSQL documents whose rendering is frozen. Between
// them they hold every shape that dialect's writer has to survive: full.json is
// the ordinary document, edge.json the awkward ones, minimal.json the smallest
// output there is, where all three optional sections vanish.
//
// They stay directly under testdata/ rather than moving into a testdata/pg/
// subdirectory beside the list a second dialect will need. The move would cost
// a rename of every golden, an edit to roundtrip.sh - which resolves a bare
// name beside itself - and an edit to two more files that quote those paths,
// for no behavioural gain; the asymmetry is worth this paragraph and nothing
// else.
var pgFixtures = []string{"full.json", "edge.json", "minimal.json"}

// myFixtures are the MySQL documents whose rendering is frozen: the same three
// shapes for the second dialect, so that a change to the shared code shows up
// as two diffs rather than one.
//
// They carry their directory in their names because the PostgreSQL ones do
// not. A second dialect is what makes testdata/ need directories at all, and
// the entries above stayed where they were for the reason given there - so
// "full.json" was already taken, and the path is also what tells the two apart
// in a sub-test name and in a golden's.
var myFixtures = []string{"mysql/full.json", "mysql/edge.json", "mysql/minimal.json"}

// allFixtures is every frozen document, dialect by dialect in the order
// dialects() lists them.
//
// A function returning a fresh slice rather than a package-level var built
// with append, because appending to pgFixtures would hand a test the power to
// scribble on another test's list. The tests below walk this rather than each
// list separately: every property they assert is a property of the output
// whichever dialect wrote it, and a per-dialect loop would have to be extended
// by hand for the third one.
func allFixtures() []string {
	return slices.Concat(pgFixtures, myFixtures)
}

// ---------------------------------------------------------------------------
// Fixtures and golden files
// ---------------------------------------------------------------------------

// loadDoc reads a fixture and decodes it. The fixture is checked against the
// schema on the way through, because a fixture that no longer conforms tests
// nothing worth knowing - and it would also be a document the CLI could never
// reach this exporter with.
func loadDoc(t *testing.T, name string) *model.Document {
	t.Helper()

	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(path, raw); err != nil {
		t.Fatalf("fixture does not conform to the schema: %v", err)
	}
	doc, err := model.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// goldenName maps a fixture name to the name of its golden file.
func goldenName(fixture string) string {
	return strings.TrimSuffix(fixture, filepath.Ext(fixture)) + ".sql"
}

func checkGolden(t *testing.T, name, have string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(have), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/export/ddl/ -update): %v", err)
	}
	if string(want) != have {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, have, want)
	}
}

// render exports doc and fails the test if the export is refused.
func render(t *testing.T, doc *model.Document) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Export(&buf, doc); err != nil {
		t.Fatalf("export: %v", err)
	}
	return buf.String()
}

func TestGolden(t *testing.T) {
	for _, name := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			checkGolden(t, goldenName(name), render(t, loadDoc(t, name)))
		})
	}
}

// TestFixturesAreDeterministic renders each fixture twice. There are no time
// zone sub-tests as there are for the workbook: those exist because the zip
// format records local timestamps, and nothing in this package reads the clock
// at all.
func TestFixturesAreDeterministic(t *testing.T) {
	for _, name := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			doc := loadDoc(t, name)
			if first, second := render(t, doc), render(t, doc); first != second {
				t.Errorf("two renders of %s differ:\n--- first ---\n%s\n--- second ---\n%s", name, first, second)
			}
		})
	}
}

// TestGoldenFixturesAreAccepted is load-bearing rather than redundant: a
// fixture Accept refuses could never be rendered, so its golden would pin
// nothing and TestGolden would fail with a refusal instead of a diff.
func TestGoldenFixturesAreAccepted(t *testing.T) {
	for _, name := range allFixtures() {
		t.Run(name, func(t *testing.T) {
			doc := loadDoc(t, name)
			if err := Accept(doc); err != nil {
				t.Fatalf("Accept refused the fixture: %v", err)
			}
			if got := Check(doc); len(got) != 0 {
				t.Errorf("Check reported %d finding(s) for a fixture that must have none: %v", len(got), got)
			}
		})
	}
}

// TestEveryDialectHasGoldenFixtures holds the fixture lists to the dialect
// table. A dialect added with no document naming it would ship with its output
// pinned by nothing at all, and every test above would keep passing, because
// they walk the fixtures rather than the table.
func TestEveryDialectHasGoldenFixtures(t *testing.T) {
	for _, d := range dialects() {
		t.Run(string(d.dbms), func(t *testing.T) {
			for _, name := range allFixtures() {
				if loadDoc(t, name).Database.DBMS == d.dbms {
					return
				}
			}
			t.Errorf("no fixture names the dbms %q, so nothing pins what %s writes", d.dbms, d.name)
		})
	}
}
