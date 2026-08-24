package schema

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// evaluateDoc runs one inline document through the embedded schema and returns
// the issues it produced, in the order the walk appended them. Writing the
// documents inline keeps each one next to the message it pins; testdata/invalid
// is for fixtures the golden report covers.
func evaluateDoc(t *testing.T, doc string) []Issue {
	t.Helper()
	root, err := compileSchema(schemadata.DBDesign)
	if err != nil {
		t.Fatalf("compileSchema(DBDesign) = %v, want nil", err)
	}
	inst, err := decodeInstance([]byte(doc))
	if err != nil {
		t.Fatalf("decodeInstance(%s) = %v, want nil", doc, err)
	}
	var issues []Issue
	root.evaluate("", inst, &issues)
	return issues
}

// wrap puts a column into the smallest document that reaches it, so that a
// test case is the one property it is about and not thirty lines of scaffolding.
func wrap(column string) string {
	return `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[` +
		`{"name":"customers","logicalName":"顧客","columns":[` + column + `]}]}`
}

func TestDecodeInstanceKeepsNumbersExact(t *testing.T) {
	// float64 has 53 bits of mantissa, so this integer is rounded the moment a
	// decoder without UseNumber touches it - and "type": "integer" would then
	// be answering about a number the document does not contain.
	const doc = `{"a":10000000000000000001}`

	inst, err := decodeInstance([]byte(doc))
	if err != nil {
		t.Fatalf("decodeInstance(%s) = %v, want nil", doc, err)
	}
	obj, ok := inst.(map[string]any)
	if !ok {
		t.Fatalf("decodeInstance returned %T, want map[string]any", inst)
	}
	num, ok := obj["a"].(json.Number)
	if !ok {
		t.Fatalf("obj[\"a\"] is %T, want json.Number", obj["a"])
	}
	if got, want := num.String(), "10000000000000000001"; got != want {
		t.Errorf("number = %q, want %q", got, want)
	}
}

func TestDecodeInstanceRefusesTrailingContent(t *testing.T) {
	t.Run("a second document in the same file", func(t *testing.T) {
		_, err := decodeInstance([]byte(`{} {}`))
		if err == nil {
			t.Fatal("decodeInstance = nil, want an error")
		}
		if got, want := err.Error(), "invalid character after top-level value"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("trailing whitespace is fine", func(t *testing.T) {
		if _, err := decodeInstance([]byte("{}  \n")); err != nil {
			t.Errorf("decodeInstance = %v, want nil", err)
		}
	})

	t.Run("an empty document is io.EOF", func(t *testing.T) {
		_, err := decodeInstance(nil)
		if !errors.Is(err, io.EOF) {
			t.Errorf("decodeInstance(nil) = %v, want io.EOF", err)
		}
	})

	t.Run("a syntax error keeps its type", func(t *testing.T) {
		_, err := decodeInstance([]byte("{\n  \"a\": 1,\n}"))
		var se *json.SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("error %v does not unwrap to *json.SyntaxError", err)
		}
		if se.Offset != 13 {
			t.Errorf("offset = %d, want 13", se.Offset)
		}
	})
}

func TestEvaluateMessageFormats(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []Issue
	}{
		{
			name: "a root that is not an object",
			doc:  `[]`,
			want: []Issue{{"", "got array, want object"}},
		},
		{
			name: "several missing properties at once",
			doc:  `{}`,
			want: []Issue{{"", "missing properties 'formatVersion', 'database', 'tables'"}},
		},
		{
			name: "one missing property",
			doc:  wrap(`{"name":"id","logicalName":"顧客ID","type":"BIGINT"}`),
			want: []Issue{{"/tables/0/columns/0", "missing property 'nullable'"}},
		},
		{
			name: "one unknown property",
			doc:  wrap(`{"name":"id","logicalName":"顧客ID","type":"BIGINT","nullable":false,"comment":"x"}`),
			want: []Issue{{"/tables/0/columns/0", "additional properties 'comment' not allowed"}},
		},
		{
			name: "three unknown properties, listed in sorted order",
			doc:  wrap(`{"name":"id","logicalName":"顧客ID","type":"BIGINT","nullable":false,"c":1,"a":2,"b":3}`),
			want: []Issue{{"/tables/0/columns/0", "additional properties 'a', 'b', 'c' not allowed"}},
		},
		{
			name: "a value outside an enum",
			doc:  `{"formatVersion":"1.0","database":{"name":"shop","dbms":"Postgres"},"tables":[{"name":"t","logicalName":"表","columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}]}]}`,
			want: []Issue{{"/database/dbms", "value must be one of 'PostgreSQL', 'MySQL', 'MariaDB', 'SQLite', 'Oracle', 'SQLServer'"}},
		},
		{
			name: "a referential action outside its enum",
			doc: `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[{"name":"t","logicalName":"表",` +
				`"columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}],` +
				`"foreignKeys":[{"columns":["id"],"references":{"table":"u","columns":["id"]},"onDelete":"NOPE"}]}]}`,
			want: []Issue{{"/tables/0/foreignKeys/0/onDelete", "value must be one of 'CASCADE', 'RESTRICT', 'SET NULL', 'SET DEFAULT', 'NO ACTION'"}},
		},
		{
			name: "a format version that is not MAJOR.MINOR",
			doc:  `{"formatVersion":"1","database":{"name":"shop"},"tables":[{"name":"t","logicalName":"表","columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}]}]}`,
			// One backslash, not two: the doubled form is a different regular
			// expression, and the message has to read as the schema is written.
			want: []Issue{{"/formatVersion", `'1' does not match pattern '^[0-9]+\.[0-9]+$'`}},
		},
		{
			name: "a table name that is not an identifier",
			doc:  `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[{"name":"order-lines","logicalName":"表","columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}]}]}`,
			want: []Issue{{"/tables/0/name", `'order-lines' does not match pattern '^[A-Za-z_][A-Za-z0-9_]*$'`}},
		},
		{
			name: "a string that is too short",
			doc:  wrap(`{"name":"id","logicalName":"","type":"BIGINT","nullable":false}`),
			want: []Issue{{"/tables/0/columns/0/logicalName", "minLength: got 0, want 1"}},
		},
		{
			name: "a string that is too long",
			// The count is printed without a thousands separator. The old
			// library rendered every message through a locale-aware printer
			// and said "3,000"; the separator left with the dependency.
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false,"description":"` + strings.Repeat("x", 3000) + `"}`),
			want: []Issue{{"/tables/0/columns/0/description", "maxLength: got 3000, want 2000"}},
		},
		{
			name: "an array that is too short",
			doc:  `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[]}`,
			want: []Issue{{"/tables", "minItems: got 0, want 1"}},
		},
		{
			name: "two equal items",
			doc: `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[{"name":"t","logicalName":"表",` +
				`"columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}],` +
				`"primaryKey":{"columns":["a","a"]}}]}`,
			want: []Issue{{"/tables/0/primaryKey/columns", "items at 0 and 1 are equal"}},
		},
		{
			name: "the earliest equal pair, not the closest",
			doc: `{"formatVersion":"1.0","database":{"name":"shop"},"tables":[{"name":"t","logicalName":"表",` +
				`"columns":[{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false}],` +
				`"primaryKey":{"columns":["a","b","a"]}}]}`,
			want: []Issue{{"/tables/0/primaryKey/columns", "items at 0 and 2 are equal"}},
		},
		{
			name: "a dependent property that is missing",
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"NUMERIC","nullable":false,"scale":2}`),
			want: []Issue{{"/tables/0/columns/0", "properties 'precision' required, if 'scale' exists"}},
		},
		{
			name: "a number below its minimum",
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"VARCHAR","nullable":false,"length":0}`),
			want: []Issue{{"/tables/0/columns/0/length", "minimum: got 0, want 1"}},
		},
		{
			name: "a negative number below a minimum of zero",
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"NUMERIC","nullable":false,"precision":1,"scale":-1}`),
			want: []Issue{{"/tables/0/columns/0/scale", "minimum: got -1, want 0"}},
		},
		{
			name: "a string where an integer belongs",
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"VARCHAR","nullable":false,"length":"255"}`),
			want: []Issue{{"/tables/0/columns/0/length", "got string, want integer"}},
		},
		{
			name: "a string where a boolean belongs",
			doc:  wrap(`{"name":"id","logicalName":"ID","type":"BIGINT","nullable":"yes"}`),
			want: []Issue{{"/tables/0/columns/0/nullable", "got string, want boolean"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateDoc(t, tt.doc)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d issue(s) %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("issue %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestEvaluateAcceptsIntegralNumberForms is about json.Number rather than
// float64: JSON Schema asks whether a value is an integer, not how it was
// spelt, so 1.0 and 1e2 are integers and 1.5 is not.
func TestEvaluateAcceptsIntegralNumberForms(t *testing.T) {
	tests := []struct {
		length string
		want   string // "" when the value must be accepted
	}{
		{"1", ""},
		{"1.0", ""},
		{"1e2", ""},
		{"100", ""},
		{"1.5", "got number, want integer"},
		{"true", "got boolean, want integer"},
		{"null", "got null, want integer"},
	}

	for _, tt := range tests {
		t.Run(tt.length, func(t *testing.T) {
			doc := wrap(`{"name":"id","logicalName":"ID","type":"VARCHAR","nullable":false,"length":` + tt.length + `}`)
			got := evaluateDoc(t, doc)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %v, want no issue", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d issue(s) %v, want 1", len(got), got)
			}
			if got[0].Message != tt.want {
				t.Errorf("message = %q, want %q", got[0].Message, tt.want)
			}
		})
	}
}

// TestEvaluateSortsAdditionalPropertyNames is what stops Go's randomised map
// iteration from leaking into a report: the same document has to produce the
// same message every time it is evaluated, and the names have to be sorted for
// that to be possible at all.
func TestEvaluateSortsAdditionalPropertyNames(t *testing.T) {
	doc := wrap(`{"name":"id","logicalName":"ID","type":"BIGINT","nullable":false,` +
		`"zulu":1,"alpha":2,"mike":3,"bravo":4,"yankee":5}`)
	const want = "additional properties 'alpha', 'bravo', 'mike', 'yankee', 'zulu' not allowed"

	for i := 0; i < 20; i++ {
		got := evaluateDoc(t, doc)
		if len(got) != 1 {
			t.Fatalf("run %d: got %d issue(s) %v, want 1", i, len(got), got)
		}
		if got[0].Message != want {
			t.Fatalf("run %d: message = %q, want %q", i, got[0].Message, want)
		}
	}
}

// TestEvaluateCountsCharactersNotBytes guards the bounds against the documents
// this tool is actually written for: 255 Japanese characters are 765 bytes,
// and a length counted in bytes would refuse a name three times shorter than
// the schema allows.
func TestEvaluateCountsCharactersNotBytes(t *testing.T) {
	t.Run("255 characters are accepted", func(t *testing.T) {
		doc := wrap(`{"name":"id","logicalName":"` + strings.Repeat("あ", 255) + `","type":"BIGINT","nullable":false}`)
		if got := evaluateDoc(t, doc); len(got) != 0 {
			t.Fatalf("got %v, want no issue", got)
		}
	})

	t.Run("256 characters are refused", func(t *testing.T) {
		doc := wrap(`{"name":"id","logicalName":"` + strings.Repeat("あ", 256) + `","type":"BIGINT","nullable":false}`)
		got := evaluateDoc(t, doc)
		if len(got) != 1 {
			t.Fatalf("got %d issue(s) %v, want 1", len(got), got)
		}
		if want := "maxLength: got 256, want 255"; got[0].Message != want {
			t.Errorf("message = %q, want %q", got[0].Message, want)
		}
	})
}

func TestQuoteToken(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"it's", `'it\'s'`},
		{"a\tb", `'a\tb'`},
		{`a"b`, `'a"b'`},
		{"a/b", `'a/b'`},
		{`a\b`, `'a\\b'`},
		{"a\nb", `'a\nb'`},
		{"日本語", `'日本語'`},
		{"", `''`},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := quoteToken(tt.in); got != tt.want {
				t.Errorf("quoteToken(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
