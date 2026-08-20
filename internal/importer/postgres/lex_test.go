package postgres

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// summarize renders tokens as "kind:text" pairs so that a test table states the
// expected lexing on one readable line. The trailing EOF token is dropped: it is
// present in every result and would only add noise.
func summarize(toks []token) []string {
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		if tok.kind == kindEOF {
			continue
		}
		out = append(out, tok.kind.String()+":"+tok.text)
	}
	return out
}

func TestLexTokens(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "quoted identifier with a doubled quote",
			src:  `"foo""bar"`,
			want: []string{`quoted-ident:foo"bar`},
		},
		{
			name: "string with a doubled quote",
			src:  `'a''b'`,
			want: []string{"string:a'b"},
		},
		{
			name: "escape string",
			src:  `E'a\nb\\c\'d\q'`,
			want: []string{"string:a\nb\\c'dq"},
		},
		{
			name: "untagged dollar quote",
			src:  `$$body$$`,
			want: []string{"string:body"},
		},
		{
			name: "tagged dollar quote containing an untagged one",
			src:  `$tag$ body with $$ inside $tag$`,
			want: []string{"string: body with $$ inside "},
		},
		{
			name: "dollar sign followed by a digit is not a dollar quote",
			src:  `$1`,
			want: []string{"punct:$", "number:1"},
		},
		{
			name: "nested block comment",
			src:  `/* outer /* inner */ still outer */ SELECT`,
			want: []string{"word:select"},
		},
		{
			name: "line comment without a trailing newline",
			src:  "SELECT 1; -- trailing",
			want: []string{"word:select", "number:1", "punct:;"},
		},
		{
			name: "comment markers inside a string are inert",
			src:  `'-- not a comment /* either */'`,
			want: []string{"string:-- not a comment /* either */"},
		},
		{
			name: "a quote inside a comment is inert",
			src:  "-- it's fine\nSELECT",
			want: []string{"word:select"},
		},
		{
			name: "qualified name",
			src:  `public.users`,
			want: []string{"word:public", "punct:.", "word:users"},
		},
		{
			name: "array type",
			src:  `character varying(255)[]`,
			want: []string{
				"word:character", "word:varying", "punct:(", "number:255", "punct:)",
				"punct:[", "punct:]",
			},
		},
		{
			name: "timestamp with a precision",
			src:  `timestamp(3) without time zone`,
			want: []string{
				"word:timestamp", "punct:(", "number:3", "punct:)",
				"word:without", "word:time", "word:zone",
			},
		},
		{
			name: "nextval default keeps the cast as one token",
			src:  `DEFAULT nextval('public.users_id_seq'::regclass)`,
			want: []string{
				"word:default", "word:nextval", "punct:(", "string:public.users_id_seq",
				"punct:::", "word:regclass", "punct:)",
			},
		},
		{
			name: "psql backslash line is skipped",
			src:  "\\connect mydb\nSELECT",
			want: []string{"word:select"},
		},
		{
			name: "quoted identifier with multibyte characters",
			src:  `"ユーザー"`,
			want: []string{"quoted-ident:ユーザー"},
		},
		{
			name: "unquoted multibyte word keeps its bytes",
			src:  `ユーザー`,
			want: []string{"word:ユーザー"},
		},
		{
			name: "exponent stays in one number token",
			src:  `1.5e3`,
			want: []string{"number:1.5e3"},
		},
		{
			name: "empty input",
			src:  ``,
			want: []string{},
		},
		{
			name: "whitespace only",
			src:  " \t\n\r\f\v",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := lex([]byte(tt.src))
			if err != nil {
				t.Fatalf("lex(%q) returned error %v, want no error", tt.src, err)
			}
			if got := summarize(toks); !slices.Equal(got, tt.want) {
				t.Errorf("lex(%q) got = %v, want %v", tt.src, got, tt.want)
			}
			if last := toks[len(toks)-1]; last.kind != kindEOF {
				t.Errorf("lex(%q) last token kind = %v, want %v", tt.src, last.kind, kindEOF)
			}
		})
	}
}

// lineNumbersSource mixes the three things that make line counting go wrong: a
// multi-line dollar-quoted body, a multi-line block comment, and CRLF endings.
const lineNumbersSource = "CREATE TABLE t (\n" + // 1
	"  a integer\n" + // 2
	");\n" + // 3
	"CREATE FUNCTION f() RETURNS void AS $body$\n" + // 4
	"  -- not a comment, it is inside the body\n" + // 5
	"  BEGIN\n" + // 6
	"    RAISE NOTICE 'hi';\n" + // 7
	"  END;\n" + // 8
	"$body$ LANGUAGE plpgsql;\n" + // 9
	"/* three\n" + // 10
	"   line\n" + // 11
	"   comment */\n" + // 12
	"alpha;\r\n" + // 13
	"beta;\r\n" + // 14
	"gamma;\n" // 15

func TestLexLineNumbers(t *testing.T) {
	toks, err := lex([]byte(lineNumbersSource))
	if err != nil {
		t.Fatalf("lex returned error %v, want no error", err)
	}

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "first statement", text: "table", want: 1},
		{name: "column name", text: "a", want: 2},
		// The body opens on line 4 and closes on line 9, so the token after it
		// proves that all five newlines inside were counted.
		{name: "word after the function body", text: "language", want: 9},
		{name: "word after the block comment", text: "alpha", want: 13},
		{name: "word after a CRLF line", text: "beta", want: 14},
		{name: "last word", text: "gamma", want: 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := slices.IndexFunc(toks, func(tok token) bool { return tok.isWord(tt.text) })
			if idx < 0 {
				t.Fatalf("token %q not found", tt.text)
			}
			if got := toks[idx].line; got != tt.want {
				t.Errorf("line of %q got = %v, want %v", tt.text, got, tt.want)
			}
		})
	}

	// The dollar-quoted body itself is reported at the line it opened on.
	idx := slices.IndexFunc(toks, func(tok token) bool { return tok.kind == kindString })
	if idx < 0 {
		t.Fatal("no string token found")
	}
	if got := toks[idx].line; got != 4 {
		t.Errorf("line of the function body got = %v, want %v", got, 4)
	}
}

// offsetSource is a slice of real pg_dump output, chosen because it exercises
// every token kind at once.
const offsetSource = `SET default_table_access_method = heap;

CREATE TABLE public."Users" (
    id integer NOT NULL,
    email character varying(255) NOT NULL,
    ratio numeric(10,2) DEFAULT 1.5e3,
    created_at timestamp(3) without time zone DEFAULT now() NOT NULL,
    note text DEFAULT 'it''s fine'
);

ALTER TABLE ONLY public."Users"
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

COMMENT ON TABLE public."Users" IS E'User\naccounts';
`

func TestLexOffsetsCoverTheSource(t *testing.T) {
	src := []byte(offsetSource)
	toks, err := lex(src)
	if err != nil {
		t.Fatalf("lex returned error %v, want no error", err)
	}

	prev := 0
	for _, tok := range toks {
		if tok.kind == kindEOF {
			if tok.pos != len(src) || tok.end != len(src) {
				t.Errorf("eof offsets got = [%d,%d), want [%d,%d)", tok.pos, tok.end, len(src), len(src))
			}
			continue
		}
		if tok.pos >= tok.end {
			t.Errorf("token %v has empty range [%d,%d)", tok.kind, tok.pos, tok.end)
		}
		if tok.pos < prev {
			t.Errorf("token %v starts at %d, before the end %d of the previous token", tok.kind, tok.pos, prev)
		}
		prev = tok.end

		// Slicing a token out of the source and lexing it on its own must give
		// the same token back; that is the property the build phase relies on
		// when it reproduces a DEFAULT expression verbatim.
		again, err := lex(src[tok.pos:tok.end])
		if err != nil {
			t.Fatalf("re-lexing %q returned error %v, want no error", src[tok.pos:tok.end], err)
		}
		if len(again) != 2 {
			t.Fatalf("re-lexing %q got = %v tokens, want 1 plus eof", src[tok.pos:tok.end], len(again)-1)
		}
		if again[0].kind != tok.kind || again[0].text != tok.text {
			t.Errorf("re-lexing %q got = %v:%s, want %v:%s",
				src[tok.pos:tok.end], again[0].kind, again[0].text, tok.kind, tok.text)
		}
	}
}

func TestLexErrors(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "unterminated string literal",
			src:      "SELECT\n'abc",
			wantLine: 2,
			wantMsg:  "unterminated string literal",
		},
		{
			name:     "unterminated escape string",
			src:      `E'abc\`,
			wantLine: 1,
			wantMsg:  "unterminated string literal",
		},
		{
			name:     "unterminated quoted identifier",
			src:      "SELECT\n\n\"abc",
			wantLine: 3,
			wantMsg:  "unterminated quoted identifier",
		},
		{
			name:     "unterminated dollar quote",
			src:      "\n$tag$abc",
			wantLine: 2,
			wantMsg:  "unterminated dollar-quoted string",
		},
		{
			name:     "unterminated block comment",
			src:      "SELECT 1;\n/* abc\nmore\n",
			wantLine: 2,
			wantMsg:  "unterminated block comment",
		},
		{
			name:     "unterminated nested block comment",
			src:      "/* outer /* inner */\n",
			wantLine: 1,
			wantMsg:  "unterminated block comment",
		},
		{
			name:     "zero-length quoted identifier",
			src:      `SELECT "" FROM t`,
			wantLine: 1,
			wantMsg:  "zero-length quoted identifier",
		},
		{
			name:     "unexpected character",
			src:      "SELECT 1;\nSELECT \\x;",
			wantLine: 2,
			wantMsg:  `unexpected character "\\"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := lex([]byte(tt.src))
			if err == nil {
				t.Fatalf("lex(%q) returned no error, want %q", tt.src, tt.wantMsg)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("lex(%q) error type got = %T, want *syntaxError", tt.src, err)
			}
			if !strings.Contains(se.Msg, tt.wantMsg) {
				t.Errorf("lex(%q) message got = %q, want it to contain %q", tt.src, se.Msg, tt.wantMsg)
			}
			if se.Line != tt.wantLine {
				t.Errorf("lex(%q) line got = %v, want %v", tt.src, se.Line, tt.wantLine)
			}
		})
	}
}

// FuzzLex asserts the invariants every caller above the lexer depends on:
// lexing never panics, and a successful result describes ranges inside the
// input, in order.
func FuzzLex(f *testing.F) {
	seeds := []string{
		"$$a$$",
		"$t$a$t$",
		"'a''b'",
		`E'\n'`,
		`"a""b"`,
		"/* /* */ */",
		"--x",
		"\\connect d",
		"1.5e3",
		"a.b",
		"$1",
		"/* /*",
		offsetSource,
		lineNumbersSource,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		toks, err := lex(src)
		if err != nil {
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error type got = %T, want *syntaxError", err)
			}
			if se.Line < 1 {
				t.Fatalf("error line got = %v, want at least 1", se.Line)
			}
			return
		}
		if len(toks) == 0 || toks[len(toks)-1].kind != kindEOF {
			t.Fatal("result does not end with an eof token")
		}
		prevEnd, prevLine := 0, 1
		for _, tok := range toks {
			if tok.pos < 0 || tok.end > len(src) || tok.pos > tok.end {
				t.Fatalf("token %v range [%d,%d) is outside the input of %d bytes",
					tok.kind, tok.pos, tok.end, len(src))
			}
			if tok.pos < prevEnd {
				t.Fatalf("token %v starts at %d, before the previous end %d", tok.kind, tok.pos, prevEnd)
			}
			if tok.line < prevLine {
				t.Fatalf("token %v is on line %d, before the previous line %d", tok.kind, tok.line, prevLine)
			}
			prevEnd, prevLine = tok.end, tok.line
		}
	})
}
