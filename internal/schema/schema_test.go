package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

var update = flag.Bool("update", false, "update golden files")

// newTestValidator builds one validator for a test to share.
func newTestValidator(t *testing.T) *Validator {
	t.Helper()
	v, err := NewValidator()
	if err != nil {
		t.Fatalf("NewValidator() failed: %v", err)
	}
	return v
}

// readFixture reads a fixture verbatim, byte order mark included.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// invalidDoc extracts the aggregated schema violation from err.
func invalidDoc(t *testing.T, err error) *InvalidDocumentError {
	t.Helper()
	var ide *InvalidDocumentError
	if !errors.As(err, &ide) {
		t.Fatalf("error %v is not an *InvalidDocumentError", err)
	}
	return ide
}

func TestNewValidatorWorksOffline(t *testing.T) {
	// denyLoader refuses every fetch, so a successful compile proves the
	// embedded schema is self-contained.
	if _, err := NewValidator(); err != nil {
		t.Fatalf("NewValidator() = %v, want nil", err)
	}
}

func TestNewValidatorIsRepeatable(t *testing.T) {
	// Each call must use a fresh compiler; reusing one would fail with
	// *jsonschema.ResourceExistsError on the second registration.
	for i := 0; i < 3; i++ {
		if _, err := NewValidator(); err != nil {
			t.Fatalf("NewValidator() call %d = %v, want nil", i, err)
		}
	}
}

func TestValidateAcceptsValidDocuments(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "valid", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures in testdata/valid")
	}

	v := newTestValidator(t)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			if err := v.Validate(file, readFixture(t, file)); err != nil {
				var ide *InvalidDocumentError
				if errors.As(err, &ide) {
					var buf bytes.Buffer
					ide.WriteReport(&buf)
					t.Fatalf("Validate() rejected a valid document:\n%s", buf.String())
				}
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// TestValidateAcceptsExampleDocument guards examples/db-design.example.json.
//
// The example is what people and AI agents copy from, so a sample that has
// drifted away from the schema teaches the wrong document shape. The CI
// selfcheck job runs the same check through the built binary; this test makes it
// fail during "go test" as well, without needing a build.
func TestValidateAcceptsExampleDocument(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "db-design.example.json")

	v := newTestValidator(t)
	err := v.Validate(path, readFixture(t, path))
	if err == nil {
		return
	}
	var ide *InvalidDocumentError
	if errors.As(err, &ide) {
		var buf bytes.Buffer
		ide.WriteReport(&buf)
		t.Fatalf("the shipped example no longer conforms to the schema:\n%s", buf.String())
	}
	t.Fatalf("Validate() = %v, want nil", err)
}

func TestValidateReportsSyntaxErrorAsInvalidInput(t *testing.T) {
	const file = "testdata/invalid/broken_syntax.json"

	v := newTestValidator(t)
	err := v.Validate(file, readFixture(t, file))
	if err == nil {
		t.Fatal("Validate() = nil, want a syntax error")
	}
	if got, want := exitcode.Of(err), exitcode.InvalidInput; got != want {
		t.Errorf("exitcode.Of(err) = %d, want %d", got, want)
	}

	var ide *InvalidDocumentError
	if errors.As(err, &ide) {
		t.Error("a syntax error must not be reported as a schema violation")
	}

	var se *json.SyntaxError
	if !errors.As(err, &se) {
		t.Errorf("error %v does not unwrap to *json.SyntaxError", err)
	}
	if msg := err.Error(); !bytes.Contains([]byte(msg), []byte("line ")) {
		t.Errorf("error %q does not locate the failure", msg)
	}
}

// wantIssue is one expected violation: an exact JSON Pointer and a substring
// of the message.
type wantIssue struct {
	pointer string
	message string
}

func TestValidateRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		file     string
		wantCode exitcode.Code
		// want is nil when the document never reaches the schema, which is the
		// case for malformed JSON.
		want []wantIssue
	}{
		{
			file:     "broken_syntax.json",
			wantCode: exitcode.InvalidInput,
		},
		{
			file:     "root_not_object.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"", "got array, want object"}},
		},
		{
			file:     "missing_required_column.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/columns/0", "missing property 'nullable'"}},
		},
		{
			file:     "missing_required_table.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0", "missing property 'logicalName'"}},
		},
		{
			file:     "wrong_type_length.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/columns/0/length", "got string, want integer"}},
		},
		{
			file:     "bad_enum_dbms.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/database/dbms", "value must be one of 'PostgreSQL'"}},
		},
		{
			file:     "bad_enum_ondelete.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/1/foreignKeys/0/onDelete", "value must be one of 'CASCADE'"}},
		},
		{
			file:     "unknown_property_table.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0", "additional properties 'engine' not allowed"}},
		},
		{
			file:     "unknown_property_column.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/columns/0", "additional properties 'comment' not allowed"}},
		},
		{
			file:     "bad_pattern_name.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/name", `'order-lines' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'`}},
		},
		{
			file:     "bad_pattern_type.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/columns/0/type", `'VARCHAR(30)' does not match pattern '^[A-Za-z][A-Za-z0-9_ ]*$'`}},
		},
		{
			// The message must show a single backslash: a doubled one is a
			// different regular expression.
			file:     "bad_pattern_version.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/formatVersion", `'1' does not match pattern '^[0-9]+\.[0-9]+$'`}},
		},
		{
			file:     "empty_key_columns.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/primaryKey/columns", "minItems: got 0, want 1"}},
		},
		{
			file:     "duplicate_key_columns.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/primaryKey/columns", "items at 0 and 1 are equal"}},
		},
		{
			file:     "scale_without_precision.json",
			wantCode: exitcode.SchemaFailed,
			want:     []wantIssue{{"/tables/0/columns/0", "properties 'precision' required, if 'scale' exists"}},
		},
		{
			file:     "numeric_order.json",
			wantCode: exitcode.SchemaFailed,
			want: []wantIssue{
				{"/tables/0/columns/2", "missing property 'nullable'"},
				{"/tables/0/columns/10", "missing property 'nullable'"},
			},
		},
		{
			// Every violation must be reported in one pass: validation does not
			// stop at the first failure.
			file:     "multi_error.json",
			wantCode: exitcode.SchemaFailed,
			want: []wantIssue{
				{"/database/dbms", "value must be one of 'PostgreSQL'"},
				{"/tables/0", "missing property 'logicalName'"},
				{"/tables/0/columns/0", "missing property 'nullable'"},
				{"/tables/0/columns/1/logicalName", "minLength: got 0, want 1"},
				{"/tables/0/columns/1/name", `'9bad' does not match pattern`},
				{"/tables/0/columns/1/nullable", "got string, want boolean"},
				{"/tables/0/indexes/0", "missing property 'name'"},
			},
		},
	}

	v := newTestValidator(t)
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("testdata", "invalid", tt.file)
			err := v.Validate(tt.file, readFixture(t, path))
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if got := exitcode.Of(err); got != tt.wantCode {
				t.Errorf("exitcode.Of(err) = %d, want %d", got, tt.wantCode)
			}

			if tt.want == nil {
				var ide *InvalidDocumentError
				if errors.As(err, &ide) {
					t.Errorf("got a schema report for a malformed document: %v", err)
				}
				return
			}

			ide := invalidDoc(t, err)
			if len(ide.Issues) != len(tt.want) {
				var buf bytes.Buffer
				ide.WriteReport(&buf)
				t.Fatalf("got %d issue(s), want %d:\n%s", len(ide.Issues), len(tt.want), buf.String())
			}
			for i, w := range tt.want {
				got := ide.Issues[i]
				if got.Pointer != w.pointer {
					t.Errorf("issue %d pointer = %q, want %q", i, got.Pointer, w.pointer)
				}
				if got.Message == "" {
					t.Errorf("issue %d has an empty message", i)
				}
				if !strings.Contains(got.Message, w.message) {
					t.Errorf("issue %d message = %q, want it to contain %q", i, got.Message, w.message)
				}
			}
		})
	}
}

func TestValidateOrdersIssuesNumerically(t *testing.T) {
	const file = "testdata/invalid/numeric_order.json"

	v := newTestValidator(t)
	ide := invalidDoc(t, v.Validate(file, readFixture(t, file)))

	var got []string
	for _, is := range ide.Issues {
		got = append(got, is.Pointer)
	}
	want := []string{"/tables/0/columns/2", "/tables/0/columns/10"}
	if len(got) != len(want) {
		t.Fatalf("pointers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pointers = %v, want %v", got, want)
		}
	}
}

func TestWriteReportGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "invalid", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures in testdata/invalid")
	}

	v := newTestValidator(t)
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			err := v.Validate(name, readFixture(t, file))
			if err == nil {
				t.Fatal("Validate() = nil, want a violation")
			}

			var ide *InvalidDocumentError
			if !errors.As(err, &ide) {
				// Malformed documents never reach the schema, so they have no
				// report to compare.
				return
			}

			var buf bytes.Buffer
			ide.WriteReport(&buf)

			golden := filepath.Join("testdata", "golden", name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run go test -update to create it): %v", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("report mismatch\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
			}
		})
	}
}

func TestStripBOM(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"with bom", []byte("\xef\xbb\xbf{}"), []byte("{}")},
		{"without bom", []byte("{}"), []byte("{}")},
		{"bom only", []byte("\xef\xbb\xbf"), []byte("")},
		{"bom not at start is kept", []byte("{\xef\xbb\xbf}"), []byte("{\xef\xbb\xbf}")},
		{"empty", []byte(""), []byte("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripBOM(tt.in); !bytes.Equal(got, tt.want) {
				t.Errorf("StripBOM(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLessPointer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"/tables/0/columns/2", "/tables/0/columns/10", true},
		{"/tables/0/columns/10", "/tables/0/columns/2", false},
		{"/tables/2", "/tables/10", true},
		{"/database/dbms", "/tables/0", true},
		{"", "/database/name", true},
		{"/tables/0", "/tables/0/columns/0", true},
		{"/tables/0/columns/0", "/tables/0", false},
		{"/tables/0/columns/1/name", "/tables/0/columns/1/nullable", true},
		{"/tables/0", "/tables/0", false},
	}

	for _, tt := range tests {
		if got := lessPointer(tt.a, tt.b); got != tt.want {
			t.Errorf("lessPointer(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestLineColumn(t *testing.T) {
	raw := []byte("{\n  \"a\": 1,\n}\n")
	tests := []struct {
		offset    int64
		line, col int
		name      string
	}{
		{0, 1, 1, "start"},
		{2, 2, 1, "second line start"},
		{12, 3, 1, "third line start"},
		{-5, 1, 1, "negative offset is clamped"},
		{9999, 4, 1, "past end is clamped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := lineColumn(raw, tt.offset)
			if line != tt.line || col != tt.col {
				t.Errorf("lineColumn(raw, %d) = (%d, %d), want (%d, %d)", tt.offset, line, col, tt.line, tt.col)
			}
		})
	}
}

func TestInvalidDocumentErrorSummaryIsOneLine(t *testing.T) {
	ide := &InvalidDocumentError{
		Source: "db.json",
		Issues: []Issue{{Pointer: "/tables/0", Message: "missing property 'name'"}},
	}
	if got, want := ide.Error(), "db.json: 1 schema violation(s)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if bytes.Contains([]byte(ide.Error()), []byte("\n")) {
		t.Error("Error() must stay on one line")
	}
}

func TestWriteReportLabelsDocumentRoot(t *testing.T) {
	ide := &InvalidDocumentError{
		Source: "db.json",
		Issues: []Issue{{Pointer: "", Message: "got array, want object"}},
	}
	var buf bytes.Buffer
	ide.WriteReport(&buf)
	if !bytes.Contains(buf.Bytes(), []byte("(document root)")) {
		t.Errorf("report does not label the root:\n%s", buf.String())
	}
}
