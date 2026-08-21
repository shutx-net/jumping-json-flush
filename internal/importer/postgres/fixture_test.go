package postgres

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/export/xlsx"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// testdata/dump/pg<major>/ holds real "pg_dump --schema-only" output, one
// directory per PostgreSQL major, produced from throwaway clusters by
// testdata/generate.sh. testdata/dump/synthetic/ holds hand-written files whose
// own headers say so. All of them are committed, so these tests need neither a
// database nor a network.

var update = flag.Bool("update", false, "update golden files")

// checkGolden compares got against testdata/golden/name, or rewrites the golden
// file when -update is given.
//
// It is a copy of the helper in cmd/jjf/run_test.go. Copying eight lines is the
// right call here: sharing it would mean a test-only package that all five
// golden-owning packages would have to be rewritten onto, for no gain.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run go test -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// goldenMajor is the pg_dump major the golden documents are built from. Every
// other major is held to those same goldens rather than to one of its own,
// which is the whole point of capturing them: see
// TestImportAgreesAcrossPgDumpMajors.
const goldenMajor = 16

// syntheticDir holds the hand-written dumps, kept out of the per-major layout
// so that nothing there claims to be a capture of a pg_dump that never wrote it.
const syntheticDir = "synthetic"

// fixture is one committed dump and what importing it must produce.
type fixture struct {
	// dir is the directory under testdata/dump the dump lives in.
	dir  string
	name string
	// schemaName overrides the default target schema.
	schemaName string
	// warningsGolden names the file the diagnostics are compared against. An
	// empty value means the dump must import with no diagnostics at all: a
	// fixture that quietly starts warning is a regression that a golden diff
	// would otherwise absorb.
	warningsGolden string
}

// majorFixtures lists the dumps every major under testdata/dump holds a capture
// of, pointed at the major the goldens were built from.
func majorFixtures() []fixture {
	return []fixture{
		{dir: majorDir(goldenMajor), name: "ecshop"},
		{dir: majorDir(goldenMajor), name: "edge", warningsGolden: "edge.warnings.txt"},
	}
}

// fixtures lists the dumps every test below walks.
func fixtures() []fixture {
	return append(majorFixtures(), fixture{dir: syntheticDir, name: "legacy_unqualified"})
}

// majorDir names the directory under testdata/dump holding the dumps one
// pg_dump major produced.
func majorDir(major int) string { return "pg" + strconv.Itoa(major) }

// capturedMajors lists the pg_dump majors testdata/dump holds a capture of, in
// ascending order.
func capturedMajors(t *testing.T) []int {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "dump", "pg[0-9][0-9]"))
	if err != nil {
		t.Fatal(err)
	}
	majors := make([]int, 0, len(paths))
	for _, path := range paths {
		major, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "pg"))
		if err != nil {
			t.Fatalf("%s is not a per-major dump directory: %v", path, err)
		}
		majors = append(majors, major)
	}
	if len(majors) == 0 {
		t.Fatal("no dump directories found under testdata/dump")
	}
	slices.Sort(majors)
	return majors
}

// dumpPath is the committed dump of a fixture.
func dumpPath(dir, name string) string {
	return filepath.Join("testdata", "dump", dir, name+".sql")
}

// importFixture reads a fixture and imports it.
func importFixture(t *testing.T, f fixture) (*model.Document, []Diagnostic) {
	t.Helper()

	path := dumpPath(f.dir, f.name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Source = path
	if f.schemaName != "" {
		opt.Schema = f.schemaName
	}
	doc, diags, err := Import(raw, opt)
	if err != nil {
		t.Fatalf("Import(%s) returned error %v, want no error", path, err)
	}
	return doc, diags
}

// renderDiagnostics lays the warnings out one per line, which is the shape the
// warnings golden pins - their content and their order both.
func renderDiagnostics(diags []Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString(d.String())
		b.WriteString("\n")
	}
	return b.String()
}

func TestImportFixtures(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			doc, diags := importFixture(t, f)

			raw, err := model.Encode(doc)
			if err != nil {
				t.Fatalf("Encode returned error %v, want no error", err)
			}
			checkGolden(t, f.name+".json", string(raw))

			if f.warningsGolden == "" {
				if len(diags) != 0 {
					t.Errorf("diagnostics got = %v, want none", diags)
				}
				return
			}
			checkGolden(t, f.warningsGolden, renderDiagnostics(diags))
		})
	}
}

// TestImportAgreesAcrossPgDumpMajors is what the per-major captures exist for:
// the same source schema, dumped by every pg_dump major jjf supports, has to
// import to the same document. A pg_dump that starts writing a different shape
// is caught here rather than in the field.
//
// Only the documents are compared. Diagnostics legitimately differ between
// majors - the warning line numbers move with the dump header, and a major
// outside the supported range adds the version warning - so the exact-warning
// golden stays on goldenMajor alone, in TestImportFixtures.
func TestImportAgreesAcrossPgDumpMajors(t *testing.T) {
	for _, f := range majorFixtures() {
		want, err := os.ReadFile(filepath.Join("testdata", "golden", f.name+".json"))
		if err != nil {
			t.Fatalf("read golden (run go test -update to create it): %v", err)
		}

		for _, major := range capturedMajors(t) {
			t.Run(f.name+"/"+majorDir(major), func(t *testing.T) {
				f.dir = majorDir(major)
				doc, _ := importFixture(t, f)

				got, err := model.Encode(doc)
				if err != nil {
					t.Fatalf("Encode returned error %v, want no error", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("importing the pg%d dump does not produce the pg%d golden\n--- got ---\n%s\n--- want ---\n%s",
						major, goldenMajor, got, want)
				}
			})
		}
	}
}

// TestImportedDocumentsAreSelfConsistent holds the importer to the promise its
// construction already makes: a document jjf builds from a dump never
// contradicts itself.
//
// The mechanism, not the assertion: applyForeignKey refuses a foreign key whose
// naming or referenced columns do not resolve, one that reaches outside the
// target schema, one whose target table was not imported, and one whose two
// ends name a different number of columns; applyKey refuses a key naming an
// unknown column and forces every primary key column NOT NULL. Each of those is
// one of the checks in internal/check, arrived at independently from the other
// end.
//
// A failure here therefore means one of two things: the importer has started
// emitting something it cannot back up, or a fixture is not the pg_dump output
// it claims to be. It does not mean the checker should be relaxed.
//
// Importing internal/check from a test in this package is not a cycle: check
// does not import the importer, and it never will.
func TestImportedDocumentsAreSelfConsistent(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			doc, _ := importFixture(t, f)
			assertSelfConsistent(t, doc)
		})
	}

	// The claim is literally "pg13 through pg18", so every captured major is
	// walked as well, the way TestImportAgreesAcrossPgDumpMajors does.
	for _, f := range majorFixtures() {
		for _, major := range capturedMajors(t) {
			t.Run(f.name+"/"+majorDir(major), func(t *testing.T) {
				f.dir = majorDir(major)
				doc, _ := importFixture(t, f)
				assertSelfConsistent(t, doc)
			})
		}
	}
}

// assertSelfConsistent fails the test with every finding on its own line, so
// that a regression reads as a list of things to fix rather than as a count.
func assertSelfConsistent(t *testing.T, doc *model.Document) {
	t.Helper()

	findings := check.Document(doc)
	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, finding := range findings {
		b.WriteString(finding.String())
		b.WriteString("\n")
	}
	t.Errorf("the imported document contradicts itself:\n%s", &b)
}

// TestCapturedMajorsCoverTheSupportedRange keeps the committed dumps and the
// range checkDumpVersion names from drifting apart. Widening the range without
// capturing the new major would leave the claim untested, and dropping a
// capture would leave the range claiming more than the fixtures show.
func TestCapturedMajorsCoverTheSupportedRange(t *testing.T) {
	var want []int
	for major := minSupportedMajor; major <= maxSupportedMajor; major++ {
		want = append(want, major)
	}
	if got := capturedMajors(t); !slices.Equal(got, want) {
		t.Errorf("captured majors got = %v, want %v", got, want)
	}
}

// TestFixtureDocumentsConformToTheSchema reads the golden documents back from
// disk rather than validating the bytes just produced, so that a golden edited
// by hand after it was generated is caught too.
func TestFixtureDocumentsConformToTheSchema(t *testing.T) {
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}

	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no golden documents found (run go test -update to create them)")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := validator.Validate(path, raw); err != nil {
				var ide *schema.InvalidDocumentError
				if errors.As(err, &ide) {
					var report strings.Builder
					ide.WriteReport(&report)
					t.Fatalf("%s does not conform to the jjf database design schema:\n%s", path, &report)
				}
				t.Fatalf("validating %s: %v", path, err)
			}
			if _, err := model.Decode(raw); err != nil {
				t.Fatalf("decoding %s: %v", path, err)
			}
		})
	}
}

func TestFixtureImportIsDeterministic(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			firstDoc, firstDiags := importFixture(t, f)
			secondDoc, secondDiags := importFixture(t, f)

			first, err := model.Encode(firstDoc)
			if err != nil {
				t.Fatal(err)
			}
			second, err := model.Encode(secondDoc)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Error("two imports of the same dump produced different bytes")
			}
			if !reflect.DeepEqual(firstDiags, secondDiags) {
				t.Errorf("diagnostics got = %v and %v, want them equal", firstDiags, secondDiags)
			}
		})
	}
}

// TestFixtureRoundTripToXLSX closes the loop the importer exists for: a dump
// becomes a document, and the document becomes a workbook.
//
// Importing internal/export/xlsx from a test in this package is not a cycle:
// xlsx does not import the importer, and it never will.
func TestFixtureRoundTripToXLSX(t *testing.T) {
	doc, _ := importFixture(t, fixture{dir: majorDir(goldenMajor), name: "ecshop"})

	raw, err := model.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := model.Decode(raw)
	if err != nil {
		t.Fatalf("Decode returned error %v, want no error", err)
	}

	var book bytes.Buffer
	if err := xlsx.Export(&book, decoded, xlsx.DefaultOptions()); err != nil {
		t.Fatalf("Export returned error %v, want no error", err)
	}
	if !bytes.HasPrefix(book.Bytes(), []byte("PK")) {
		t.Errorf("the exported workbook is not a zip package (%d bytes)", book.Len())
	}
}

func TestImportBrokenFixture(t *testing.T) {
	path := dumpPath(syntheticDir, "broken")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Source = path

	_, _, err = Import(raw, opt)
	if err == nil {
		t.Fatal("Import returned no error, want one")
	}
	if got := exitcode.Of(err); got != exitcode.InvalidInput {
		t.Errorf("exit code got = %v, want %v", got, exitcode.InvalidInput)
	}
	var se *syntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error type got = %T, want it to wrap *syntaxError", err)
	}
	if se.Line <= 1 {
		t.Errorf("error line got = %v, want a line inside the second table", se.Line)
	}
}

func TestImportSchemaFilterOnEdgeFixture(t *testing.T) {
	t.Run("audit", func(t *testing.T) {
		doc, diags := importFixture(t, fixture{dir: majorDir(goldenMajor), name: "edge", schemaName: "audit"})
		var names []string
		for _, table := range doc.Tables {
			names = append(names, table.Name)
		}
		if want := []string{"events"}; !slices.Equal(names, want) {
			t.Errorf("tables got = %v, want %v", names, want)
		}
		// The warnings are about the DUMP, not about the schema being
		// imported: they are produced while parsing, which happens before
		// anything is filtered. Only the absence of an audit warning is
		// asserted here.
		for _, d := range diags {
			if strings.Contains(d.Message, "audit.") {
				t.Errorf("diagnostics got = %q, want nothing about the imported schema", d)
			}
		}
	})

	t.Run("the cross-schema foreign key is reported once", func(t *testing.T) {
		_, diags := importFixture(t, fixture{dir: majorDir(goldenMajor), name: "edge"})
		crossing := 0
		for _, d := range diags {
			if strings.Contains(d.Message, "outside schema public") {
				crossing++
			}
		}
		if crossing != 1 {
			t.Errorf("cross-schema warnings got = %v, want exactly 1\n%s", crossing, renderDiagnostics(diags))
		}
	})
}
