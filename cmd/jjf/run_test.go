package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
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
			golden: "export_unsupported_format.txt",
			args:   []string{"export", "markdown", "testdata/valid.json"},
			stream: func(_, stderr *bytes.Buffer) string { return stderr.String() },
		},
		{
			golden: "validate_ok.txt",
			args:   []string{"validate", "testdata/valid.json"},
			stream: func(stdout, _ *bytes.Buffer) string { return stdout.String() },
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
