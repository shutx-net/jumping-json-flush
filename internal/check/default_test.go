package check

import (
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// withDefault gives a column a default. The value has to be addressable, and
// writing &def at forty call sites would need forty named variables.
func withDefault(c model.Column, def string) model.Column {
	c.Default = &def
	return c
}

// oneDefault wraps a single default in the smallest document that carries it,
// so that a case differs from its neighbours in the expression alone.
func oneDefault(def string) *model.Document {
	return document(table("t", withDefault(col("notes"), def)))
}

// TestColumnDefaultAccepts is the list of expressions that must stay silent.
//
// It is the more important half of the two: a false positive here is jjf
// warning about a document it would itself produce, or about correct SQL an
// author had every right to write, and either one teaches people to stop
// reading the warnings. Everything the importer can emit into a document is
// represented, together with the standard forms - casts, qualified names,
// typed literals, the array constructor - that no allowlist of function names
// could have covered.
func TestColumnDefaultAccepts(t *testing.T) {
	defs := []string{
		// Numbers, including the exponent form the scanner has to keep whole.
		"0", "1", "0.00", "-1", "1e3", "1.5e-3", ".5",

		// Keyword constants, in both cases, from every system the schema names.
		"CURRENT_TIMESTAMP", "current_timestamp", "CURRENT_DATE", "CURRENT_TIME",
		"LOCALTIME", "LOCALTIMESTAMP", "CURRENT_USER", "SESSION_USER", "USER",
		"CURRENT_ROLE", "CURRENT_CATALOG", "CURRENT_SCHEMA",
		"SYSDATE", "SYSTIMESTAMP",
		"NULL", "null", "true", "TRUE", "false",

		// Function calls, qualified and not, with and without arguments.
		"now()", "NOW()", "gen_random_uuid()", "public.myfunc()",
		"CURRENT_TIMESTAMP(3)", "nextval('x'::regclass)",

		// String literals, including the empty one - which is a real default
		// and must not be confused with an empty "default" key.
		"'pending'", "''", "'it''s'::text", "'hello  world'::text",

		// Casts, spaced and not, onto qualified and unqualified types.
		"''::text", "'{}'::jsonb", "'pending'::public.order_status", "'pending' :: text",

		// Parentheses, nested, and the array constructor the importer emits.
		"(1 + 2)", "((1))", "ARRAY[]::text[]",

		// The escape-string form pg_dump writes for a backslash, and the typed
		// literal a hand author writes for an interval or a date.
		`E'a\b'`, "INTERVAL '1 day'", "DATE '2020-01-01'", "b'0'",

		// An operator between two literals, which contains no word at all.
		"'a' || 'b'",

		// Radix literals. mysqldump writes the hexadecimal one for any default
		// it cannot spell in the connection charset - an emoji, most often -
		// so this is a value the importer really produces. PostgreSQL 16.13
		// reads all three and MySQL 8.0.46 the first two; the check only has
		// to stop calling the tail of one a bare word.
		"0xF09F8DA3", "0x1f", "0X1F", "0b1010", "0B1010", "0o17", "0O17",

		// A comment introducer and a statement terminator inside a string
		// literal are ordinary text. '#ff0000' is the everyday one: a colour
		// is a perfectly good default and starts with the byte MySQL reads as
		// a comment when it stands outside quotes.
		"'--'", "'/* */'", "';'", "'#'", "'#ff0000'", "'a; b'", "'-- not a comment'",

		// One minus is subtraction and one slash is division. Only the doubled
		// minus and the slash-star pair introduce a comment, so an expression
		// that merely contains either byte must still pass.
		"-1", "1 - -2", "1/2", "1 * 2",
	}

	for _, def := range defs {
		t.Run(def, func(t *testing.T) {
			if got := rendered(Document(oneDefault(def))); got != nil {
				t.Errorf("default %q: findings got = %v, want none", def, got)
			}
		})
	}
}

// TestColumnDefaultReports covers the mistakes, one finding each.
func TestColumnDefaultReports(t *testing.T) {
	tests := []struct {
		name string
		def  string
		want []string
	}{
		{
			name: "an empty default is a DEFAULT clause with nothing in it",
			def:  "",
			want: []string{
				`column notes on table t: declares an empty default; omit the "default" key when the column has no DEFAULT clause`,
			},
		},
		{
			name: "whitespace is as empty as the empty string",
			def:  "   ",
			want: []string{
				`column notes on table t: declares an empty default; omit the "default" key when the column has no DEFAULT clause`,
			},
		},
		{
			name: "an unquoted word is a column reference, not a string",
			def:  "now",
			want: []string{
				`column notes on table t: declares the default "now", in which "now" is a bare word; a string literal is written 'now'`,
			},
		},
		{
			name: "an unquoted sentence is one mistake, named by its first word",
			def:  "hello world",
			want: []string{
				`column notes on table t: declares the default "hello world", in which "hello" is a bare word; a string literal is written 'hello'`,
			},
		},
		{
			name: "a word punctuation cannot rescue",
			def:  "N/A",
			want: []string{
				`column notes on table t: declares the default "N/A", in which "N" is a bare word; a string literal is written 'N'`,
			},
		},
		{
			name: "an apostrophe opens a string that is never closed",
			def:  "it's",
			want: []string{
				`column notes on table t: declares the default "it's", which has an unbalanced single quote`,
			},
		},
		{
			name: "a quoted string that is never closed",
			def:  "'unclosed",
			want: []string{
				`column notes on table t: declares the default "'unclosed", which has an unbalanced single quote`,
			},
		},
		{
			// The doubling rule must not read the middle quote as an escape and
			// call the whole thing one well-formed string.
			name: "an apostrophe inside a quoted string that was not doubled",
			def:  "'it's'",
			want: []string{
				`column notes on table t: declares the default "'it's'", which has an unbalanced single quote`,
			},
		},
		{
			name: "an unclosed parenthesis",
			def:  "(1 + 2",
			want: []string{
				`column notes on table t: declares the default "(1 + 2", which has an unbalanced parenthesis`,
			},
		},
		{
			name: "a parenthesis closed but never opened",
			def:  "1 + 2)",
			want: []string{
				`column notes on table t: declares the default "1 + 2)", which has an unbalanced parenthesis`,
			},
		},
		{
			// One "(" and one ")" in the wrong order. A count would call this
			// balanced, which is why the checker carries a depth instead.
			name: "parentheses that close before they open",
			def:  ")(",
			want: []string{
				`column notes on table t: declares the default ")(", which has an unbalanced parenthesis`,
			},
		},
		{
			// Two things are wrong; the first one is reported and the second is
			// not, because the second is only visible through the first.
			name: "a column produces at most one default finding",
			def:  "(now",
			want: []string{
				`column notes on table t: declares the default "(now", which has an unbalanced parenthesis`,
			},
		},
		{
			// The case that cost the most: PostgreSQL writes DEFAULT before
			// NOT NULL, so a comment opened inside the default swallows the
			// clause the generator writes after it. The script then succeeds
			// and the column is nullable, leaving the document and the
			// database disagreeing with nothing to read that says so.
			name: "a line comment swallows the clause the generated DDL writes after the default",
			def:  "1 --",
			want: []string{
				`column notes on table t: declares the default "1 --", which starts a comment at "--"; the text after it is not part of the expression`,
			},
		},
		{
			name: "a line comment with text of its own",
			def:  "1 -- fixme",
			want: []string{
				`column notes on table t: declares the default "1 -- fixme", which starts a comment at "--"; the text after it is not part of the expression`,
			},
		},
		{
			// No space is needed to start one, so the pair has to be found by
			// adjacency rather than by the spacing an author happened to use.
			name: "two minus signs need no space between them or after them",
			def:  "1--2",
			want: []string{
				`column notes on table t: declares the default "1--2", which starts a comment at "--"; the text after it is not part of the expression`,
			},
		},
		{
			name: "a block comment that is never closed",
			def:  "1 /* fixme",
			want: []string{
				`column notes on table t: declares the default "1 /* fixme", which starts a comment at "/*"; the text after it is not part of the expression`,
			},
		},
		{
			// Closing it changes nothing: the DDL is one line, and a comment
			// that opens and closes inside the default still means the field
			// is carrying text that is not the expression.
			name: "a block comment that is closed again",
			def:  "1 /* fixme */",
			want: []string{
				`column notes on table t: declares the default "1 /* fixme */", which starts a comment at "/*"; the text after it is not part of the expression`,
			},
		},
		{
			name: "a semicolon ends the statement rather than the expression",
			def:  "1 ;",
			want: []string{
				`column notes on table t: declares the default "1 ;", which ends its statement at ";"; a DEFAULT is one expression`,
			},
		},
		{
			// Reported before the parenthesis, not after it: a "(" the author
			// wrote inside a comment is not one they have to balance, so
			// naming the parenthesis first would name the wrong mistake.
			name: "a comment is reported before the parenthesis it hides",
			def:  "(1 --",
			want: []string{
				`column notes on table t: declares the default "(1 --", which starts a comment at "--"; the text after it is not part of the expression`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rendered(Document(oneDefault(tt.def)))
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings got = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestScanExpr pins the token stream itself.
//
// It exists for the reason internal/importer/postgres has lex_test.go: the
// rules the checker states - "preceded by a cast", "followed by a string" - are
// only as true as the tokens they are read from, and a scanner mistake is far
// easier to read at this level than through a finding message that names the
// wrong word for a reason the message cannot show.
func TestScanExpr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		ok   bool
	}{
		{
			name: "a doubled quote stays inside one string",
			in:   "'it''s'::text",
			want: []string{`string "'it''s'"`, `punct "::"`, `word "text"`},
			ok:   true,
		},
		{
			name: "an escape string swallows the byte after a backslash",
			in:   `E'a\b'`,
			want: []string{`string "E'a\b'"`},
			ok:   true,
		},
		{
			// A doubled quote inside an ESCAPE string, which PostgreSQL accepts
			// alongside the backslash form. It is a different arm from the
			// doubled quote in the ordinary string two cases above, and both
			// spellings reach a default that pg_dump wrote.
			name: "a doubled quote inside an escape string stays inside it",
			in:   `E'it''s'::text`,
			want: []string{`string "E'it''s'"`, `punct "::"`, `word "text"`},
			ok:   true,
		},
		{
			// A number may begin with its decimal point. What this pins is the
			// token COUNT: split into a punct and a number, the leading dot
			// would be an operator with nothing before it and a perfectly good
			// default would be reported as one that does not read as SQL.
			name: "a number may begin with its decimal point",
			in:   ".5",
			want: []string{`number ".5"`},
			ok:   true,
		},
		{
			name: "a trailing zero is part of the number",
			in:   "0.00",
			want: []string{`number "0.00"`},
			ok:   true,
		},
		{
			name: "an exponent does not break into a number and a word",
			in:   "1e3",
			want: []string{`number "1e3"`},
			ok:   true,
		},
		{
			name: "a dot with no digit after it is punctuation, not a number",
			in:   "public.myfunc",
			want: []string{`word "public"`, `punct "."`, `word "myfunc"`},
			ok:   true,
		},
		{
			name: "brackets are punctuation of their own",
			in:   "ARRAY[]::text[]",
			want: []string{
				`word "ARRAY"`, `punct "["`, `punct "]"`, `punct "::"`,
				`word "text"`, `punct "["`, `punct "]"`,
			},
			ok: true,
		},
		{
			name: "a cast is one token, not two colons",
			in:   "::",
			want: []string{`punct "::"`},
			ok:   true,
		},
		{
			name: "an unterminated string is the scanner's only failure",
			in:   "'unclosed",
			ok:   false,
		},
		{
			// A backslash at the end walks the index past the last byte. The
			// answer is "unterminated"; the point is that it is an answer.
			name: "an escape string ending mid-escape terminates the scan",
			in:   `E'a\`,
			ok:   false,
		},
		{
			name: "nothing at all is no tokens and no failure",
			in:   "",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, ok := scanExpr(tt.in)
			if ok != tt.ok {
				t.Fatalf("scanExpr(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if !ok {
				return
			}
			if got := renderTokens(toks); !slices.Equal(got, tt.want) {
				t.Errorf("scanExpr(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// renderTokens spells a token stream out for a failure message.
func renderTokens(toks []exprToken) []string {
	var out []string
	for _, tk := range toks {
		var kind string
		switch tk.kind {
		case exprString:
			kind = "string"
		case exprNumber:
			kind = "number"
		case exprWord:
			kind = "word"
		case exprPunct:
			kind = "punct"
		}
		out = append(out, kind+" \""+tk.text+"\"")
	}
	return out
}
