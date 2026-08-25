package dot

import "testing"

// ptr returns a pointer to n, so that a table row can declare an optional
// numeric column attribute inline. TestRenderType moved to
// internal/export/erd with the function it covered; export_test.go still
// builds columns with a declared size, so ptr stays.
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
