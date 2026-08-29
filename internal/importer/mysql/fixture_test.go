package mysql

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

// testdata/dump/mysql<series>/ holds real "mysqldump --no-data" output, one
// directory per MySQL release series, produced from throwaway databases by
// testdata/generate.sh. testdata/dump/synthetic/ holds hand-written files whose
// own headers say so. All of them are committed, so these tests need neither a
// database nor a network.

var update = flag.Bool("update", false, "update golden files")

// checkGolden compares got against testdata/golden/name, or rewrites the golden
// file when -update is given.
//
// It is a copy of the helper in cmd/jjf/run_test.go and in
// internal/importer/postgres/fixture_test.go. Copying eight lines is the right
// call here: sharing it would mean a test-only package that all eight
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

// goldenSeries is the MySQL release series the golden documents are built from.
// Every other captured series is held to those same goldens rather than to ones
// of its own, which is the whole point of capturing them: see
// TestImportAgreesAcrossMysqldumpSeries.
const goldenSeries = "8.0"

// syntheticDir holds the hand-written dumps, kept out of the per-series layout
// so that nothing there claims to be a capture of a mysqldump that never wrote
// it.
const syntheticDir = "synthetic"

// fixture is one committed dump and what importing it must produce.
type fixture struct {
	// dir is the directory under testdata/dump the dump lives in.
	dir  string
	name string
	// database overrides the name the document gives the database. Every
	// capture names its own in the banner, so it is only set for a synthetic
	// file that carries none.
	database string
	// warningsGolden names the file the diagnostics are compared against. An
	// empty value means the dump must import with no diagnostics at all: a
	// fixture that quietly starts warning is a regression that a golden diff
	// would otherwise absorb.
	warningsGolden string
}

// seriesFixtures lists the dumps every directory under testdata/dump holds a
// capture of, pointed at the series the goldens were built from.
func seriesFixtures() []fixture {
	return []fixture{
		{dir: seriesDir(goldenSeries), name: "ecshop"},
		{dir: seriesDir(goldenSeries), name: "edge", warningsGolden: "edge.warnings.txt"},
	}
}

// fixtures lists the dumps every test below walks.
func fixtures() []fixture {
	return append(seriesFixtures(),
		fixture{dir: syntheticDir, name: "legacy57", warningsGolden: "legacy57.warnings.txt"})
}

// seriesDir names the directory under testdata/dump holding the dumps one MySQL
// release series produced.
func seriesDir(series string) string { return "mysql" + series }

// capturedSeries lists the release series testdata/dump holds a capture of, in
// ascending order.
//
// The glob is deliberately loose about the digits, where the PostgreSQL
// sibling's "pg[0-9][0-9]" is exact: MySQL's series is two numbers with a dot
// between them and neither is guaranteed to stay one digit. What the loop below
// insists on instead is that every directory it finds parses as one.
func capturedSeries(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "dump", "mysql*"))
	if err != nil {
		t.Fatal(err)
	}
	series := make([]string, 0, len(paths))
	for _, path := range paths {
		s := strings.TrimPrefix(filepath.Base(path), "mysql")
		if _, ok := seriesMajor(s); !ok {
			t.Fatalf("%s is not a per-series dump directory", path)
		}
		series = append(series, s)
	}
	if len(series) == 0 {
		t.Fatal("no dump directories found under testdata/dump")
	}
	slices.Sort(series)
	return series
}

// seriesMajor reads the major out of a release series such as "8.0". It is the
// test's own reading of a directory name and is deliberately not majorVersion:
// that one reads a server banner, which carries a patch level and a
// distribution suffix this never does.
func seriesMajor(series string) (int, bool) {
	head, tail, ok := strings.Cut(series, ".")
	if !ok || head == "" || tail == "" {
		return 0, false
	}
	if _, err := strconv.Atoi(tail); err != nil {
		return 0, false
	}
	major, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}
	return major, true
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
	opt.Database = f.database
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

// TestImportAgreesAcrossMysqldumpSeries is what the per-series captures exist
// for: the same source schema, dumped by every mysqldump series jjf supports,
// has to import to the same document. A mysqldump that starts writing a
// different shape is caught here rather than in the field.
//
// The second captured series is what made it assert anything, and adding one
// needs no code: testdata/dump/mysql8.4/ is a directory. What that capture
// showed is that mysqldump 8.4 writes these fixtures exactly as 8.0 does apart
// from the three header lines that describe the dump rather than the schema -
// the ones .github/workflows/mysql-fixtures.yml ignores - so today the
// documents agree because the text does. A failure here means that stopped
// being true.
//
// Which is also what it cannot yet tell apart: a series-insensitive importer
// and two identical files. Those three header lines are the ONLY bytes that
// differ between the two captures, and of what the importer reads out of them
// the database name is the same in both and the server version reaches
// checkDumpVersion and so the diagnostics this test discards. What the
// comparison establishes today is therefore that the text which does differ
// stays out of the document; a series-dependent misreading of the SCHEMA would
// pass here, because there is no schema text differing to misread. The capture
// is doing the work rather than the comparison - which is the point rather than
// a defect, because the day mysqldump's output diverges is the day this starts
// checking, and the capture is what makes that day visible.
//
// The PostgreSQL counterpart is the contrast and not the same test in another
// package: pg13 and pg18 disagree below the header - pg16 moved a sequence's
// ownership from ALTER TABLE to ALTER SEQUENCE and pg17 added
// SET transaction_timeout - so TestImportAgreesAcrossPgDumpMajors is comparing
// two readings of two texts. A reader who has seen that one earn its name
// should not assume this one does yet.
//
// There is nothing here to fix in the importer, which never asks what series it
// is reading. Confirming that it does not would need text that differs by
// series, and the only text that does is the header.
//
// Only the documents are compared. Diagnostics legitimately differ between
// series - the warning line numbers move with the dump header, and a series
// outside the supported range adds the version warning - so the exact-warning
// golden stays on goldenSeries alone, in TestImportFixtures.
func TestImportAgreesAcrossMysqldumpSeries(t *testing.T) {
	for _, f := range seriesFixtures() {
		want, err := os.ReadFile(filepath.Join("testdata", "golden", f.name+".json"))
		if err != nil {
			t.Fatalf("read golden (run go test -update to create it): %v", err)
		}

		for _, series := range capturedSeries(t) {
			t.Run(f.name+"/"+seriesDir(series), func(t *testing.T) {
				f.dir = seriesDir(series)
				doc, _ := importFixture(t, f)

				got, err := model.Encode(doc)
				if err != nil {
					t.Fatalf("Encode returned error %v, want no error", err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("importing the mysql%s dump does not produce the mysql%s golden\n--- got ---\n%s\n--- want ---\n%s",
						series, goldenSeries, got, want)
				}
			})
		}
	}
}

// TestCapturedSeriesCoverTheSupportedMajors keeps the committed dumps and the
// range checkDumpVersion names from drifting apart. Widening the range without
// capturing a dump from the new major would leave the claim untested, and
// dropping the last capture of a major would leave the range claiming more than
// the fixtures show.
//
// It is stated over MAJORS rather than over series, which is the one place this
// differs from its PostgreSQL counterpart and follows from the two granularities
// the package already keeps apart: the captures are per series because
// mysqldump's output differs between 8.0 and 8.4, and the supported range is
// per major because that is what a banner's leading number means. So the rule
// is: every captured series belongs to a supported major, and every supported
// major has at least one captured series.
func TestCapturedSeriesCoverTheSupportedMajors(t *testing.T) {
	covered := map[int]bool{} // looked up and assigned only; the walk below is over a range
	for _, series := range capturedSeries(t) {
		major, ok := seriesMajor(series)
		if !ok {
			t.Fatalf("captured series %q does not name a major", series)
		}
		if major < minSupportedMajor || major > maxSupportedMajor {
			t.Errorf("testdata/dump/mysql%s is outside the supported range %s", series, supportedMajors())
		}
		covered[major] = true
	}
	for major := minSupportedMajor; major <= maxSupportedMajor; major++ {
		if !covered[major] {
			t.Errorf("MySQL %d is inside the supported range %s but no dump under testdata/dump was captured from it",
				major, supportedMajors())
		}
	}
}

// TestImportedDocumentsAreSelfConsistent holds the importer to the promise its
// construction already makes: a document jjf builds from a dump never
// contradicts itself.
//
// The mechanism, not the assertion: applyForeignKey refuses a foreign key whose
// naming or referenced columns do not resolve, one whose target table was not
// imported, and one whose two ends name a different number of columns; applyKey
// refuses a key naming an unknown column and forces every primary key column
// NOT NULL; and applyIndexes declines the index InnoDB names after a foreign
// key, which is the one collision MySQL produces and a jjf document cannot
// hold. Each of those is one of the checks in internal/check, arrived at
// independently from the other end.
//
// A failure here therefore means one of two things: the importer has started
// emitting something it cannot back up, or a fixture is not the mysqldump
// output it claims to be. It does not mean the checker should be relaxed.
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

	// The claim is about every captured series, so every one of them is walked
	// as well, the way TestImportAgreesAcrossMysqldumpSeries does.
	for _, f := range seriesFixtures() {
		for _, series := range capturedSeries(t) {
			t.Run(f.name+"/"+seriesDir(series), func(t *testing.T) {
				f.dir = seriesDir(series)
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
	doc, _ := importFixture(t, fixture{dir: seriesDir(goldenSeries), name: "ecshop"})

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
	if se.Line <= 20 {
		t.Errorf("error line got = %v, want a line inside the second table", se.Line)
	}
}

// TestTheCapturedDumpsCarryWhatTheImporterMustStepOver checks the ecshop
// capture for the three statement shapes that reach the importer only from a
// real mysqldump: an executable comment, a DELIMITER-wrapped trigger, and a
// view written twice. It is a check on the FIXTURE rather than on the code, and
// it exists because all three are easy to lose while editing testdata/source
// and impossible to notice afterwards - the importer skips them in silence, so
// their absence changes no golden.
func TestTheCapturedDumpsCarryWhatTheImporterMustStepOver(t *testing.T) {
	for _, series := range capturedSeries(t) {
		t.Run(seriesDir(series), func(t *testing.T) {
			raw, err := os.ReadFile(dumpPath(seriesDir(series), "ecshop"))
			if err != nil {
				t.Fatal(err)
			}
			src := string(raw)
			for _, want := range []string{
				"/*!40101 SET",             // an executable comment
				"DELIMITER ;;",             // a trigger under a custom delimiter
				"Temporary view structure", // the view placeholder
				"Final view structure",     // and the view itself
				"DROP TABLE IF EXISTS",     // a statement with nothing to import
			} {
				if !strings.Contains(src, want) {
					t.Errorf("the ecshop capture no longer contains %q", want)
				}
			}
		})
	}
}

// TestTheCapturedDumpsKeepTheirJapanese is the trap that survives every other
// test in this file. A mysqldump taken over a connection that defaulted to
// latin1 writes every Japanese comment double-encoded, and the result still
// lexes, still parses, still imports and still round-trips - so a golden
// regenerated from such a capture would simply carry mojibake forward and no
// assertion would notice. testdata/generate.sh passes
// --default-character-set=utf8mb4 for that reason, and this is what says so in
// a form that fails.
func TestTheCapturedDumpsKeepTheirJapanese(t *testing.T) {
	for _, series := range capturedSeries(t) {
		t.Run(seriesDir(series), func(t *testing.T) {
			doc, _ := importFixture(t, fixture{dir: seriesDir(series), name: "ecshop"})
			for _, want := range []string{"顧客", "メールアドレス", "残高", "注文明細", "配送"} {
				if !documentContains(doc, want) {
					t.Errorf("the imported ecshop document does not contain %q; the capture may be mojibake", want)
				}
			}
		})
	}
}

// documentContains reports whether any logical name or description in doc is
// exactly want. An exact match rather than a substring: mojibake of "顧客" is
// longer than "顧客" and contains none of it, so equality is the sharper test.
func documentContains(doc *model.Document, want string) bool {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		if t.LogicalName == want || t.Description == want {
			return true
		}
		for j := range t.Columns {
			c := &t.Columns[j]
			if c.LogicalName == want || c.Description == want {
				return true
			}
		}
	}
	return false
}
