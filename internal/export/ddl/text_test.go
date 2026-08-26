package ddl

import "testing"

func TestPGQuoteIdent(t *testing.T) {
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
			if got := pgQuoteIdent(tt.in); got != tt.want {
				t.Errorf("pgQuoteIdent(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestPGQuoteLiteral(t *testing.T) {
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
			if got := pgQuoteLiteral(tt.in); got != tt.want {
				t.Errorf("pgQuoteLiteral(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuotedListUsesTheDialectQuoting exercises the parameter rather than the
// loop. The stub case is what proves the quoting really comes from the
// argument: with the function ignored and a delimiter hard-coded, the
// PostgreSQL case alone would still pass.
func TestQuotedListUsesTheDialectQuoting(t *testing.T) {
	if got, want := quotedList(pgQuoteIdent, []string{"a", "b"}), `"a", "b"`; got != want {
		t.Errorf("quotedList(pgQuoteIdent, ...) = %s, want %s", got, want)
	}

	stub := func(s string) string { return "<" + s + ">" }
	if got, want := quotedList(stub, []string{"a", "b"}), "<a>, <b>"; got != want {
		t.Errorf("quotedList(stub, ...) = %s, want %s", got, want)
	}
	if got, want := quotedList(stub, nil), ""; got != want {
		t.Errorf("quotedList(stub, nil) = %q, want %q", got, want)
	}
	if got, want := quotedList(stub, []string{"only"}), "<only>"; got != want {
		t.Errorf("quotedList(stub, one column) = %s, want %s", got, want)
	}
}
