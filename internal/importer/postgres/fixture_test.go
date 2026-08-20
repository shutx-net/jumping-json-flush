package postgres

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/export/xlsx"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// The fixtures under testdata/dump are real "pg_dump --schema-only" output,
// produced from a throwaway PostgreSQL 16 cluster by testdata/generate.sh, plus
// two hand-written dumps whose own headers say so. They are committed, so these
// tests need neither a database nor a network.

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

// fixture is one committed dump and what importing it must produce.
type fixture struct {
	name string
	// schemaName overrides the default target schema.
	schemaName string
	// warningsGolden names the file the diagnostics are compared against. An
	// empty value means the dump must import with no diagnostics at all: a
	// fixture that quietly starts warning is a regression that a golden diff
	// would otherwise absorb.
	warningsGolden string
}

// fixtures lists the dumps every test below walks.
func fixtures() []fixture {
	return []fixture{
		{name: "ecshop"},
		{name: "edge", warningsGolden: "edge.warnings.txt"},
		{name: "pg13_legacy"},
	}
}

// dumpPath is the committed dump of a fixture.
func dumpPath(name string) string { return filepath.Join("testdata", "dump", name+".sql") }

// importFixture reads a fixture and imports it.
func importFixture(t *testing.T, f fixture) (*model.Document, []Diagnostic) {
	t.Helper()

	path := dumpPath(f.name)
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
	doc, _ := importFixture(t, fixture{name: "ecshop"})

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
	path := dumpPath("broken")
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
		doc, diags := importFixture(t, fixture{name: "edge", schemaName: "audit"})
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
		_, diags := importFixture(t, fixture{name: "edge"})
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
