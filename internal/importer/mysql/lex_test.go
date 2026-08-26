package mysql

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// summarize renders tokens as "kind:text" pairs so that a test table states the
// expected lexing on one readable line. The trailing EOF token is dropped: it is
// present in every result and would only add noise.
//
// A near-copy of internal/importer/postgres/lex_test.go's helper of the same
// name. It is copied rather than shared because sharing it would mean a
// test-only package for six lines, which is the call fixture_test.go's
// checkGolden already made in the sibling.
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

// lexed lexes src or fails the test.
func lexed(t *testing.T, src string) []token {
	t.Helper()
	toks, err := lex([]byte(src))
	if err != nil {
		t.Fatalf("lex(%q) returned error %v, want no error", src, err)
	}
	return toks
}

// runLexTable is the body every success table below shares.
func runLexTable(t *testing.T, tests []struct {
	name string
	src  string
	want []string
}) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks := lexed(t, tt.src)
			if got := summarize(toks); !slices.Equal(got, tt.want) {
				t.Errorf("lex(%q) got = %v, want %v", tt.src, got, tt.want)
			}
			if last := toks[len(toks)-1]; last.kind != kindEOF {
				t.Errorf("lex(%q) last token kind = %v, want %v", tt.src, last.kind, kindEOF)
			}
		})
	}
}

func TestLexIdentifiers(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "a bare word folds to lower case",
			src:  `SeLeCt`,
			want: []string{"word:select"},
		},
		{
			name: "a backtick identifier keeps its case",
			src:  "`Users`",
			want: []string{"quoted-ident:Users"},
		},
		{
			name: "a doubled backtick inside is one backtick",
			src:  "`a``b`",
			want: []string{"quoted-ident:a`b"},
		},
		{
			name: "a reserved word in backticks is an identifier and not a keyword",
			src:  "`key`",
			want: []string{"quoted-ident:key"},
		},
		{
			name: "a word containing a dollar sign is one word",
			src:  `a$b`,
			want: []string{"word:a$b"},
		},
		{
			name: "a word beginning with a dollar sign is one word",
			src:  `$b`,
			want: []string{"word:$b"},
		},
		{
			name: "a double quoted run is a string and not an identifier",
			src:  `"Users"`,
			want: []string{"string:Users"},
		},
		{
			name: "an unquoted multibyte word keeps its bytes",
			src:  `ユーザー`,
			want: []string{"word:ユーザー"},
		},
		{
			name: "a backtick identifier with multibyte characters",
			src:  "`ユーザー`",
			want: []string{"quoted-ident:ユーザー"},
		},
	})
}

func TestLexStrings(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "a single quoted string",
			src:  `'abc'`,
			want: []string{"string:abc"},
		},
		{
			name: "a doubled single quote is one character",
			src:  `'a''b'`,
			want: []string{"string:a'b"},
		},
		{
			name: "a doubled double quote is one character",
			src:  `"a""b"`,
			want: []string{`string:a"b`},
		},
		{
			name: "a doubled quote of the other character is two characters",
			src:  `'a""b'`,
			want: []string{`string:a""b`},
		},
		{
			name: "the escapes MySQL defines",
			src:  `'a\nb\tc\rd\0e\Zf\\g'`,
			want: []string{"string:a\nb\tc\rd\x00e\x1af\\g"},
		},
		{
			name: "a backslash before the closing quote does not close it",
			src:  `'a\'b'`,
			want: []string{"string:a'b"},
		},
		{
			name: "the LIKE escapes keep their backslash",
			src:  `'a\%b\_c'`,
			want: []string{`string:a\%b\_c`},
		},
		{
			// \b completes the table above. It is separate only because
			// writing 0x08 into the row above would make that row's expected
			// value harder to read than the escapes it is about.
			name: "a backspace escape",
			src:  `'a\bb'`,
			want: []string{"string:a\bb"},
		},
		{
			// The divergence this lexer's own comment argues for: \f is
			// PostgreSQL's escape and MySQL has none, so the server reads it as
			// the letter f and so does this. The PostgreSQL package's table
			// decodes the same bytes to a form feed, and the two are meant to
			// differ - nothing is shared between them.
			//
			// The comment makes the same claim about \v, and that half is
			// deliberately not a row here: the PostgreSQL lexer does not decode
			// \v either, although PostgreSQL documents it, so the two files
			// disagree about who owns that escape. A row asserting either
			// answer would settle that by fiat.
			name: "an escape that belongs to the other dialect",
			src:  `'a\fb'`,
			want: []string{"string:afb"},
		},
		{
			name: "an unknown escape yields the escaped character",
			src:  `'a\qb'`,
			want: []string{"string:aqb"},
		},
		{
			name: "a semicolon inside a string does not end a statement",
			src:  `'x;y'`,
			want: []string{"string:x;y"},
		},
		{
			name: "a string spanning two lines keeps its newline",
			src:  "'a\nb'",
			want: []string{"string:a\nb"},
		},
		{
			name: "a bit literal is a word and a string",
			src:  `b'1010'`,
			want: []string{"word:b", "string:1010"},
		},
	})
}

func TestLexComments(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "a hash comment runs to the end of the line",
			src:  "# nothing here\nSELECT",
			want: []string{"word:select"},
		},
		{
			name: "a double dash comment needs the space after it",
			src:  "-- nothing here\nSELECT",
			want: []string{"word:select"},
		},
		{
			name: "a double dash at end of input is a comment",
			src:  "SELECT 1 --",
			want: []string{"word:select", "number:1"},
		},
		{
			name: "a double dash with no space after it is an operator run",
			src:  `a--b`,
			want: []string{"word:a", "punct:--", "word:b"},
		},
		{
			name: "a plain block comment",
			src:  "/* nothing here */ SELECT",
			want: []string{"word:select"},
		},
		{
			name: "a block comment ends at the first close and does not nest",
			src:  "/* outer /* inner */ SELECT 1",
			want: []string{"word:select", "number:1"},
		},
		{
			name: "comment markers inside a string are inert",
			src:  `'-- not a comment /* either */ # nor this'`,
			want: []string{`string:-- not a comment /* either */ # nor this`},
		},
		{
			name: "an apostrophe inside a double dash comment is inert",
			src:  "-- it's fine\nSELECT",
			want: []string{"word:select"},
		},
		{
			name: "an apostrophe inside a hash comment is inert",
			src:  "# it's fine\nSELECT",
			want: []string{"word:select"},
		},
	})
}

func TestLexExecutableComments(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "the wrapper of a preamble SET contributes no token",
			src:  `/*!40101 SET @a=1 */;`,
			want: []string{
				"word:set", "punct:@", "word:a", "punct:=", "number:1", "terminator:;",
			},
		},
		{
			name: "a partition clause lexes as ordinary SQL",
			src:  "/*!50100 PARTITION BY RANGE (year(`created`)) */;",
			want: []string{
				"word:partition", "word:by", "word:range", "punct:(", "word:year",
				"punct:(", "quoted-ident:created", "punct:)", "punct:)", "terminator:;",
			},
		},
		{
			name: "a wrapper with no version digits is accepted",
			src:  `/*! SELECT 1 */;`,
			want: []string{"word:select", "number:1", "terminator:;"},
		},
		{
			name: "a wrapper that closes directly after a word",
			src:  "/*!50003 CREATE*/ /*!50017 DEFINER=`root`*/ /*!50003 TRIGGER*/;",
			want: []string{
				"word:create", "word:definer", "punct:=", "quoted-ident:root",
				"word:trigger", "terminator:;",
			},
		},
		{
			name: "a wrapper that closes directly after an operator character",
			src:  `/*!50003 SET a=*/;`,
			want: []string{"word:set", "word:a", "punct:=", "terminator:;"},
		},
		{
			name: "a wrapper spanning the tail of a CREATE TABLE",
			src:  ") ENGINE=InnoDB\n/*!50100 PARTITION BY HASH (id) */;",
			want: []string{
				"punct:)", "word:engine", "punct:=", "word:innodb",
				"word:partition", "word:by", "word:hash", "punct:(", "word:id", "punct:)",
				"terminator:;",
			},
		},
		{
			name: "a wrapper inside a column definition",
			src:  "`pt` point NOT NULL /*!80003 SRID 4326 */,",
			want: []string{
				"quoted-ident:pt", "word:point", "word:not", "word:null",
				"word:srid", "number:4326", "punct:,",
			},
		},
	})
}

func TestLexDelimiter(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "the default delimiter is a semicolon",
			src:  `SELECT 1;`,
			want: []string{"word:select", "number:1", "terminator:;"},
		},
		{
			name: "a body under a custom delimiter keeps its own semicolons",
			src:  "DELIMITER ;;\nBEGIN a; b; END;;\nDELIMITER ;\nSELECT 1;",
			want: []string{
				"word:begin", "word:a", "punct:;", "word:b", "punct:;", "word:end",
				"terminator:;;", "word:select", "number:1", "terminator:;",
			},
		},
		{
			name: "a lower case delimiter directive works",
			src:  "delimiter //\nSELECT 1//\n",
			want: []string{"word:select", "number:1", "terminator://"},
		},
		{
			name: "a delimiter directive that is not at the start of a line is a word",
			src:  `SELECT delimiter ;`,
			want: []string{"word:select", "word:delimiter", "terminator:;"},
		},
		{
			name: "a word that merely begins with the directive is a word",
			src:  "delimiters ;",
			want: []string{"word:delimiters", "terminator:;"},
		},
	})
}

// triggerBlock is a verbatim excerpt of "mysqldump --no-data" output from
// MySQL 8.0.46, not an invented one. Its shape is the whole reason
// kindTerminator exists: mysqldump includes triggers by default, wraps each in
// DELIMITER lines, and writes the CREATE across three executable comments whose
// last one closes AFTER the body and before the delimiter.
//
// A reader counts thirteen statements here - eight SET, the trigger, four SET -
// so the lexer has to produce exactly thirteen terminators. The semicolons
// after END IF and inside the body are not among them.
const triggerBlock = "/*!50003 SET @saved_cs_client      = @@character_set_client */ ;\n" +
	"/*!50003 SET @saved_cs_results     = @@character_set_results */ ;\n" +
	"/*!50003 SET @saved_col_connection = @@collation_connection */ ;\n" +
	"/*!50003 SET character_set_client  = latin1 */ ;\n" +
	"/*!50003 SET character_set_results = latin1 */ ;\n" +
	"/*!50003 SET collation_connection  = latin1_swedish_ci */ ;\n" +
	"/*!50003 SET @saved_sql_mode       = @@sql_mode */ ;\n" +
	"/*!50003 SET sql_mode              = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION' */ ;\n" +
	"DELIMITER ;;\n" +
	"/*!50003 CREATE*/ /*!50017 DEFINER=`root`@`localhost`*/ /*!50003 TRIGGER `trg_orders_bi` BEFORE INSERT ON `orders` FOR EACH ROW BEGIN\n" +
	"  IF NEW.note IS NULL THEN\n" +
	"    SET NEW.note = 'x;y';\n" +
	"  END IF;\n" +
	"END */;;\n" +
	"DELIMITER ;\n" +
	"/*!50003 SET sql_mode              = @saved_sql_mode */ ;\n" +
	"/*!50003 SET character_set_client  = @saved_cs_client */ ;\n" +
	"/*!50003 SET character_set_results = @saved_cs_results */ ;\n" +
	"/*!50003 SET collation_connection  = @saved_col_connection */ ;\n"

func TestLexTriggerBlockIsOneStatement(t *testing.T) {
	toks := lexed(t, triggerBlock)

	terminators := 0
	for _, tok := range toks {
		if tok.kind == kindTerminator {
			terminators++
		}
	}
	if want := 13; terminators != want {
		t.Errorf("terminators got = %v, want %v", terminators, want)
	}

	// The trigger's own CREATE has to be visible as SQL. If the executable
	// comment had been treated as a comment there would be no such word, and a
	// dump's triggers would vanish without a diagnostic.
	if !slices.ContainsFunc(toks, func(tok token) bool { return tok.isWord("trigger") }) {
		t.Error("no TRIGGER keyword in the token stream, want the wrapper unwrapped")
	}
	// The semicolon inside the body stayed ordinary punctuation, which is what
	// keeps the body from being cut into pieces.
	if !slices.ContainsFunc(toks, func(tok token) bool { return tok.isPunct(";") }) {
		t.Error("no plain semicolon in the token stream, want the body's own semicolons intact")
	}
}

func TestLexNumbers(t *testing.T) {
	runLexTable(t, []struct {
		name string
		src  string
		want []string
	}{
		{name: "an integer", src: `1`, want: []string{"number:1"}},
		{name: "a decimal", src: `1.5`, want: []string{"number:1.5"}},
		{name: "a decimal with no integer part", src: `.5`, want: []string{"number:.5"}},
		{name: "an exponent stays in one token", src: `1e3`, want: []string{"number:1e3"}},
		{name: "a signed exponent stays in one token", src: `1.5e-3`, want: []string{"number:1.5e-3"}},
		{name: "a hexadecimal literal is one token", src: `0x1F`, want: []string{"number:0x1F"}},
		{
			name: "a dotted name is not a number",
			src:  `db.orders`,
			want: []string{"word:db", "punct:.", "word:orders"},
		},
		{
			name: "a negative default is an operator and a number",
			src:  `-1`,
			want: []string{"punct:-", "number:1"},
		},
	})
}

// offsetSource is a slice of real mysqldump output, chosen because it exercises
// every token kind at once.
const offsetSource = "/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;\n" +
	"\n" +
	"CREATE TABLE `customers` (\n" +
	"  `id` bigint NOT NULL AUTO_INCREMENT COMMENT 'customer it''s id',\n" +
	"  `ratio` decimal(10,2) unsigned DEFAULT NULL,\n" +
	"  `kind` enum('a','b,c') NOT NULL DEFAULT 'a',\n" +
	"  `uid` char(36) DEFAULT (uuid()),\n" +
	"  `sci` double DEFAULT 1.5e3,\n" +
	"  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  KEY `ix_customers_kind` (`kind`)\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='a\\ntwo line comment';\n"

func TestLexOffsetsCoverDelimiters(t *testing.T) {
	src := []byte(offsetSource)
	toks := lexed(t, offsetSource)

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

		// A terminator is the one token a slice cannot reproduce, because the
		// active delimiter is lexer state and a byte slice does not carry it:
		// re-lexing ";;" with the default delimiter yields two tokens. Its
		// offsets are still asserted, and nothing above the lexer is affected,
		// because no expression ever contains a terminator.
		if tok.kind == kindTerminator {
			if got := string(src[tok.pos:tok.end]); got != tok.text {
				t.Errorf("terminator slice got = %q, want %q", got, tok.text)
			}
			continue
		}

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

// lineNumbersSource mixes the four things that make line counting go wrong: a
// multi-line string literal, a multi-line block comment, a trigger body under a
// custom delimiter, and CRLF endings.
const lineNumbersSource = "CREATE TABLE `t` (\n" + // 1
	"  `a` int\n" + // 2
	") COMMENT='one\n" + // 3
	"two\n" + // 4
	"three';\n" + // 5
	"DELIMITER ;;\n" + // 6
	"CREATE TRIGGER `g` BEFORE INSERT ON `t` FOR EACH ROW BEGIN\n" + // 7
	"  SET @x = 1;\n" + // 8
	"END;;\n" + // 9
	"DELIMITER ;\n" + // 10
	"/* three\n" + // 11
	"   line\n" + // 12
	"   comment */\n" + // 13
	"alpha;\r\n" + // 14
	"beta;\r\n" + // 15
	"gamma;\n" // 16

func TestLexLineNumbers(t *testing.T) {
	toks := lexed(t, lineNumbersSource)

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "first statement", text: "table", want: 1},
		// The comment opens on line 3 and closes on line 5, so the token after
		// it proves that both newlines inside were counted.
		{name: "word after a multi-line string", text: "trigger", want: 7},
		{name: "word after the trigger body", text: "alpha", want: 14},
		{name: "word after a CRLF line", text: "beta", want: 15},
		{name: "last word", text: "gamma", want: 16},
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

	// The multi-line table comment itself is reported at the line it opened on.
	idx := slices.IndexFunc(toks, func(tok token) bool { return tok.kind == kindString })
	if idx < 0 {
		t.Fatal("no string token found")
	}
	if got := toks[idx].line; got != 3 {
		t.Errorf("line of the table comment got = %v, want %v", got, 3)
	}
}

func TestLexRejectsGarbage(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "a NUL byte",
			src:      "SELECT 1;\nSELECT \x00;",
			wantLine: 2,
			wantMsg:  `unexpected character "\x00"`,
		},
		{
			name:     "a stray control character",
			src:      "SELECT \x01;",
			wantLine: 1,
			wantMsg:  `unexpected character "\x01"`,
		},
		{
			name:     "an unterminated string literal reports the line it opened on",
			src:      "SELECT\n'abc",
			wantLine: 2,
			wantMsg:  "unterminated string literal",
		},
		{
			name:     "a trailing backslash does not terminate a string literal",
			src:      `'abc\`,
			wantLine: 1,
			wantMsg:  "unterminated string literal",
		},
		{
			name:     "an unterminated backtick identifier",
			src:      "SELECT\n\n`abc",
			wantLine: 3,
			wantMsg:  "unterminated quoted identifier",
		},
		{
			name:     "an empty backtick pair",
			src:      "SELECT `` FROM `t`",
			wantLine: 1,
			wantMsg:  "zero-length quoted identifier",
		},
		{
			name:     "an unterminated block comment",
			src:      "SELECT 1;\n/* abc\nmore\n",
			wantLine: 2,
			wantMsg:  "unterminated block comment",
		},
		{
			name:     "an unterminated executable comment reports where it opened",
			src:      "SELECT 1;\n\n/*!50100 PARTITION BY HASH (id)\n",
			wantLine: 3,
			wantMsg:  "unterminated executable comment",
		},
		{
			name:     "a close with no open wrapper",
			src:      "SELECT 1;\n*/",
			wantLine: 2,
			wantMsg:  `unexpected "*/" outside a comment`,
		},
		{
			name:     "a DELIMITER line that names nothing",
			src:      "SELECT 1;\nDELIMITER\n",
			wantLine: 2,
			wantMsg:  "DELIMITER names no delimiter",
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

func TestLexAlwaysEndsWithEOF(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "empty input", src: ""},
		{name: "whitespace only", src: " \t\n\r\f\v"},
		{name: "comments only", src: "-- nothing\n# nor here\n/* nor here */\n"},
		{name: "a whole trigger block", src: triggerBlock},
		{name: "a whole CREATE TABLE", src: offsetSource},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks := lexed(t, tt.src)
			eofs := 0
			for _, tok := range toks {
				if tok.kind == kindEOF {
					eofs++
				}
			}
			if eofs != 1 {
				t.Errorf("eof tokens got = %v, want 1", eofs)
			}
			last := toks[len(toks)-1]
			if last.kind != kindEOF {
				t.Fatalf("last token kind got = %v, want %v", last.kind, kindEOF)
			}
			if last.pos != len(tt.src) || last.end != len(tt.src) {
				t.Errorf("eof offsets got = [%d,%d), want [%d,%d)",
					last.pos, last.end, len(tt.src), len(tt.src))
			}
		})
	}
}

// FuzzLex asserts the invariants every caller above the lexer depends on:
// lexing never panics, and a successful result describes ranges inside the
// input, in order. The seeds are the shapes a MySQL dump is made of, and the
// DELIMITER seeds are there because that rule is the one with no PostgreSQL
// counterpart to have been tested by proxy.
func FuzzLex(f *testing.F) {
	seeds := []string{
		"`a``b`",
		"'a''b'",
		`'a\nb'`,
		`'a\%b'`,
		`"a""b"`,
		"/* /* */",
		"/*!50100 x */",
		"/*!",
		"*/",
		"--x",
		"-- x",
		"#x",
		"DELIMITER ;;\na;;\nDELIMITER ;\n",
		"DELIMITER\n",
		"delimiter //\na//",
		"0x1F",
		"1.5e3",
		"a.b",
		"$a",
		offsetSource,
		lineNumbersSource,
		triggerBlock,
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
