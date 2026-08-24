package schema

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
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

// TestJSONEqualComparesNumbersAsNumbers holds jsonEqual's doc comment to its
// word.
//
// That comment says reflect.DeepEqual would be wrong here because it compares
// json.Number as text. Nothing else stops a later reader from believing the
// two are interchangeable and shortening the function to a one-liner, so both
// halves of the claim are asserted: that DeepEqual really does call these two
// values different, and that jsonEqual really does call them the same.
func TestJSONEqualComparesNumbersAsNumbers(t *testing.T) {
	one, oneZero := json.Number("1"), json.Number("1.0")

	if reflect.DeepEqual(one, oneZero) {
		t.Fatal("reflect.DeepEqual now calls 1 and 1.0 equal, so jsonEqual's reason for existing needs rewriting")
	}
	if !jsonEqual(one, oneZero) {
		t.Errorf("jsonEqual(%s, %s) = false, want true", one, oneZero)
	}
}

// TestJSONEqual pins the equality JSON Schema means, which is not Go's.
//
// The embedded schema asks for it in one place only - uniqueItems on
// #/$defs/columnNameList, a list of strings - so every case below the string
// ones is reached by this test and by nothing else. That is the point rather
// than an argument for deleting them: uniqueItems is defined over any JSON
// value, and a schema that grows a second use of it must not be the thing that
// reaches these branches first.
func TestJSONEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{"null and null", nil, nil, true},
		{"null and false", nil, false, false},
		{"null and the empty string", nil, "", false},

		{"the same boolean", true, true, true},
		{"different booleans", true, false, false},
		{"a boolean and its spelling", true, "true", false},

		{"the same string", "id", "id", true},
		{"strings differing in case", "id", "ID", false},
		{"a string and the number it spells", "1", json.Number("1"), false},

		// A number is its value, not how it was written.
		{"1 and 1.0", json.Number("1"), json.Number("1.0"), true},
		{"1 and 1.00", json.Number("1"), json.Number("1.00"), true},
		{"100 and 1e2", json.Number("100"), json.Number("1e2"), true},
		{"0 and -0", json.Number("0"), json.Number("-0"), true},
		{"1 and 2", json.Number("1"), json.Number("2"), false},
		// Past float64's 53 bits of mantissa these two land on the same value.
		// Comparing them as rationals is what keeps them apart.
		{"consecutive integers past 2^53", json.Number("9007199254740993"), json.Number("9007199254740992"), false},
		// big.Rat refuses these, so the comparison falls back to the text.
		// decodeInstance cannot produce them - encoding/json rejects NaN and
		// Inf - but json.Number is a string, so a caller can hand one over.
		{"the same unparsable number", json.Number("NaN"), json.Number("NaN"), true},
		{"different unparsable numbers", json.Number("NaN"), json.Number("Inf"), false},

		// Order and length are part of an array's value.
		{"the same array", []any{"a", "b"}, []any{"a", "b"}, true},
		{"a reordered array", []any{"a", "b"}, []any{"b", "a"}, false},
		{"a shorter array", []any{"a"}, []any{"a", "b"}, false},
		{"two empty arrays", []any{}, []any{}, true},
		{"nested arrays down to a number form", []any{[]any{json.Number("1")}}, []any{[]any{json.Number("1.0")}}, true},
		{"an array and an object", []any{}, map[string]any{}, false},

		// Key order is not part of an object's value.
		{"reordered keys", map[string]any{"a": "1", "b": "2"}, map[string]any{"b": "2", "a": "1"}, true},
		{"a renamed key", map[string]any{"a": "1"}, map[string]any{"b": "1"}, false},
		{"an extra key", map[string]any{"a": "1"}, map[string]any{"a": "1", "b": "2"}, false},
		{"a changed value", map[string]any{"a": "1"}, map[string]any{"a": "2"}, false},
		{"two empty objects", map[string]any{}, map[string]any{}, true},
		{"nested objects down to a number form", map[string]any{"a": map[string]any{"b": json.Number("1")}}, map[string]any{"a": map[string]any{"b": json.Number("1.0")}}, true},

		// Anything decodeInstance cannot produce is equal to nothing, itself
		// included. A float64 only reaches here if a caller skipped UseNumber,
		// and answering "equal" would hide that mistake instead of failing on
		// it.
		{"a float64 is not a decoded JSON value", float64(1), float64(1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("jsonEqual(%#v, %#v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// Equality is symmetric. A switch on the first argument's type is
			// exactly the shape that can quietly stop being so, which makes it
			// something to check rather than assume.
			if got := jsonEqual(tt.b, tt.a); got != tt.want {
				t.Errorf("jsonEqual(%#v, %#v) = %v, want %v: not symmetric", tt.b, tt.a, got, tt.want)
			}
		})
	}
}
