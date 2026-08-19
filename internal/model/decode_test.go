package model

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

// validCorpus is the set of known-good documents. It deliberately points at the
// fixtures internal/schema validates rather than at a copy: one corpus is what
// makes the drift check below meaningful, because a property added to the schema
// then has exactly one place where it must be exercised.
const validCorpus = "../schema/testdata/valid/*.json"

// withoutBOM strips the byte order mark the way Decode does, so that tests
// which bypass Decode can read the same fixtures.
func withoutBOM(b []byte) []byte { return bytes.TrimPrefix(b, utf8BOM) }

// docWithColumn builds a minimal document whose single column carries the given
// extra JSON fields.
func docWithColumn(columnFields string) string {
	return `{
	  "formatVersion": "1.0",
	  "database": {"name": "shop"},
	  "tables": [
	    {
	      "name": "customers",
	      "logicalName": "顧客",
	      "columns": [
	        {"name": "c", "logicalName": "列", "type": "TEXT", "nullable": true` + columnFields + `}
	      ]
	    }
	  ]
	}`
}

// firstColumn decodes doc and returns its single column.
func firstColumn(t *testing.T, doc string) Column {
	t.Helper()
	d, err := Decode([]byte(doc))
	if err != nil {
		t.Fatalf("Decode() = %v, want nil", err)
	}
	if len(d.Tables) != 1 || len(d.Tables[0].Columns) != 1 {
		t.Fatalf("fixture shape changed: %d table(s)", len(d.Tables))
	}
	return d.Tables[0].Columns[0]
}

func TestDecodeDistinguishesDefaultStates(t *testing.T) {
	tests := []struct {
		name    string
		fields  string
		wantNil bool
		want    string
	}{
		{"absent means no DEFAULT clause", ``, true, ""},
		{"empty string means DEFAULT ''", `, "default": ""`, false, ""},
		{"NULL means DEFAULT NULL", `, "default": "NULL"`, false, "NULL"},
		{"expression is kept verbatim", `, "default": "CURRENT_TIMESTAMP"`, false, "CURRENT_TIMESTAMP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := firstColumn(t, docWithColumn(tt.fields))
			if tt.wantNil {
				if col.Default != nil {
					t.Fatalf("Default = %q, want nil", *col.Default)
				}
				return
			}
			if col.Default == nil {
				t.Fatalf("Default = nil, want %q", tt.want)
			}
			if *col.Default != tt.want {
				t.Errorf("*Default = %q, want %q", *col.Default, tt.want)
			}
		})
	}
}

func TestDecodeDistinguishesAbsentNumbersFromZero(t *testing.T) {
	absent := firstColumn(t, docWithColumn(``))
	if absent.Length != nil || absent.Precision != nil || absent.Scale != nil {
		t.Errorf("absent numeric attributes must decode to nil, got %v %v %v",
			absent.Length, absent.Precision, absent.Scale)
	}

	zeroScale := firstColumn(t, docWithColumn(`, "precision": 5, "scale": 0`))
	if zeroScale.Scale == nil {
		t.Fatal("Scale = nil, want a pointer to 0")
	}
	if *zeroScale.Scale != 0 {
		t.Errorf("*Scale = %d, want 0", *zeroScale.Scale)
	}
	if zeroScale.Precision == nil || *zeroScale.Precision != 5 {
		t.Errorf("Precision = %v, want a pointer to 5", zeroScale.Precision)
	}
}

func TestDecodeRoundTripPreservesMeaning(t *testing.T) {
	const src = `{
	  "formatVersion": "1.0",
	  "database": {"name": "shop", "dbms": "PostgreSQL"},
	  "tables": [
	    {
	      "name": "order_lines",
	      "logicalName": "受注明細",
	      "columns": [
	        {"name": "line_no", "logicalName": "明細番号", "type": "SMALLINT", "nullable": false, "autoIncrement": false},
	        {"name": "note", "logicalName": "備考", "type": "TEXT", "nullable": true, "default": ""}
	      ],
	      "primaryKey": {"columns": ["line_no"]},
	      "indexes": [{"name": "ix_note", "columns": ["note"], "unique": false}]
	    }
	  ]
	}`

	first, err := Decode([]byte(src))
	if err != nil {
		t.Fatalf("Decode() = %v, want nil", err)
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}

	// A false autoIncrement and a false unique carry no information the schema
	// does not already default, so they drop out of the encoding.
	for _, key := range []string{`"autoIncrement"`, `"unique"`} {
		if bytes.Contains(encoded, []byte(key)) {
			t.Errorf("re-encoded document still carries %s: %s", key, encoded)
		}
	}
	// A DEFAULT '' does carry information, so it must survive.
	if !bytes.Contains(encoded, []byte(`"default":""`)) {
		t.Errorf(`re-encoded document lost "default":"": %s`, encoded)
	}

	second, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(re-encoded) = %v, want nil", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("round trip changed the document:\nfirst  = %+v\nsecond = %+v", first, second)
	}
}

func TestDecodeChecksFormatVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{"current", "1.0", false},
		{"future minor is accepted", "1.7", false},
		{"future major is rejected", "2.0", true},
		{"missing minor is rejected", "1", true},
		{"empty is rejected", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := `{"formatVersion": "` + tt.version + `", "database": {"name": "d"}, "tables": []}`
			_, err := Decode([]byte(doc))
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Decode() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Decode() = nil, want an error")
			}
			if !strings.Contains(err.Error(), "unsupported formatVersion") {
				t.Errorf("error = %q, want it to mention unsupported formatVersion", err)
			}
			if !strings.Contains(err.Error(), "upgrade jjf") {
				t.Errorf("error = %q, want it to say what to do", err)
			}
			if got, want := exitcode.Of(err), exitcode.InvalidInput; got != want {
				t.Errorf("exitcode.Of(err) = %d, want %d", got, want)
			}
		})
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	_, err := Decode([]byte(`{"formatVersion": "1.0",}`))
	if err == nil {
		t.Fatal("Decode() = nil, want a syntax error")
	}
	if got, want := exitcode.Of(err), exitcode.InvalidInput; got != want {
		t.Errorf("exitcode.Of(err) = %d, want %d", got, want)
	}
}

func TestDecodeStripsBOM(t *testing.T) {
	doc := append([]byte("\xef\xbb\xbf"), []byte(docWithColumn(``))...)
	if _, err := Decode(doc); err != nil {
		t.Fatalf("Decode() rejected a document with a byte order mark: %v", err)
	}
}

func TestDecodeAcceptsEveryValidFixture(t *testing.T) {
	files := corpus(t)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode() = %v, want nil", err)
			}
			if doc.FormatVersion == "" {
				t.Error("FormatVersion is empty")
			}
			if doc.Database.Name == "" {
				t.Error("Database.Name is empty")
			}
			if len(doc.Tables) == 0 {
				t.Error("Tables is empty")
			}
		})
	}
}

// TestModelMatchesSchema fails when the schema grows a property that the Go
// model does not have. DisallowUnknownFields is a test-only tool here: using it
// on the production path would make the Go struct a second source of truth and
// break forward compatibility with newer documents.
func TestModelMatchesSchema(t *testing.T) {
	for _, file := range corpus(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(bytes.NewReader(withoutBOM(raw)))
			dec.DisallowUnknownFields()
			var doc Document
			if err := dec.Decode(&doc); err != nil {
				t.Fatalf("model drifted from schema: %v", err)
			}
		})
	}
}

// corpus lists the shared valid fixtures.
func corpus(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(validCorpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no fixtures matched %s", validCorpus)
	}
	return files
}
