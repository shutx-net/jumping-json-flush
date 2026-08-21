package dot

import (
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ptr returns a pointer to n, so that a table row can declare an optional
// numeric column attribute inline.
func ptr(n int) *int { return &n }

func TestQuoteID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain identifier", "customers", `"customers"`},
		// Every DOT keyword is a legal table name under $defs/identifier, which
		// is the reason quoteID never omits its quotes.
		{"the keyword graph", "graph", `"graph"`},
		{"the keyword node", "node", `"node"`},
		{"the keyword edge", "edge", `"edge"`},
		{"the keyword subgraph", "subgraph", `"subgraph"`},
		{"the keyword strict", "strict", `"strict"`},
		{"the keyword digraph", "digraph", `"digraph"`},
		{"empty string still gets its quotes", "", `""`},
		{"a quote is escaped", `a"b`, `"a\"b"`},
		{"a backslash is escaped", `a\b`, `"a\\b"`},
		{"multibyte UTF-8 passes through", "会員", `"会員"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteID(tt.in); got != tt.want {
				t.Errorf("quoteID(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestEscapeHTML pins the escaping rule. Only a logicalName or a description
// can ever carry one of the escaped characters: $defs/identifier and
// $defs/columnType both forbid them, so no physical name and no type name in a
// valid document needs escaping at all. These rows document a defence rather
// than a reachable case, which is also why the fixtures put such characters in
// logical names and nowhere else.
func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is unchanged", "customers", "customers"},
		{"empty string", "", ""},
		{"ampersand", "A & B", "A &amp; B"},
		{"angle brackets and quotes", `<x> "y"`, `&lt;x&gt; &quot;y&quot;`},
		// Input is document text, not markup, so an entity in it is escaped
		// again. No is-it-already-an-entity heuristic belongs here.
		{"an already-escaped entity is escaped again", "&amp;", "&amp;amp;"},
		{"the apostrophe is left alone", "it's", "it's"},
		{"all four escapable characters at once", `A & B <c> "d"`, `A &amp; B &lt;c&gt; &quot;d&quot;`},
		{"Japanese passes through byte for byte", "受注明細", "受注明細"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeHTML(tt.in); got != tt.want {
				t.Errorf("escapeHTML(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderType(t *testing.T) {
	tests := []struct {
		name string
		in   model.Column
		want string
	}{
		{"no size at all", model.Column{Type: "TEXT"}, "TEXT"},
		{"length", model.Column{Type: "VARCHAR", Length: ptr(255)}, "VARCHAR(255)"},
		{
			"precision and scale, one comma and no space",
			model.Column{Type: "NUMERIC", Precision: ptr(10), Scale: ptr(2)},
			"NUMERIC(10,2)",
		},
		{
			"precision alone",
			model.Column{Type: "NUMERIC", Precision: ptr(8)},
			"NUMERIC(8)",
		},
		// Scale is a pointer precisely so that an explicit zero is not mistaken
		// for absence.
		{
			"an explicit zero scale is printed",
			model.Column{Type: "NUMERIC", Precision: ptr(10), Scale: ptr(0)},
			"NUMERIC(10,0)",
		},
		{
			"a type name may contain spaces",
			model.Column{Type: "TIMESTAMP WITH TIME ZONE"},
			"TIMESTAMP WITH TIME ZONE",
		},
		// The schema permits both (dependentRequired only ties scale to
		// precision), and the precedence must match sizeOf, which also tests
		// Length first.
		{
			"length wins over precision",
			model.Column{Type: "NUMERIC", Length: ptr(4), Precision: ptr(10), Scale: ptr(2)},
			"NUMERIC(4)",
		},
		// Not reachable from a valid document, but the code must not crash:
		// the scale branch is not entered and the bare type name comes back.
		{
			"a scale without a precision renders as the bare type",
			model.Column{Type: "NUMERIC", Scale: ptr(2)},
			"NUMERIC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderType(&tt.in); got != tt.want {
				t.Errorf("renderType(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
