package ddl

import "testing"

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"an ordinary name", "orders", `"orders"`},
		{"a reserved word, which is why quoting is unconditional", "order", `"order"`},
		{"mixed case, preserved by the quoting", "OrderItems", `"OrderItems"`},
		// The schema's identifier pattern forbids a quote, so this is
		// defensive rather than reachable.
		{"an embedded double quote, doubled", `a"b`, `"a""b"`},
		{"a backslash, which SQL does not escape", `a\b`, `"a\b"`},
		{"the empty string", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteIdent(tt.in); got != tt.want {
				t.Errorf("quoteIdent(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary text", "pending", `'pending'`},
		{"an apostrophe, doubled", "it's", `'it''s'`},
		{"two apostrophes", "''", `''''''`},
		// standard_conforming_strings is on, so a backslash is an ordinary
		// character and escaping it would change the text.
		{"a backslash, left alone", `a\b`, `'a\b'`},
		// The newline has to survive: a comment is the logical name and the
		// description joined by one, and the importer cuts it back apart there.
		{"an embedded newline", "one\ntwo", "'one\ntwo'"},
		{"Japanese text, byte for byte", "受注", `'受注'`},
		{"the empty string", "", `''`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteLiteral(tt.in); got != tt.want {
				t.Errorf("quoteLiteral(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
