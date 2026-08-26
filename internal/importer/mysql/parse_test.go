package mysql

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// mustParse parses src and fails the test if the dump is not well formed. The
// warnings are returned rather than asserted, because most tests care about the
// intermediate representation alone.
func mustParse(t *testing.T, src string) (*myDump, []Diagnostic) {
	t.Helper()
	var d diagList
	dump, err := parse([]byte(src), &d)
	if err != nil {
		t.Fatalf("parse returned error %v, want no error", err)
	}
	return dump, d.all()
}

// messages renders diagnostics as their strings, which is what a test asserts
// on: the line and the sentence together are the whole of what a reader gets.
func messages(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

// cursor lexes src and returns a parser over it, for the tests that exercise
// one production rather than a whole statement. The EOF token is dropped so
// that the cursor behaves exactly as one splitStatements produced.
func cursor(t *testing.T, src string) *parser {
	t.Helper()
	toks, err := lex([]byte(src))
	if err != nil {
		t.Fatalf("lex(%q) returned error %v, want no error", src, err)
	}
	return &parser{toks: toks[:len(toks)-1]}
}

// statementHeads lexes src and returns the first token of every statement, which
// is enough to say where the splitter cut.
func statementHeads(t *testing.T, src string) []string {
	t.Helper()
	toks, err := lex([]byte(src))
	if err != nil {
		t.Fatalf("lex(%q) returned error %v, want no error", src, err)
	}
	heads := []string{}
	for _, stmt := range splitStatements(toks) {
		heads = append(heads, stmt[0].text)
	}
	return heads
}

func TestSplitStatementsIgnoresSemicolonsInsideParens(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "two statements",
			src:  "SELECT 1; SELECT 2;",
			want: []string{"select", "select"},
		},
		{
			name: "a trailing statement without its terminator",
			src:  "SELECT 1; UPDATE t SET a = 1",
			want: []string{"select", "update"},
		},
		{
			name: "empty statements are dropped",
			src:  ";;SELECT 1;;",
			want: []string{"select"},
		},
		{
			name: "a semicolon inside parentheses",
			src:  "SELECT f(a;b); SELECT 2;",
			want: []string{"select", "select"},
		},
		{
			name: "a semicolon inside a string",
			src:  "INSERT INTO t VALUES (';');\nSELECT 1;",
			want: []string{"insert", "select"},
		},
		{
			name: "a semicolon inside a comment",
			src:  "-- one ; two\nSELECT 1;",
			want: []string{"select"},
		},
		{
			name: "a trigger body under a custom delimiter is one statement",
			src: "DELIMITER ;;\n" +
				"CREATE TRIGGER g BEFORE INSERT ON t FOR EACH ROW BEGIN\n" +
				"  SET @a = 1;\n" +
				"  SET @b = 2;\n" +
				"END;;\n" +
				"DELIMITER ;\n" +
				"SELECT 1;",
			want: []string{"create", "select"},
		},
		{
			name: "only comments",
			src:  "-- nothing here\n# nor here\n/* nor here */\n",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statementHeads(t, tt.src); !slices.Equal(got, tt.want) {
				t.Errorf("splitStatements(%q) got = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestQualifiedNameRejectsThreeParts(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		want    string
		wantErr bool
	}{
		{name: "a bare table name", src: "`orders`", want: "orders"},
		{name: "a database qualified name", src: "`shop`.`orders`", want: "shop.orders"},
		{name: "three parts", src: "`a`.`b`.`c`", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cursor(t, tt.src).qualifiedName()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("qualifiedName(%q) returned no error, want one", tt.src)
				}
				var se *syntaxError
				if !errors.As(err, &se) {
					t.Fatalf("error type got = %T, want *syntaxError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("qualifiedName(%q) returned error %v, want no error", tt.src, err)
			}
			if got.String() != tt.want {
				t.Errorf("qualifiedName(%q) got = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

// TestExprTextIsVerbatim pins the promise the schema makes about a default: it
// reaches the document as it was written, not as the tokens would render.
func TestExprTextIsVerbatim(t *testing.T) {
	tests := []struct {
		name string
		def  string
	}{
		{name: "a doubled quote stays doubled", def: `'it''s'`},
		{name: "internal spacing inside a literal survives", def: `'a  b'`},
		{name: "a function call keeps its parentheses", def: `(uuid())`},
		{name: "a parenthesised arithmetic default", def: `((1 + 2))`},
		{name: "a bit literal is not two tokens joined", def: `b'10101010'`},
		{name: "a quoted negative number", def: `'-1'`},
		{name: "a comma inside a literal", def: `'a, b (c)'`},
		{name: "a backslash escape survives", def: `'C:\\tmp'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "CREATE TABLE `t` (\n  `c` varchar(64) DEFAULT " + tt.def + "\n);"
			dump, _ := mustParse(t, src)
			col := dump.Tables[0].Columns[0]
			if !col.HasDefault {
				t.Fatalf("column has no default, want %q", tt.def)
			}
			if col.Default != tt.def {
				t.Errorf("default got = %q, want %q", col.Default, tt.def)
			}
		})
	}
}

// TestExprTextSpanningTwoLinesKeepsItsBytes is separate because the expected
// value contains the newline the test is about.
func TestExprTextSpanningTwoLinesKeepsItsBytes(t *testing.T) {
	dump, _ := mustParse(t, "CREATE TABLE `t` (\n  `c` varchar(64) DEFAULT 'one\ntwo'\n);")
	if got, want := dump.Tables[0].Columns[0].Default, "'one\ntwo'"; got != want {
		t.Errorf("default got = %q, want %q", got, want)
	}
}

// TestStopAtColumnAttribute is a direct test of the single list that decides
// where a DEFAULT ends. It is tested here rather than only through a column
// because getting it wrong silently corrupts a default rather than failing.
func TestStopAtColumnAttribute(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		want      string
		wantOnUpd bool
		wantNull  bool
	}{
		{
			name:      "an automatic update rule ends the default",
			src:       "`c` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP",
			want:      "CURRENT_TIMESTAMP",
			wantOnUpd: true,
		},
		{
			name:     "an explicit NULL default followed by NOT NULL",
			src:      "`c` int DEFAULT NULL NOT NULL",
			want:     "NULL",
			wantNull: false,
		},
		{
			name: "a parenthesised default keeps its parentheses",
			src:  "`c` json DEFAULT (JSON_ARRAY()) COMMENT 'x'",
			want: "(JSON_ARRAY())",
		},
		{
			name: "a default followed by a comment",
			src:  "`c` int DEFAULT 0 COMMENT 'x'",
			want: "0",
		},
		{
			name: "a default followed by a column position",
			src:  "`c` int DEFAULT 0 AFTER `b`",
			want: "0",
		},
		{
			name: "a fractional precision belongs to the default",
			src:  "`c` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)",
			want: "CURRENT_TIMESTAMP(3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "CREATE TABLE `t` (\n  " + tt.src + "\n);"
			dump, _ := mustParse(t, src)
			col := dump.Tables[0].Columns[0]
			if col.Default != tt.want {
				t.Errorf("default got = %q, want %q", col.Default, tt.want)
			}
			if col.OnUpdateCurrentTimestamp != tt.wantOnUpd {
				t.Errorf("on update flag got = %v, want %v", col.OnUpdateCurrentTimestamp, tt.wantOnUpd)
			}
		})
	}
}

func TestServerVersionReadsTheBanner(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		want     string
		wantLine int
	}{
		{
			name:     "a real mysqldump header",
			src:      wholeDump,
			want:     "8.0.46-0ubuntu0.24.04.3",
			wantLine: 5,
		},
		{
			name: "an older banner",
			src: "-- MySQL dump 10.13  Distrib 8.0.36, for Linux (x86_64)\n" +
				"--\n" +
				"-- Host: localhost    Database: shop\n" +
				"-- ------------------------------------------------------\n" +
				"-- Server version\t8.0.36\n",
			want:     "8.0.36",
			wantLine: 5,
		},
		{
			name: "a file with no banner",
			src:  "CREATE TABLE `t` (`a` int);\n",
		},
		{
			name: "a table comment further down cannot impersonate the banner",
			src: strings.Repeat("-- filler\n", headerLines) +
				"-- Server version\t9.9.9\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, line := serverVersion([]byte(tt.src))
			if got != tt.want || line != tt.wantLine {
				t.Errorf("serverVersion got = (%q, %v), want (%q, %v)", got, line, tt.want, tt.wantLine)
			}
		})
	}
}

func TestHeaderDatabaseReadsTheHostLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "a real mysqldump header", src: wholeDump, want: "jjf_probe5"},
		{
			name: "a header with no database, as --all-databases writes",
			src:  "-- Host: localhost    Database: \n-- Server version\t8.0.46\n",
		},
		{name: "a file with no banner", src: "CREATE TABLE `t` (`a` int);\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := headerDatabase([]byte(tt.src)); got != tt.want {
				t.Errorf("headerDatabase got = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUseStatementWins pins the precedence parse expresses through the order of
// its two reads. A dump taken with --databases names only the first database in
// its banner and says the rest with USE, so a USE has to beat the banner - and
// the FIRST one has to win, or a dump of several databases would silently
// become the last of them.
func TestUseStatementWins(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the banner alone",
			src:  "-- Host: localhost    Database: shop\nCREATE TABLE `t` (`a` int);\n",
			want: "shop",
		},
		{
			name: "a USE beats the banner",
			src:  "-- Host: localhost    Database: shop\nUSE `warehouse`;\nCREATE TABLE `t` (`a` int);\n",
			want: "warehouse",
		},
		{
			name: "the first of several USE statements wins",
			src:  "USE `first`;\nCREATE TABLE `t` (`a` int);\nUSE `second`;\nCREATE TABLE `u` (`a` int);\n",
			want: "first",
		},
		{
			name: "neither",
			src:  "CREATE TABLE `t` (`a` int);\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if dump.Database != tt.want {
				t.Errorf("database got = %q, want %q", dump.Database, tt.want)
			}
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
		})
	}
}

func TestIndexColumnListReportsAPrefixLength(t *testing.T) {
	cols, err := cursor(t, "(`a`, `body`(255), `c`(10))").indexColumnList()
	if err != nil {
		t.Fatalf("indexColumnList returned error %v, want no error", err)
	}
	if want := []string{"a", "body", "c"}; !slices.Equal(cols.Names, want) {
		t.Errorf("names got = %v, want %v", cols.Names, want)
	}
	if want := []string{"body", "c"}; !slices.Equal(cols.Prefixed, want) {
		t.Errorf("prefixed got = %v, want %v", cols.Prefixed, want)
	}
	if cols.Expression {
		t.Error("expression got = true, want false")
	}
}

func TestIndexColumnListReportsAnExpression(t *testing.T) {
	cols, err := cursor(t, "((lower(`name`)))").indexColumnList()
	if err != nil {
		t.Fatalf("indexColumnList returned error %v, want no error", err)
	}
	if !cols.Expression {
		t.Error("expression got = false, want true")
	}
	if len(cols.Names) != 0 {
		t.Errorf("names got = %v, want none", cols.Names)
	}
}

func TestIndexColumnListReportsADescendingColumn(t *testing.T) {
	cols, err := cursor(t, "(`b`,`c` DESC)").indexColumnList()
	if err != nil {
		t.Fatalf("indexColumnList returned error %v, want no error", err)
	}
	if want := []string{"b", "c"}; !slices.Equal(cols.Names, want) {
		t.Errorf("names got = %v, want %v", cols.Names, want)
	}
	if !cols.Descending {
		t.Error("descending got = false, want true")
	}

	// ASC is the default, so it is not a loss and must not be reported.
	plain, err := cursor(t, "(`b` ASC)").indexColumnList()
	if err != nil {
		t.Fatalf("indexColumnList returned error %v, want no error", err)
	}
	if plain.Descending {
		t.Error("descending got = true for an ASC column, want false")
	}
}

// TestDiagnosticsRenderTheirLine covers the two shapes a warning reaches the
// terminal in. A zero line means the warning is about the dump as a whole
// rather than a place in it, which is the one case that must not print
// "line 0".
func TestDiagnosticsRenderTheirLine(t *testing.T) {
	var d diagList
	d.warnf(12, "table %s: partitioning is not imported", "parts")
	d.warnf(0, "this dump names no database")
	want := []string{
		"line 12: table parts: partitioning is not imported",
		"this dump names no database",
	}
	if got := messages(d.all()); !slices.Equal(got, want) {
		t.Errorf("diagnostics got = %v, want %v", got, want)
	}
	if (&diagList{}).all() != nil {
		t.Error("an empty list got = non-nil, want nil")
	}
}

// TestSyntaxErrorNamesItsLine pins the one sentence a caller sees when a dump
// cannot be read at all. A dump is far too large to search by hand, so the line
// is not decoration.
func TestSyntaxErrorNamesItsLine(t *testing.T) {
	_, err := parse([]byte("CREATE TABLE `t` (\n  `a`\n);"), &diagList{})
	if err == nil {
		t.Fatal("parse returned no error, want one")
	}
	if got, want := err.Error(), "line 3: expected a type name, got punct \")\""; got != want {
		t.Errorf("error got = %q, want %q", got, want)
	}
}
