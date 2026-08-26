package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/importer/postgres"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

// TestMain doubles as the entry point of the subprocess used by
// TestExitCodeOfRealProcess, so that the exit code os.Exit really produces can
// be asserted once. Every other test calls run directly.
func TestMain(m *testing.M) {
	if os.Getenv("JJF_TEST_SUBPROCESS") == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// checkGolden compares got against testdata/golden/name, or rewrites the golden
// file when -update is given.
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

// TestFlags covers the permuting argument parser. The standard flag package
// stops at the first operand, so every case where a flag follows one would
// otherwise be silently dropped.
func TestFlags(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantOut      string
		wantVerbose  bool
		wantOperands []string
		wantErr      string // substring of the reported flag error
	}{
		{
			name:         "flag after operands",
			args:         []string{"xlsx", "in.json", "-o", "out.xlsx"},
			wantOut:      "out.xlsx",
			wantOperands: []string{"xlsx", "in.json"},
		},
		{
			name:         "flag between operands",
			args:         []string{"xlsx", "-o", "out.xlsx", "in.json"},
			wantOut:      "out.xlsx",
			wantOperands: []string{"xlsx", "in.json"},
		},
		{
			name:         "flag before operands",
			args:         []string{"-o", "out.xlsx", "xlsx", "in.json"},
			wantOut:      "out.xlsx",
			wantOperands: []string{"xlsx", "in.json"},
		},
		{
			name:         "trailing boolean flag",
			args:         []string{"xlsx", "in.json", "-o", "out.xlsx", "-v"},
			wantOut:      "out.xlsx",
			wantVerbose:  true,
			wantOperands: []string{"xlsx", "in.json"},
		},
		{
			name:         "double dash protects one operand",
			args:         []string{"xlsx", "--", "-weird.json"},
			wantOperands: []string{"xlsx", "-weird.json"},
		},
		{
			name:         "double dash protects every remaining operand",
			args:         []string{"xlsx", "--", "-weird.json", "-o", "out.xlsx"},
			wantOperands: []string{"xlsx", "-weird.json", "-o", "out.xlsx"},
		},
		{
			name:    "flag without its value",
			args:    []string{"xlsx", "in.json", "-o"},
			wantErr: "flag needs an argument: -o",
		},
		{
			name:    "undefined flag",
			args:    []string{"xlsx", "in.json", "-bogus"},
			wantErr: "flag provided but not defined: -bogus",
		},
		{
			name:         "surplus operands are returned, not rejected",
			args:         []string{"xlsx", "in.json", "extra.json"},
			wantOperands: []string{"xlsx", "in.json", "extra.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := newCommand("export", "Usage:\n  jjf export <format> <input.json> -o <output>\n")
			out := cmd.fs.String("o", "", "output path")
			verbose := cmd.fs.Bool("v", false, "verbose output")

			operands, err := cmd.parse(tt.args, &stdout, &stderr)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parse(%q) = nil error, want %q", tt.args, tt.wantErr)
				}
				if !strings.Contains(stderr.String(), tt.wantErr) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
				}
				if got, want := exitcode.Of(err), exitcode.InvalidInput; got != want {
					t.Errorf("exitcode.Of(err) = %d, want %d", got, want)
				}
				if !strings.Contains(stderr.String(), "Usage:") {
					t.Error("stderr does not include the usage")
				}
				if stdout.Len() != 0 {
					t.Errorf("flag errors must not go to stdout, got %q", stdout.String())
				}
				return
			}

			if err != nil {
				t.Fatalf("parse(%q) = %v, want nil", tt.args, err)
			}
			if *out != tt.wantOut {
				t.Errorf("-o = %q, want %q", *out, tt.wantOut)
			}
			if *verbose != tt.wantVerbose {
				t.Errorf("-v = %v, want %v", *verbose, tt.wantVerbose)
			}
			if !reflect.DeepEqual(operands, tt.wantOperands) {
				t.Errorf("operands = %q, want %q", operands, tt.wantOperands)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestFlagsHelpGoesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := newCommand("export", "Usage:\n  jjf export <format> <input.json> -o <output>\n")
	cmd.fs.String("o", "", "output path")

	_, err := cmd.parse([]string{"-h"}, &stdout, &stderr)
	if !errors.Is(err, errHelpHandled) {
		t.Fatalf("parse(-h) = %v, want errHelpHandled", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("stdout = %q, want the usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "-o string") {
		t.Errorf("stdout = %q, want the flag defaults", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	if got, want := reportAndCode(err, &stderr), int(exitcode.OK); got != want {
		t.Errorf("reportAndCode(errHelpHandled) = %d, want %d", got, want)
	}
}

func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCode    int
		wantStdout  string // substring
		wantStderr  string // substring
		emptyStdout bool
		emptyStderr bool
	}{
		{
			name:        "no arguments prints usage to stderr",
			args:        nil,
			wantCode:    2,
			wantStderr:  "Usage:",
			emptyStdout: true,
		},
		{
			name:        "long help prints usage to stdout",
			args:        []string{"--help"},
			wantCode:    0,
			wantStdout:  "Usage:",
			emptyStderr: true,
		},
		{
			name:        "short help prints usage to stdout",
			args:        []string{"-h"},
			wantCode:    0,
			wantStdout:  "Usage:",
			emptyStderr: true,
		},
		{
			name:        "help subcommand prints usage to stdout",
			args:        []string{"help"},
			wantCode:    0,
			wantStdout:  "Usage:",
			emptyStderr: true,
		},
		{
			name:        "unknown command",
			args:        []string{"bogus"},
			wantCode:    2,
			wantStderr:  `unknown command "bogus"`,
			emptyStdout: true,
		},
		{
			name:        "version",
			args:        []string{"version"},
			wantCode:    0,
			wantStdout:  "jjf ",
			emptyStderr: true,
		},
		{
			name:        "version rejects arguments",
			args:        []string{"version", "extra"},
			wantCode:    2,
			wantStderr:  "version takes no arguments",
			emptyStdout: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d\nstdout: %s\nstderr: %s",
					tt.args, code, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.emptyStdout && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if tt.emptyStderr && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunVersionReportsPlatform(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(version) = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	for _, want := range []string{"jjf ", "built with go", "/"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}

func TestBuildVersionFallsBack(t *testing.T) {
	// version is empty in a plain `go test` build, so buildVersion must fall
	// back to the stamped build info rather than return an empty string.
	if got := buildVersion(); got == "" {
		t.Error("buildVersion() = \"\", want a non-empty version")
	}
}

func TestRunValidate(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring
	}{
		{
			name:       "valid document",
			args:       []string{"validate", "testdata/valid.json"},
			wantCode:   0,
			wantStdout: "testdata/valid.json: OK",
		},
		{
			name:       "a clean document is clean with -strict too",
			args:       []string{"validate", "-strict", "testdata/valid.json"},
			wantCode:   0,
			wantStdout: "testdata/valid.json: OK",
		},
		{
			name:       "referential warnings are not fatal",
			args:       []string{"validate", "testdata/referential_warnings.json"},
			wantCode:   0,
			wantStdout: "testdata/referential_warnings.json: OK, 3 warning(s)",
			wantStderr: ": warning: ",
		},
		{
			name:       "missing file",
			args:       []string{"validate", "testdata/does_not_exist.json"},
			wantCode:   2,
			wantStderr: "no such file or directory",
		},
		{
			name:       "malformed json",
			args:       []string{"validate", "testdata/broken.json"},
			wantCode:   2,
			wantStderr: "line 5, column 4",
		},
		{
			name:       "schema violation",
			args:       []string{"validate", "testdata/schema_violation.json"},
			wantCode:   3,
			wantStderr: "7 error(s)",
		},
		{
			name:       "unsupported format version",
			args:       []string{"validate", "testdata/future_version.json"},
			wantCode:   2,
			wantStderr: `unsupported formatVersion "2.0"`,
		},
		{
			name:       "no input file",
			args:       []string{"validate"},
			wantCode:   2,
			wantStderr: "exactly one input file, got 0",
		},
		{
			name:       "two input files",
			args:       []string{"validate", "testdata/valid.json", "testdata/valid.json"},
			wantCode:   2,
			wantStderr: "exactly one input file, got 2",
		},
		{
			name:       "help",
			args:       []string{"validate", "-h"},
			wantCode:   0,
			wantStdout: "jjf validate <input.json>",
		},
		{
			name:       "undefined flag",
			args:       []string{"validate", "testdata/valid.json", "-bogus"},
			wantCode:   2,
			wantStderr: "flag provided but not defined: -bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d\nstdout: %s\nstderr: %s",
					tt.args, code, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// TestRunValidateStrict mirrors TestRunImportStrict: the warnings are the same
// either way, and -strict changes only what the run is worth.
func TestRunValidateStrict(t *testing.T) {
	const wantWarnings = 3

	tests := []struct {
		name     string
		strict   bool
		wantCode int
	}{
		{name: "warnings are not fatal by default", wantCode: 0},
		{name: "strict turns warnings into a failure", strict: true, wantCode: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"validate", "testdata/referential_warnings.json"}
			if tt.strict {
				args = append(args, "-strict")
			}

			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("run = %d, want %d\nstderr: %s", code, tt.wantCode, &stderr)
			}
			// The warnings are printed either way: -strict changes what happens
			// next, not what the user is told.
			if got := strings.Count(stderr.String(), ": warning: "); got != wantWarnings {
				t.Errorf("warnings on stderr = %d, want %d\nstderr: %s", got, wantWarnings, &stderr)
			}

			if !tt.strict {
				if !strings.Contains(stdout.String(), ": OK, 3 warning(s)") {
					t.Errorf("stdout = %q, want the OK line with the warning count", &stdout)
				}
				return
			}
			// A strict run has no verdict to report: saying OK after failing
			// would be worse than saying nothing.
			if strings.Contains(stdout.String(), ": OK") {
				t.Errorf("stdout = %q, want no success line from a strict run", &stdout)
			}
			if !strings.Contains(stderr.String(), "jjf: 3 warning(s) with -strict") {
				t.Errorf("stderr = %q, want the same sentence \"jjf import -strict\" says", &stderr)
			}
		})
	}
}

// TestRunExportWritesWorkbook covers the three positions -o may appear in. The
// standard flag package would drop it in two of them.
func TestRunExportWritesWorkbook(t *testing.T) {
	tests := []struct {
		name string
		args func(out string) []string
	}{
		{"flag after operands", func(out string) []string {
			return []string{"export", "xlsx", "testdata/valid.json", "-o", out}
		}},
		{"flag before operands", func(out string) []string {
			return []string{"export", "-o", out, "xlsx", "testdata/valid.json"}
		}},
		{"flag between operands", func(out string) []string {
			return []string{"export", "xlsx", "-o", out, "testdata/valid.json"}
		}},
	}

	var first []byte
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.xlsx")
			var stdout, stderr bytes.Buffer

			if code := run(tt.args(out), &stdout, &stderr); code != 0 {
				t.Fatalf("run = %d, want 0\nstdout: %s\nstderr: %s", code, &stdout, &stderr)
			}
			if !strings.Contains(stdout.String(), out) {
				t.Errorf("stdout = %q, want it to name %s", stdout.String(), out)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}

			got := readWorkbook(t, out)
			if first == nil {
				first = got
			} else if !bytes.Equal(first, got) {
				t.Error("the same document exported to different bytes depending on flag position")
			}
		})
	}
}

func TestRunExportDefaultsToTheInputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "db-design.json")
	raw, err := os.ReadFile("testdata/valid.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "xlsx", input}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	readWorkbook(t, filepath.Join(dir, "db-design.xlsx"))
}

func TestRunExportIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var runs [2][]byte
	for i := range runs {
		out := filepath.Join(dir, fmt.Sprintf("run%d.xlsx", i))
		var stdout, stderr bytes.Buffer
		if code := run([]string{"export", "xlsx", "testdata/valid.json", "-o", out}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
		runs[i] = readWorkbook(t, out)
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Errorf("two exports differ: %d vs %d bytes", len(runs[0]), len(runs[1]))
	}
}

func TestRunExportToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// A bytes.Buffer is not a character device, so the workbook is allowed
	// through just as it would be into a pipe or a file.
	if code := run([]string{"export", "xlsx", "testdata/valid.json", "-o", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	checkWorkbook(t, stdout.Bytes())
}

// TestRunExportRefusesATerminal checks the one thing that keeps "-o -" from
// garbling a terminal. /dev/null stands in for the terminal: what the check
// looks at is the character device bit, which both share.
func TestRunExportRefusesATerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/null to stand in for a terminal")
	}
	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer dev.Close()
	if fi, err := dev.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this system", os.DevNull)
	}

	var stderr bytes.Buffer
	code := run([]string{"export", "xlsx", "testdata/valid.json", "-o", "-"}, dev, &stderr)
	if code != 2 {
		t.Errorf("run = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "refusing to write a workbook to the terminal") {
		t.Errorf("stderr = %q, want the refusal", stderr.String())
	}
}

func TestRunExportErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		useTempOut bool // append -o <tempdir>/out.xlsx
		wantCode   int
		wantStderr string // substring
	}{
		{
			name:       "unsupported format",
			args:       []string{"export", "markdown", "testdata/valid.json"},
			wantCode:   2,
			wantStderr: `unsupported format "markdown"`,
		},
		{
			// The message lists the table, so a new format appears in it
			// without anyone editing the message.
			name:       "unsupported format names every format",
			args:       []string{"export", "markdown", "testdata/valid.json"},
			wantCode:   2,
			wantStderr: "supported formats: xlsx, svg, ddl",
		},
		{
			// A format name that differs only in case is unknown, exactly as
			// an import dialect that does is.
			name:       "a format name is case sensitive",
			args:       []string{"export", "XLSX", "testdata/valid.json"},
			wantCode:   2,
			wantStderr: `unsupported format "XLSX"`,
		},
		{
			name:       "no arguments",
			args:       []string{"export"},
			wantCode:   2,
			wantStderr: "takes a format and exactly one input file, got 0",
		},
		{
			name:       "format without input",
			args:       []string{"export", "xlsx"},
			wantCode:   2,
			wantStderr: "takes a format and exactly one input file, got 1",
		},
		{
			name:       "surplus input",
			args:       []string{"export", "xlsx", "testdata/valid.json", "testdata/valid.json"},
			wantCode:   2,
			wantStderr: "takes a format and exactly one input file, got 3",
		},
		{
			name:       "missing input file",
			args:       []string{"export", "xlsx", "testdata/does_not_exist.json"},
			useTempOut: true,
			wantCode:   2,
			wantStderr: "no such file or directory",
		},
		{
			name:       "malformed json",
			args:       []string{"export", "xlsx", "testdata/broken.json"},
			useTempOut: true,
			wantCode:   2,
			wantStderr: "line 5, column 4",
		},
		{
			name:       "unsupported format version",
			args:       []string{"export", "xlsx", "testdata/future_version.json"},
			useTempOut: true,
			wantCode:   2,
			wantStderr: `unsupported formatVersion "2.0"`,
		},
		{
			// Validation comes first, so a document that fails it produces the
			// schema exit code rather than an output error.
			name:       "schema violation",
			args:       []string{"export", "xlsx", "testdata/schema_violation.json"},
			useTempOut: true,
			wantCode:   3,
			wantStderr: "does not conform to the jjf database design schema",
		},
		{
			name:       "undefined flag",
			args:       []string{"export", "xlsx", "testdata/valid.json", "-bogus"},
			wantCode:   2,
			wantStderr: "flag provided but not defined: -bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			args := tt.args
			if tt.useTempOut {
				args = append(append([]string{}, args...), "-o", filepath.Join(dir, "out.xlsx"))
			}

			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d\nstderr: %s", args, code, tt.wantCode, &stderr)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			// A failed export must not leave a half written file behind.
			if entries, err := os.ReadDir(dir); err != nil {
				t.Fatal(err)
			} else if len(entries) != 0 {
				t.Errorf("the output directory holds %d file(s) after a failure", len(entries))
			}
		})
	}
}

func TestRunExportUnwritableOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "no-such-dir", "out.xlsx")
	var stdout, stderr bytes.Buffer

	code := run([]string{"export", "xlsx", "testdata/valid.json", "-o", out}, &stdout, &stderr)
	if code != 4 {
		t.Errorf("run = %d, want 4 (output generation error)\nstderr: %s", code, &stderr)
	}
	if !strings.Contains(stderr.String(), out) {
		t.Errorf("stderr = %q, want it to name the requested output path %s", stderr.String(), out)
	}
}

// readWorkbook reads path and checks that it is an .xlsx package.
func readWorkbook(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	checkWorkbook(t, b)
	return b
}

// checkWorkbook checks that b is a zip archive holding a spreadsheet.
func checkWorkbook(t *testing.T, b []byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("the output is not a zip archive: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	for _, want := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml"} {
		if !slices.Contains(names, want) {
			t.Errorf("the workbook has no %s; it holds %v", want, names)
		}
	}
}

// ---------------------------------------------------------------------------
// The svg format
// ---------------------------------------------------------------------------

// TestRunExportSVGAllowsATerminal is the mirror of
// TestRunExportRefusesATerminal: SVG is text, so writing it to a terminal
// garbles nothing, while a workbook to the same destination is refused.
//
// With TestRunExportDDLAllowsATerminal it is also what makes the format
// table's binary field mean something rather than describe one entry. Two
// formats the field lets through, and they are not alike - one is a picture
// and one is a script - so a later change that generalised the guard to
// "anything that is not xlsx" or narrowed it to "anything that is a diagram"
// would have two tests to argue with instead of one.
func TestRunExportSVGAllowsATerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/null to stand in for a terminal")
	}
	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer dev.Close()
	if fi, err := dev.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this system", os.DevNull)
	}

	var stderr bytes.Buffer
	if code := run([]string{"export", "svg", "testdata/valid.json", "-o", "-"}, dev, &stderr); code != 0 {
		t.Errorf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunExportSVGIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var runs [2][]byte
	for i := range runs {
		out := filepath.Join(dir, fmt.Sprintf("run%d.svg", i))
		var stdout, stderr bytes.Buffer
		if code := run([]string{"export", "svg", "testdata/valid.json", "-o", out}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
		runs[i] = readSVGDocument(t, out)
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Errorf("two exports differ: %d vs %d bytes", len(runs[0]), len(runs[1]))
	}
}

// readSVGDocument reads path and checks that it is the SVG jjf writes.
func readSVGDocument(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	checkSVGDocument(t, b)
	return b
}

// checkSVGDocument checks that b is the SVG document jjf writes. It is a shape
// check, not a parse, in the same spirit as checkDDLScript: the exporter's own
// tests in internal/export/svg already read the bytes back with encoding/xml
// and make ten statements about the geometry, so what is worth checking out
// here is that the real binary wrote a whole file to the place it was asked to.
func checkSVGDocument(t *testing.T, b []byte) {
	t.Helper()
	if !utf8.Valid(b) {
		t.Fatal("the output is not valid UTF-8")
	}
	src := string(b)
	if !strings.HasPrefix(src, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("the output does not open with the XML declaration:\n%.80s", src)
	}
	if !strings.Contains(src, "<svg ") {
		t.Errorf("the output has no svg element:\n%.200s", src)
	}
	if !strings.HasSuffix(src, "</svg>\n") {
		t.Errorf("the output does not end with a closing svg element and one newline: %q", src[max(0, len(src)-20):])
	}
}

// ---------------------------------------------------------------------------
// The ddl format
// ---------------------------------------------------------------------------

// TestRunExportWritesDDL covers the three positions -o may appear in, as the
// workbook does, and checks that the position cannot change the bytes.
func TestRunExportWritesDDL(t *testing.T) {
	tests := []struct {
		name string
		args func(out string) []string
	}{
		{"flag after operands", func(out string) []string {
			return []string{"export", "ddl", "testdata/valid.json", "-o", out}
		}},
		{"flag before operands", func(out string) []string {
			return []string{"export", "-o", out, "ddl", "testdata/valid.json"}
		}},
		{"flag between operands", func(out string) []string {
			return []string{"export", "ddl", "-o", out, "testdata/valid.json"}
		}},
	}

	var first []byte
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.sql")
			var stdout, stderr bytes.Buffer

			if code := run(tt.args(out), &stdout, &stderr); code != 0 {
				t.Fatalf("run = %d, want 0\nstdout: %s\nstderr: %s", code, &stdout, &stderr)
			}
			if !strings.Contains(stdout.String(), out) {
				t.Errorf("stdout = %q, want it to name %s", stdout.String(), out)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}

			got := readDDLScript(t, out)
			if first == nil {
				first = got
			} else if !bytes.Equal(first, got) {
				t.Error("the same document exported to different bytes depending on flag position")
			}
		})
	}
}

// TestRunExportDDLDefaultsToTheInputPath is the one case where the default
// output path is not the format name: ddl writes a .sql.
func TestRunExportDDLDefaultsToTheInputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "db-design.json")
	raw, err := os.ReadFile("testdata/valid.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "ddl", input}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	readDDLScript(t, filepath.Join(dir, "db-design.sql"))
}

func TestRunExportDDLToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "ddl", "testdata/valid.json", "-o", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	checkDDLScript(t, stdout.Bytes())
}

// TestRunExportDDLAllowsATerminal mirrors TestRunExportSVGAllowsATerminal:
// SQL is text worth reading, so xlsx stays the only format the terminal guard
// applies to.
func TestRunExportDDLAllowsATerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/null to stand in for a terminal")
	}
	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer dev.Close()
	if fi, err := dev.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this system", os.DevNull)
	}

	var stderr bytes.Buffer
	if code := run([]string{"export", "ddl", "testdata/valid.json", "-o", "-"}, dev, &stderr); code != 0 {
		t.Errorf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunExportDDLIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var runs [2][]byte
	for i := range runs {
		out := filepath.Join(dir, fmt.Sprintf("run%d.sql", i))
		var stdout, stderr bytes.Buffer
		if code := run([]string{"export", "ddl", "testdata/valid.json", "-o", out}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
		runs[i] = readDDLScript(t, out)
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Errorf("two exports differ: %d vs %d bytes", len(runs[0]), len(runs[1]))
	}
}

// TestRunExportDDLRefusesAnInconsistentDocument covers the whole refusal: exit
// code 2 because the document is what is wrong, one annotator-parseable line
// per finding, the one-line summary, and no file left behind.
func TestRunExportDDLRefusesAnInconsistentDocument(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sql")
	var stdout, stderr bytes.Buffer

	code := run([]string{"export", "ddl", "testdata/referential_warnings.json", "-o", out}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run = %d, want 2 (invalid input)\nstderr: %s", code, &stderr)
	}
	if want := "testdata/referential_warnings.json: error: "; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want a line beginning %q", stderr.String(), want)
	}
	if want := "jjf: 3 problem(s) prevent PostgreSQL DDL generation"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want the summary %q", stderr.String(), want)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("the refused export left a file at %s", out)
	}
}

// TestExportSVGRendersWhatDDLRefuses is the asymmetry itself, on one fixture.
// A document that contradicts itself still makes a useful diagram - the SVG
// exporter draws the missing foreign key target as a dashed stub - and makes no
// useful SQL at all.
func TestExportSVGRendersWhatDDLRefuses(t *testing.T) {
	dir := t.TempDir()
	const input = "testdata/referential_warnings.json"

	svgOut := filepath.Join(dir, "out.svg")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"export", "svg", input, "-o", svgOut}, &stdout, &stderr); code != 0 {
		t.Fatalf("export svg = %d, want 0\nstderr: %s", code, &stderr)
	}
	readSVGDocument(t, svgOut)

	ddlOut := filepath.Join(dir, "out.sql")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"export", "ddl", input, "-o", ddlOut}, &stdout, &stderr); code != 2 {
		t.Fatalf("export ddl = %d, want 2\nstderr: %s", code, &stderr)
	}
	if _, err := os.Stat(ddlOut); !os.IsNotExist(err) {
		t.Errorf("the refused export left a file at %s", ddlOut)
	}
}

// TestRunExportDDLWritesMySQL is the second dialect reached the way a user
// reaches it: through the CLI, with nothing said on the command line about
// which dialect to write. The document's own database.dbms is the whole of the
// choice.
func TestRunExportDDLWritesMySQL(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sql")
	var stdout, stderr bytes.Buffer

	if code := run([]string{"export", "ddl", "testdata/mysql.json", "-o", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	src := string(readDDLScript(t, out))
	if want := "CREATE TABLE `customers` ("; !strings.Contains(src, want) {
		t.Errorf("the output does not carry %q, so it is not MySQL:\n%s", want, src)
	}
	if strings.Contains(src, `"customers"`) {
		t.Errorf("the output quotes an identifier the PostgreSQL way:\n%s", src)
	}
}

// TestRunExportDDLRefusesAnUnsupportedDBMS reads a SQLite document rather than
// the MySQL one it used to, so that the case keeps testing a refusal as
// dialects are added: the message it pins is the one a reader gets when jjf
// writes no DDL for their target at all.
func TestRunExportDDLRefusesAnUnsupportedDBMS(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sql")
	var stdout, stderr bytes.Buffer

	code := run([]string{"export", "ddl", "testdata/sqlite.json", "-o", out}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run = %d, want 2\nstderr: %s", code, &stderr)
	}
	if want := `jjf: ddl export supports PostgreSQL, MySQL; this document names "SQLite"`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestRunExportDDLRefusesMariaDB has a fixture of its own rather than sharing
// SQLite's, because the two refusals are the same message for opposite reasons.
// Nobody expects jjf to write SQLite DDL; MariaDB takes the DDL MySQL takes, so
// its refusal is the one that has to be deliberate - it has no importer and no
// live-server leg, and design/ddl-export.md's gate forbids shipping a dialect
// on golden files alone.
func TestRunExportDDLRefusesMariaDB(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sql")
	var stdout, stderr bytes.Buffer

	code := run([]string{"export", "ddl", "testdata/mariadb.json", "-o", out}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run = %d, want 2\nstderr: %s", code, &stderr)
	}
	if want := `jjf: ddl export supports PostgreSQL, MySQL; this document names "MariaDB"`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("the refused export left a file at %s", out)
	}
}

func TestRunExportDDLRefusesADocumentWithNoDBMS(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.sql")
	var stdout, stderr bytes.Buffer

	code := run([]string{"export", "ddl", "testdata/nodbms.json", "-o", out}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run = %d, want 2\nstderr: %s", code, &stderr)
	}
	// The sentence offers every value a document may name, so that an author
	// who has to add the field is not told about one dialect and left to find
	// the other.
	if want := `jjf: ddl export needs the document to name its target; add "dbms": "PostgreSQL" or "MySQL" to "database"`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// TestOnlyDDLRefusesFindings pins the asymmetry as a fact of the format table
// rather than as a comment, so that a well-meaning later change has to argue
// with a test.
func TestOnlyDDLRefusesFindings(t *testing.T) {
	for _, f := range exportFormats() {
		if (f.accept != nil) != (f.name == "ddl") {
			t.Errorf("format %q has accept != nil = %v; only ddl refuses a document", f.name, f.accept != nil)
		}
	}
}

// readDDLScript reads path and checks that it is the DDL jjf writes.
func readDDLScript(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	checkDDLScript(t, b)
	return b
}

// checkDDLScript checks that b is the DDL script jjf writes. It is a shape
// check, not a parse: no test may need a database or a SQL parser to run.
func checkDDLScript(t *testing.T, b []byte) {
	t.Helper()
	if !utf8.Valid(b) {
		t.Fatal("the output is not valid UTF-8")
	}
	src := string(b)
	if !strings.HasPrefix(src, "-- Generated by jjf") {
		t.Errorf("the output does not open with the generated-by comment:\n%.80s", src)
	}
	if !strings.Contains(src, "CREATE TABLE ") {
		t.Errorf("the output has no CREATE TABLE statement:\n%s", src)
	}
	if !strings.HasSuffix(src, ";\n") {
		t.Errorf("the output does not end with a statement terminator and one newline: %q", src[max(0, len(src)-20):])
	}
}

// ---------------------------------------------------------------------------
// Usage drift guards
// ---------------------------------------------------------------------------

// TestExportUsageListsEveryFormat keeps the help text and the format table
// from drifting apart.
func TestExportUsageListsEveryFormat(t *testing.T) {
	usage := exportUsage()
	for _, f := range exportFormats() {
		if !strings.Contains(usage, f.name) {
			t.Errorf("the export usage does not name the format %q:\n%s", f.name, usage)
		}
		if !strings.Contains(usage, f.summary) {
			t.Errorf("the export usage does not carry the summary %q:\n%s", f.summary, usage)
		}
	}
}

// TestImportUsageListsEveryDialect keeps the help text and the dialect table
// from drifting apart, exactly as its export counterpart above does. Adding an
// entry to importDialects() and regenerating the goldens is meant to be the
// whole of advertising a third dialect.
func TestImportUsageListsEveryDialect(t *testing.T) {
	usage := importUsage()
	for _, d := range importDialects() {
		if !strings.Contains(usage, d.name) {
			t.Errorf("the import usage does not name the dialect %q:\n%s", d.name, usage)
		}
		if !strings.Contains(usage, d.summary) {
			t.Errorf("the import usage does not carry the summary %q:\n%s", d.summary, usage)
		}
	}
}

// TestEveryImportDialectIsFullyPopulated and TestImportDialectNamesAreDistinct
// pin the shape of the table itself, the way TestOnlyDDLRefusesFindings pins
// the export table's: an entry missing its summary would print a blank line in
// the help, and one missing its imports function would panic at the moment a
// user typed its name.
func TestEveryImportDialectIsFullyPopulated(t *testing.T) {
	for _, d := range importDialects() {
		if d.name == "" {
			t.Error("an import dialect has no name")
		}
		if d.summary == "" {
			t.Errorf("import dialect %q has no summary", d.name)
		}
		if d.imports == nil {
			t.Errorf("import dialect %q has no imports function", d.name)
		}
	}
}

func TestImportDialectNamesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range importDialects() {
		if seen[d.name] {
			t.Errorf("import dialect %q appears twice; lookupImportDialect would never reach the second", d.name)
		}
		seen[d.name] = true
	}
}

// TestOnlyPostgresUsesTheSchemaFlag holds the -schema exception to one dialect.
// The flag has to be registered for all of them, because flags.go permutes
// flags and operands and so the dialect is not known until parsing is done; the
// table is what decides which of them may act on it.
func TestOnlyPostgresUsesTheSchemaFlag(t *testing.T) {
	for _, d := range importDialects() {
		if d.schemaFlag != (d.name == "postgres") {
			t.Errorf("dialect %q has schemaFlag = %v; only postgres has a schema to choose", d.name, d.schemaFlag)
		}
	}
}

// TestRootUsageListsEveryExportFormat guards the one hand-written list of
// formats, which the root usage keeps as a literal.
func TestRootUsageListsEveryExportFormat(t *testing.T) {
	for _, f := range exportFormats() {
		if !strings.Contains(rootUsage, f.name) {
			t.Errorf("the root usage does not name the export format %q:\n%s", f.name, rootUsage)
		}
	}
}

// TestRootUsageListsEveryImportDialect guards the other hand-written list.
func TestRootUsageListsEveryImportDialect(t *testing.T) {
	for _, d := range importDialects() {
		if !strings.Contains(rootUsage, d.name) {
			t.Errorf("the root usage does not name the import dialect %q:\n%s", d.name, rootUsage)
		}
	}
}

func TestGoldenOutput(t *testing.T) {
	tests := []struct {
		golden string
		args   []string
		stream func(stdout, stderr *bytes.Buffer) string
	}{
		{
			golden: "root_usage.txt",
			args:   []string{"--help"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
		},
		{
			golden: "validate_usage.txt",
			args:   []string{"validate", "-h"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
		},
		{
			golden: "export_usage.txt",
			args:   []string{"export", "-h"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
		},
		{
			golden: "import_usage.txt",
			args:   []string{"import", "-h"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
		},
		{
			golden: "import_unsupported_dialect.txt",
			args:   []string{"import", "oracle", "testdata/postgres/schema.sql"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "export_unsupported_format.txt",
			args:   []string{"export", "markdown", "testdata/valid.json"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "export_ddl_refused.txt",
			args:   []string{"export", "ddl", "testdata/referential_warnings.json", "-o", "-"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "validate_ok.txt",
			args:   []string{"validate", "testdata/valid.json"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
		},
		{
			golden: "validate_warnings.txt",
			args:   []string{"validate", "testdata/referential_warnings.json"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "validate_schema_violation.txt",
			args:   []string{"validate", "testdata/schema_violation.json"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "validate_no_input.txt",
			args:   []string{"validate"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "unknown_command.txt",
			args:   []string{"bogus"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.golden, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			run(tt.args, &stdout, &stderr)
			checkGolden(t, tt.golden, tt.stream(&stdout, &stderr))
		})
	}
}

// TestExitCodeOfRealProcess re-executes the test binary as jjf to confirm
// that main really exits with the code run returns. Everything else calls run
// in process, which keeps the tests fast and measurable by the cover tool.
func TestExitCodeOfRealProcess(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"valid document", []string{"validate", "testdata/valid.json"}, 0},
		{"schema violation", []string{"validate", "testdata/schema_violation.json"}, 3},
		{"referential warnings with -strict", []string{"validate", "-strict", "testdata/referential_warnings.json"}, 2},
		// The refusal is a claim about an exit code, which is exactly what
		// this test exists to verify through a real process.
		{"a ddl export the document forbids", []string{"export", "ddl", "testdata/referential_warnings.json", "-o", "-"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], tt.args...)
			cmd.Env = append(os.Environ(), "JJF_TEST_SUBPROCESS=1")
			out, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil {
				t.Fatalf("subprocess did not run: %v", err)
			}
			if got := cmd.ProcessState.ExitCode(); got != tt.want {
				t.Fatalf("exit code = %d, want %d\n%s", got, tt.want, out)
			}
		})
	}
}

// pgFixture names one of the dumps under testdata/postgres.
func pgFixture(name string) string { return filepath.Join("testdata", "postgres", name) }

// myFixture names one of the dumps under testdata/mysql. Every one of them but
// broken.sql is real "mysqldump --no-data" output, and broken.sql says in its
// own header that it is not.
func myFixture(name string) string { return filepath.Join("testdata", "mysql", name) }

// importedDocument reads a generated document back and asserts that it is one
// jjf itself accepts. Nothing the importer writes may fail "jjf validate".
func importedDocument(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the generated document: %v", err)
	}
	assertValidDocument(t, path, raw)
	return raw
}

// assertValidDocument decodes raw and validates it against the embedded schema.
func assertValidDocument(t *testing.T, source string, raw []byte) {
	t.Helper()

	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(source, raw); err != nil {
		var ide *schema.InvalidDocumentError
		if errors.As(err, &ide) {
			var report strings.Builder
			ide.WriteReport(&report)
			t.Fatalf("the generated document does not conform to the schema:\n%s", &report)
		}
		t.Fatalf("validating the generated document: %v", err)
	}
	if _, err := model.Decode(raw); err != nil {
		t.Fatalf("decoding the generated document: %v", err)
	}
}

func TestRunImport(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring
	}{
		{
			name:       "a dump with nothing to warn about",
			args:       []string{"import", "postgres", pgFixture("schema.sql"), "-o", filepath.Join(dir, "ok.json")},
			wantCode:   0,
			wantStdout: "ok.json: written",
		},
		{
			name:       "missing file",
			args:       []string{"import", "postgres", pgFixture("does_not_exist.sql"), "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: "no such file or directory",
		},
		{
			name:       "structurally broken sql",
			args:       []string{"import", "postgres", pgFixture("broken.sql"), "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: "line 7",
		},
		{
			name:       "a dump with no tables",
			args:       []string{"import", "postgres", pgFixture("no_tables.sql"), "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: `no tables found in schema "public"`,
		},
		{
			name:       "a schema the dump does not have",
			args:       []string{"import", "postgres", pgFixture("schema.sql"), "-schema", "audit", "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: `no tables found in schema "audit"`,
		},
		{
			name:       "the database name can be given",
			args:       []string{"import", "postgres", pgFixture("schema.sql"), "-database", "shop", "-o", filepath.Join(dir, "named.json")},
			wantCode:   0,
			wantStdout: "named.json: written",
		},
		{
			name:       "unsupported dialect",
			args:       []string{"import", "oracle", pgFixture("schema.sql")},
			wantCode:   2,
			wantStderr: `unsupported dialect "oracle"`,
		},
		{
			name:       "no operands",
			args:       []string{"import"},
			wantCode:   2,
			wantStderr: "a dialect and exactly one input file, got 0",
		},
		{
			name:       "only a dialect",
			args:       []string{"import", "postgres"},
			wantCode:   2,
			wantStderr: "a dialect and exactly one input file, got 1",
		},
		{
			name:       "three operands",
			args:       []string{"import", "postgres", pgFixture("schema.sql"), "extra"},
			wantCode:   2,
			wantStderr: "a dialect and exactly one input file, got 3",
		},
		{
			name:       "help",
			args:       []string{"import", "-h"},
			wantCode:   0,
			wantStdout: "jjf import <dialect> <input.sql>",
		},
		{
			name:       "undefined flag",
			args:       []string{"import", "postgres", pgFixture("schema.sql"), "-bogus"},
			wantCode:   2,
			wantStderr: "flag provided but not defined: -bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("run = %d, want %d\nstdout: %s\nstderr: %s", code, tt.wantCode, &stdout, &stderr)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", &stdout, tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", &stderr, tt.wantStderr)
			}
		})
	}

	// Nothing above that failed may have left a file behind.
	if _, err := os.Stat(filepath.Join(dir, "x.json")); !os.IsNotExist(err) {
		t.Errorf("a failed import left an output file behind: %v", err)
	}
}

// TestRunImportWritesJSON pins that -o is honoured wherever it appears. The
// standard flag package stops at the first operand, so without the permuting
// parser in flags.go two of these three would silently write nothing.
func TestRunImportWritesJSON(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		args func(out string) []string
	}{
		{
			name: "flag last",
			args: func(out string) []string {
				return []string{"import", "postgres", pgFixture("schema.sql"), "-o", out}
			},
		},
		{
			name: "flag first",
			args: func(out string) []string {
				return []string{"import", "-o", out, "postgres", pgFixture("schema.sql")}
			},
		},
		{
			name: "flag between the operands",
			args: func(out string) []string {
				return []string{"import", "postgres", "-o", out, pgFixture("schema.sql")}
			},
		},
	}

	var first []byte
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(dir, tt.name+".json")
			var stdout, stderr bytes.Buffer
			if code := run(tt.args(out), &stdout, &stderr); code != 0 {
				t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", &stderr)
			}

			got := importedDocument(t, out)
			if first == nil {
				first = got
			} else if !bytes.Equal(first, got) {
				t.Error("the same dump imported to different bytes depending on flag position")
			}
		})
	}
}

func TestRunImportDefaultsToTheInputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "db-dump.sql")
	raw, err := os.ReadFile(pgFixture("schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	// The file name is not a legal identifier, so the database name has to come
	// from the flag.
	if code := run([]string{"import", "postgres", input, "-database", "shop"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	importedDocument(t, filepath.Join(dir, "db-dump.json"))
}

func TestRunImportToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"import", "postgres", pgFixture("schema.sql"), "-o", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", &stderr)
	}
	assertValidDocument(t, "standard output", stdout.Bytes())
}

func TestRunImportStrict(t *testing.T) {
	const wantWarnings = 3

	tests := []struct {
		name      string
		strict    bool
		wantCode  int
		wantWrite bool
	}{
		{name: "warnings are not fatal by default", wantCode: 0, wantWrite: true},
		{name: "strict turns warnings into a failure", strict: true, wantCode: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.json")
			args := []string{"import", "postgres", pgFixture("warnings.sql"), "-o", out}
			if tt.strict {
				args = append(args, "-strict")
			}

			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("run = %d, want %d\nstderr: %s", code, tt.wantCode, &stderr)
			}
			// The warnings are printed either way: -strict changes what happens
			// next, not what the user is told.
			if got := strings.Count(stderr.String(), ": warning: "); got != wantWarnings {
				t.Errorf("warnings on stderr = %d, want %d\nstderr: %s", got, wantWarnings, &stderr)
			}
			if !strings.Contains(stderr.String(), "warnings.sql:14: warning: ") {
				t.Errorf("stderr = %q, want a warning naming line 14", &stderr)
			}

			_, err := os.Stat(out)
			if tt.wantWrite {
				if err != nil {
					t.Fatalf("output file: %v", err)
				}
				importedDocument(t, out)
				return
			}
			if !os.IsNotExist(err) {
				t.Errorf("a strict run wrote an output file anyway: %v", err)
			}
		})
	}
}

// TestRunImportMySQL is TestRunImport's second dialect. The cases that differ
// between the two are the ones here; the ones that do not - a missing file, a
// bad flag, the wrong number of operands - are dialect-independent and are
// covered once, above.
func TestRunImportMySQL(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring
		wantStderr string // substring
	}{
		{
			name:       "a dump with nothing to warn about",
			args:       []string{"import", "mysql", myFixture("schema.sql"), "-o", filepath.Join(dir, "ok.json")},
			wantCode:   0,
			wantStdout: "ok.json: written",
		},
		{
			name:       "structurally broken sql",
			args:       []string{"import", "mysql", myFixture("broken.sql"), "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: "line 18",
		},
		{
			name:       "a dump holding a view and a routine and no table",
			args:       []string{"import", "mysql", myFixture("no_tables.sql"), "-o", filepath.Join(dir, "x.json")},
			wantCode:   2,
			wantStderr: "no tables found in the dump",
		},
		{
			name:       "the database name can be given",
			args:       []string{"import", "mysql", myFixture("schema.sql"), "-database", "storefront", "-o", filepath.Join(dir, "named.json")},
			wantCode:   0,
			wantStdout: "named.json: written",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("run = %d, want %d\nstdout: %s\nstderr: %s", code, tt.wantCode, &stdout, &stderr)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", &stdout, tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", &stderr, tt.wantStderr)
			}
		})
	}

	// The document a successful run wrote has to be one jjf itself accepts.
	importedDocument(t, filepath.Join(dir, "ok.json"))

	// Nothing above that failed may have left a file behind.
	if _, err := os.Stat(filepath.Join(dir, "x.json")); !os.IsNotExist(err) {
		t.Errorf("a failed import left an output file behind: %v", err)
	}
}

// TestRunImportMySQLKeepsItsJapaneseText is a trap this repository has already
// fallen into once and must not fall into again: a mysqldump taken over a
// connection that defaulted to latin1 encodes every Japanese comment twice, and
// the result still round-trips, so no round-trip test catches it. The committed
// dumps are taken with --default-character-set=utf8mb4 for that reason, and
// this is what says so out loud.
func TestRunImportMySQLKeepsItsJapaneseText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"import", "mysql", myFixture("schema.sql"), "-o", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	for _, want := range []string{"ユーザー", "メールアドレス", "注文"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the generated document does not contain %q; the dump may have been captured as mojibake", want)
		}
	}
}

func TestRunImportMySQLToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"import", "mysql", myFixture("schema.sql"), "-o", "-"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", &stderr)
	}
	assertValidDocument(t, "standard output", stdout.Bytes())
}

// TestRunImportMySQLAndTheSchemaFlag asserts BOTH directions, because the
// mechanism is FlagSet.Visit and only a test of both would notice a later
// change that gave -schema a different default. Refused when it was set,
// accepted when it was left alone.
func TestRunImportMySQLAndTheSchemaFlag(t *testing.T) {
	t.Run("explicitly set is an error", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "out.json")
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "mysql", myFixture("schema.sql"), "-schema", "public", "-o", out}, &stdout, &stderr); code != 2 {
			t.Fatalf("run = %d, want 2\nstderr: %s", code, &stderr)
		}
		if want := "a mysql dump holds one database"; !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", &stderr, want)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("a refused import wrote an output file anyway: %v", err)
		}
	})

	// Even the flag's own default value, passed explicitly, is refused: what is
	// wrong is asking for a schema at all, not asking for the wrong one.
	t.Run("the same value the default holds is still an error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "mysql", myFixture("schema.sql"), "-schema", postgres.DefaultSchema, "-o", "-"}, &stdout, &stderr); code != 2 {
			t.Errorf("run = %d, want 2\nstderr: %s", code, &stderr)
		}
	})

	t.Run("left alone it is ignored", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "mysql", myFixture("schema.sql"), "-o", "-"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
		assertValidDocument(t, "standard output", stdout.Bytes())
	})

	// postgres keeps the flag, which is the other half of the claim.
	t.Run("postgres still takes it", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "postgres", pgFixture("schema.sql"), "-schema", "public", "-o", "-"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
	})
}

// TestRunImportMySQLStrict mirrors TestRunImportStrict for the second dialect:
// the warnings are printed either way, and -strict changes what happens next
// rather than what the user is told.
func TestRunImportMySQLStrict(t *testing.T) {
	const wantWarnings = 3

	tests := []struct {
		name      string
		strict    bool
		wantCode  int
		wantWrite bool
	}{
		{name: "warnings are not fatal by default", wantCode: 0, wantWrite: true},
		{name: "strict turns warnings into a failure", strict: true, wantCode: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.json")
			args := []string{"import", "mysql", myFixture("warnings.sql"), "-o", out}
			if tt.strict {
				args = append(args, "-strict")
			}

			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("run = %d, want %d\nstderr: %s", code, tt.wantCode, &stderr)
			}
			if got := strings.Count(stderr.String(), ": warning: "); got != wantWarnings {
				t.Errorf("warnings on stderr = %d, want %d\nstderr: %s", got, wantWarnings, &stderr)
			}
			if !strings.Contains(stderr.String(), "warnings.sql:29: warning: ") {
				t.Errorf("stderr = %q, want a warning naming line 29", &stderr)
			}

			_, err := os.Stat(out)
			if tt.wantWrite {
				if err != nil {
					t.Fatalf("output file: %v", err)
				}
				importedDocument(t, out)
				return
			}
			if !os.IsNotExist(err) {
				t.Errorf("a strict run wrote an output file anyway: %v", err)
			}
		})
	}
}

// TestRunImportMySQLThenValidateAndExport walks the whole journey for a MySQL
// document, and the last step is the one that matters: the DDL exporter has to
// accept what the importer just wrote. A document jjf produced and jjf then
// refused would be the worst outcome the pair could have.
func TestRunImportMySQLThenValidateAndExport(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "db-design.json")
	book := filepath.Join(dir, "db-design.xlsx")
	script := filepath.Join(dir, "db-design.sql")

	steps := []struct {
		name string
		args []string
	}{
		{name: "import", args: []string{"import", "mysql", myFixture("schema.sql"), "-o", doc}},
		{name: "validate", args: []string{"validate", doc, "-strict"}},
		{name: "export", args: []string{"export", "xlsx", doc, "-o", book}},
		{name: "export ddl", args: []string{"export", "ddl", doc, "-o", script}},
	}
	for _, step := range steps {
		var stdout, stderr bytes.Buffer
		if code := run(step.args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s = %d, want 0\nstderr: %s", step.name, code, &stderr)
		}
	}
	readWorkbook(t, book)
	scriptBytes := readDDLScript(t, script)
	if !bytes.Contains(scriptBytes, []byte("CREATE TABLE `orders`")) {
		t.Errorf("the exported script is not MySQL DDL:\n%s", scriptBytes)
	}
}

func TestRunImportIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var runs [2][]byte
	for i := range runs {
		out := filepath.Join(dir, fmt.Sprintf("run%d.json", i))
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "postgres", pgFixture("schema.sql"), "-o", out}, &stdout, &stderr); code != 0 {
			t.Fatalf("run = %d, want 0\nstderr: %s", code, &stderr)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		runs[i] = raw
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Errorf("two imports of the same dump differ:\n%s\n%s", runs[0], runs[1])
	}
}

// TestRunImportThenValidateAndExport walks the journey the importer exists for:
// a dump becomes a document, the document passes validation, and the document
// renders as a workbook and as DDL. The last step is the closest this test
// suite comes to the round trip, because an imported document is the most
// realistic input the DDL exporter will ever see.
func TestRunImportThenValidateAndExport(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "db-design.json")
	book := filepath.Join(dir, "db-design.xlsx")
	script := filepath.Join(dir, "db-design.sql")

	steps := []struct {
		name string
		args []string
	}{
		{name: "import", args: []string{"import", "postgres", pgFixture("schema.sql"), "-o", doc}},
		{name: "validate", args: []string{"validate", doc}},
		{name: "export", args: []string{"export", "xlsx", doc, "-o", book}},
		{name: "export ddl", args: []string{"export", "ddl", doc, "-o", script}},
	}
	for _, step := range steps {
		var stdout, stderr bytes.Buffer
		if code := run(step.args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s = %d, want 0\nstderr: %s", step.name, code, &stderr)
		}
	}
	readWorkbook(t, book)
	readDDLScript(t, script)
}

func TestRunImportUnwritableOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "no-such-dir", "out.json")
	var stdout, stderr bytes.Buffer

	code := run([]string{"import", "postgres", pgFixture("schema.sql"), "-o", out}, &stdout, &stderr)
	if code != 4 {
		t.Errorf("run = %d, want 4 (output generation error)\nstderr: %s", code, &stderr)
	}
	if !strings.Contains(stderr.String(), out) {
		t.Errorf("stderr = %q, want it to name the requested output path %s", &stderr, out)
	}
}

// TestWriteDiagnosticsShapesALineLessWarning pins both shapes of an importer
// warning. The source:line: prefix is the one editors and CI annotators already
// parse, so its punctuation is a published interface rather than a formatting
// preference, and a diagnostic with no line has to drop the number without
// leaving the colon behind.
//
// No importer emits a zero-line diagnostic today. The branch exists because the
// Diagnostic types in both importers document one - "pass 0 when no line
// applies" - for the resolve-time warning that has no place in the file to
// point at, and this is the only thing holding the CLI's half of that.
func TestWriteDiagnosticsShapesALineLessWarning(t *testing.T) {
	var buf bytes.Buffer
	writeDiagnostics(&buf, "schema.sql", []warning{
		{Line: 3, Message: "first"},
		{Message: "second"},
		{Line: 0, Message: "third"},
	})
	want := "schema.sql:3: warning: first\n" +
		"schema.sql: warning: second\n" +
		"schema.sql: warning: third\n"
	if got := buf.String(); got != want {
		t.Errorf("writeDiagnostics wrote %q, want %q", got, want)
	}
}

// TestWriteFileAtomicallyLeavesNothingBehindWhenTheWriteFails holds the promise
// the whole helper exists for: a failure part way through leaves no half
// written document behind. The exporters write straight into the writer they
// are handed, so a failure at the tenth of twenty sheets is an ordinary
// outcome, and what must not survive it is a file - either at the destination
// or beside it under a temporary name.
//
// Asserting that the DIRECTORY is empty is the load-bearing half. A version
// that deleted the destination and forgot the temporary file would pass an
// assertion about the destination alone, and would leave a stray dot-file next
// to every failed export.
func TestWriteFileAtomicallyLeavesNothingBehindWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	boom := errors.New("boom")

	err := writeFileAtomically(path, func(io.Writer) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("error got = %v, want the one the writer returned", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat %s got = %v, want the file not to exist", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the output directory holds %v, want it empty", names)
	}
}

// TestWriteFileAtomicallyReportsARenameFailure covers the last thing that can
// go wrong, and the one a user is most likely to cause: naming an existing
// directory as the output file. The write itself succeeds, so the failure
// arrives at the rename, and it has to come back with the exit code that means
// the output could not be written rather than as a bare error.
func TestWriteFileAtomicallyReportsARenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// A non-empty directory, because renaming onto an EMPTY one succeeds on
	// some systems and the test is about the failure.
	if err := os.WriteFile(filepath.Join(path, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := writeFileAtomically(path, func(w io.Writer) error {
		_, err := w.Write([]byte("body"))
		return err
	})
	if err == nil {
		t.Fatal("writeFileAtomically returned no error, want one about the rename")
	}
	if got := exitcode.Of(err); got != exitcode.OutputFailed {
		t.Errorf("exit code got = %v, want %v", got, exitcode.OutputFailed)
	}
	if !strings.Contains(err.Error(), "rename output file") {
		t.Errorf("error got = %q, want it to name the rename", err.Error())
	}
	// The temporary file is still cleaned up on this path, which is what keeps
	// a mistyped -o from littering the directory it named.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "out.xlsx" {
		t.Errorf("the output directory holds %v, want only the directory that was already there", entries)
	}
}
