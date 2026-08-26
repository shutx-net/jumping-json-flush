package mysql

import (
	"bytes"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Statement splitting
// ---------------------------------------------------------------------------

// parse turns the bytes of a dump into the intermediate representation.
//
// Warnings are appended to d in source order; only structurally broken SQL
// returns an error, and it is returned unwrapped so that the CLI alone decides
// which exit code it carries.
//
// The order of the two things that can name the database IS the precedence
// rule. A USE statement is read while the statements are walked and takes the
// name only when nothing has claimed it yet, so the FIRST USE wins; the banner
// is read afterwards and therefore only when no USE said anything. Both are
// hints: a dump with no banner and no USE is a legitimate input, and the CLI's
// -database flag overrides whatever is found here.
func parse(src []byte, d *diagList) (*myDump, error) {
	out := &myDump{}
	out.ServerVersion, out.ServerVersionLine = serverVersion(src)

	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	for _, stmt := range splitStatements(toks) {
		if skippable(stmt) {
			continue
		}
		p := &parser{toks: stmt}
		if err := p.dispatch(src, out, d); err != nil {
			return nil, err
		}
	}
	if out.Database == "" {
		out.Database = headerDatabase(src)
	}
	return out, nil
}

// splitStatements cuts a token stream at every top-level terminator.
//
// This is its PostgreSQL sibling with one word changed: kindTerminator where
// that one tests for a ';'. Keeping the two the same shape is exactly what the
// lexer's DELIMITER handling bought - a splitter that had to know about a
// client directive would be a splitter that could get a trigger body wrong.
//
// Only the paren depth has to be tracked here. Every other place a terminator
// can hide - a string, a comment, a trigger body under a custom delimiter - was
// already resolved into a single token or dropped by the lexer, which is
// precisely why this package tokenizes instead of scanning lines. Empty runs,
// as produced by ";;" under the default delimiter, are dropped, and a trailing
// statement without its terminator is still returned.
func splitStatements(toks []token) [][]token {
	var out [][]token
	depth, start := 0, 0
	for i, tok := range toks {
		switch {
		case tok.kind == kindEOF:
			if i > start {
				out = append(out, toks[start:i])
			}
		case tok.isPunct("("):
			depth++
		case tok.isPunct(")"):
			if depth > 0 {
				depth--
			}
		case tok.kind == kindTerminator && depth == 0:
			if i > start {
				out = append(out, toks[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Cursor
// ---------------------------------------------------------------------------

// parser is a cursor over the tokens of ONE statement. Every handler is entered
// with the cursor on the first token of its statement and consumes its own
// leading keywords, which keeps the dispatch below a pure lookahead.
type parser struct {
	toks []token
	i    int
}

// peek returns the token at the cursor, or a synthetic end-of-statement token
// carrying the line of the last real token, so no caller has to bounds-check.
func (p *parser) peek() token {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	if len(p.toks) > 0 {
		last := p.toks[len(p.toks)-1]
		return token{kind: kindEOF, line: last.line, pos: last.end, end: last.end}
	}
	return token{kind: kindEOF, line: 1}
}

// next returns the token at the cursor and advances past it.
func (p *parser) next() token {
	tok := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return tok
}

// done reports whether the statement has been consumed.
func (p *parser) done() bool { return p.i >= len(p.toks) }

// wordAt returns the unquoted word n tokens ahead, or "" when that token is not
// one. It is the lookahead the dispatch and the element classifiers use, and it
// consumes nothing.
//
// That it returns "" for a BACKTICK identifier is what makes a column honestly
// named `key` or `check` parse: MySQL lets a reserved word be a column name
// when it is quoted, and a quoted name is a kindQuotedIdent.
//
// It is the only lookahead this cursor has, where the PostgreSQL sibling also
// needs a punctAt. That one exists so isConstraintStart can look past EXCLUDE,
// which PostgreSQL does not reserve, and so parseIndexElement can tell a column
// from a function call. MySQL reserves all nine of its table-element keywords
// and writes a functional key part in parentheses of its own, so both questions
// are answered by the token at the cursor alone.
func (p *parser) wordAt(n int) string {
	if p.i+n >= len(p.toks) {
		return ""
	}
	tok := p.toks[p.i+n]
	if tok.kind != kindWord {
		return ""
	}
	return tok.text
}

// atWord reports whether the cursor is on the keyword w, without consuming it.
func (p *parser) atWord(w string) bool { return p.peek().isWord(w) }

// acceptWord consumes the keyword w when it is at the cursor.
func (p *parser) acceptWord(w string) bool {
	if p.atWord(w) {
		p.i++
		return true
	}
	return false
}

// acceptWords consumes a whole keyword sequence, or nothing at all. The cursor
// is restored on a partial match, which is what lets "not null" be tried before
// "not secondary" without either stealing the other's first word.
func (p *parser) acceptWords(ws ...string) bool {
	save := p.i
	for _, w := range ws {
		if !p.acceptWord(w) {
			p.i = save
			return false
		}
	}
	return true
}

// acceptPunct consumes the punctuation s when it is at the cursor.
func (p *parser) acceptPunct(s string) bool {
	if p.peek().isPunct(s) {
		p.i++
		return true
	}
	return false
}

// expectPunct consumes s or reports where it should have been.
func (p *parser) expectPunct(s string) error {
	if p.acceptPunct(s) {
		return nil
	}
	return errAt(p.peek().line, "expected %q, got %s", s, describe(p.peek()))
}

// skipBalancedParens consumes a "(" ... ")" group, however deeply nested. It is
// how an unrepresentable expression - a CHECK body, a generated column's
// expression, a partition definition - is stepped over without being
// understood.
func (p *parser) skipBalancedParens() error {
	line := p.peek().line
	if err := p.expectPunct("("); err != nil {
		return err
	}
	for depth := 1; depth > 0; {
		if p.done() {
			return errAt(line, "unterminated parenthesis")
		}
		switch tok := p.next(); {
		case tok.isPunct("("):
			depth++
		case tok.isPunct(")"):
			depth--
		}
	}
	return nil
}

// skipElement consumes the rest of one list element: everything up to the comma
// that ends it, or to the parenthesis that ends the list it belongs to.
func (p *parser) skipElement() {
	for depth := 0; !p.done(); {
		tok := p.peek()
		if depth == 0 && (tok.isPunct(",") || tok.isPunct(")")) {
			return
		}
		switch {
		case tok.isPunct("("):
			depth++
		case tok.isPunct(")"):
			depth--
		}
		p.i++
	}
}

// ---------------------------------------------------------------------------
// Names and expressions
// ---------------------------------------------------------------------------

// identifier reads one SQL identifier. Unquoted words arrive already folded to
// lower case and backtick-quoted ones keep their case.
//
// Nothing is validated here. Whether an identifier can be written to a design
// document at all is decided in build.go.
func (p *parser) identifier() (string, error) {
	tok := p.peek()
	if tok.kind != kindWord && tok.kind != kindQuotedIdent {
		return "", errAt(tok.line, "expected an identifier, got %s", describe(tok))
	}
	p.i++
	return tok.text, nil
}

// dottedName reads a dot-separated name of any length, together with the line
// it starts on.
func (p *parser) dottedName() ([]string, int, error) {
	line := p.peek().line
	var parts []string
	for {
		id, err := p.identifier()
		if err != nil {
			return nil, line, err
		}
		parts = append(parts, id)
		if !p.acceptPunct(".") {
			return parts, line, nil
		}
	}
}

// qualifiedName reads a name of one or two parts. MySQL's two parts are
// database.table; three (there is no third level to name) are an error, because
// guessing which part is which would be worse than saying so.
func (p *parser) qualifiedName() (qname, error) {
	parts, line, err := p.dottedName()
	if err != nil {
		return qname{}, err
	}
	switch len(parts) {
	case 1:
		return qname{Name: parts[0], Line: line}, nil
	case 2:
		return qname{Schema: parts[0], Name: parts[1], Line: line}, nil
	}
	return qname{}, errAt(line, "qualified name %s has too many parts", strings.Join(parts, "."))
}

// columnList reads "( ident [, ident]* )", the plain list MySQL's FOREIGN KEY
// and REFERENCES clauses take. A key or index column list is richer and is read
// by indexColumnList instead.
func (p *parser) columnList() ([]string, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	var cols []string
	for {
		col, err := p.identifier()
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)
		if p.acceptPunct(",") {
			continue
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return cols, nil
	}
}

// indexColumns is what one key or index column list amounted to: the columns
// the document can name, and the three ways MySQL's key_part grammar can say
// something the document cannot hold.
//
// A struct rather than three extra return values, because the three flags are
// read together by one caller that turns them into diagnostics, and a
// five-value signature would put the reader in charge of remembering their
// order.
type indexColumns struct {
	Names []string
	// Prefixed lists the columns that carried a prefix length, as in
	// KEY ix (body(255)). The index still covers the column the document names,
	// so it is imported and the narrowing is reported, naming the column.
	Prefixed []string
	// Expression reports that at least one element was an expression rather
	// than a column, as in KEY ix ((lower(name))). The caller drops the whole
	// index: importing only its plain columns would describe an index that does
	// not exist.
	Expression bool
	// Descending reports that at least one column carried DESC. MySQL 8 has
	// real descending indexes and mysqldump writes the keyword back, so this is
	// a fact about the schema and not a spelling.
	Descending bool
}

// indexColumnList reads "( key_part [, key_part]* )", where a key_part is a
// column with an optional prefix length and an optional direction, or a
// parenthesised expression.
//
// A prefixed column and a functional index are DIFFERENT losses and are treated
// differently, which is the one place this file departs from the shape of
// internal/importer/postgres/stmt.go's parseIndexElement. A prefix length
// narrows an index that still covers the column the document will name, so
// importing it and saying what was dropped describes something true. Dropping
// the whole index instead would discard every index on a TEXT column in every
// real MySQL schema, which is most of them.
func (p *parser) indexColumnList() (indexColumns, error) {
	var out indexColumns
	if err := p.expectPunct("("); err != nil {
		return out, err
	}
	for {
		switch tok := p.peek(); {
		case tok.isPunct("("):
			// A functional key part. MySQL writes it doubly parenthesised.
			out.Expression = true
			if err := p.skipBalancedParens(); err != nil {
				return out, err
			}
		case tok.kind == kindWord || tok.kind == kindQuotedIdent:
			name, err := p.identifier()
			if err != nil {
				return out, err
			}
			if p.peek().isPunct("(") {
				out.Prefixed = append(out.Prefixed, name)
				if err := p.skipBalancedParens(); err != nil {
					return out, err
				}
			}
			out.Names = append(out.Names, name)
		default:
			return out, errAt(tok.line, "expected an index column, got %s", describe(tok))
		}
		// ASC is the default and says nothing; DESC does.
		if !p.acceptWord("asc") && p.acceptWord("desc") {
			out.Descending = true
		}
		if p.acceptPunct(",") {
			continue
		}
		if err := p.expectPunct(")"); err != nil {
			return out, err
		}
		return out, nil
	}
}

// exprText returns the verbatim source text of an expression, from the cursor to
// the first point at paren depth 0 where stop reports true.
//
// Slicing the source rather than joining token texts is the whole point: a
// default has to reach the document exactly as it was written, down to the
// doubled quote inside a string literal and the parentheses of DEFAULT (uuid())
// that MySQL 8.0.13 and later require. Internal whitespace is left alone here;
// build.go collapses it when it decides what fits in a document.
func (p *parser) exprText(src []byte, stop func(*parser) bool) string {
	start := p.peek().pos
	end := start
	for depth := 0; !p.done(); {
		if depth == 0 && stop(p) {
			break
		}
		tok := p.next()
		switch {
		case tok.isPunct("("):
			depth++
		case tok.isPunct(")"):
			depth--
		}
		end = tok.end
	}
	if end <= start {
		return ""
	}
	return strings.TrimSpace(string(src[start:end]))
}

// stopAtColumnAttribute ends a column DEFAULT expression.
//
// The comma or the closing parenthesis of the column list is not enough:
// mysqldump writes "`updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP
// ON UPDATE CURRENT_TIMESTAMP", so the expression also ends where the next
// column attribute begins. start is the cursor position the expression began
// at, because the FIRST token may legitimately be one of those keywords -
// "DEFAULT NULL" is a default, not an empty one followed by an attribute.
func stopAtColumnAttribute(start int) func(*parser) bool {
	return func(p *parser) bool {
		tok := p.peek()
		if tok.isPunct(",") || tok.isPunct(")") {
			return true
		}
		return p.i > start && tok.kind == kindWord && startsColumnAttribute(tok.text)
	}
}

// startsColumnAttribute reports whether w begins a column attribute.
//
// This is MySQL's word list and not the PostgreSQL sibling's, and it is the
// SINGLE list that decides where a default ends, which is why it is tested
// directly rather than only through a column. "on" has to be here so that
// DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP ends the default at the
// right token, and it is the one real dumps hit. "first" and "after" are here
// for ALTER TABLE ... ADD COLUMN, where the definition is followed by a
// position instead of by a comma.
//
// Several of these words - COMMENT, COLLATE, CHARSET, STORAGE, FIRST, AFTER -
// are NOT reserved in MySQL, so a column may honestly be called one of them
// without quoting. That is not what this list decides. It decides where a
// DEFAULT expression ends, and a default at paren depth 0 is a literal, a
// keyword such as CURRENT_TIMESTAMP, or a parenthesised expression; none of the
// three can be continued by any word here.
func startsColumnAttribute(w string) bool {
	switch w {
	case "not", "null", "default", "auto_increment", "unique", "primary", "key",
		"comment", "collate", "character", "charset", "generated", "as", "stored",
		"virtual", "references", "check", "constraint", "invisible", "visible",
		"srid", "column_format", "storage", "on", "first", "after":
		return true
	}
	return false
}

// errAt builds a parse failure that names its line, so that a parser error and
// a lexer error read the same way.
func errAt(line int, format string, args ...any) error {
	return &syntaxError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// describe names a token in an error message.
func describe(t token) string {
	if t.kind == kindEOF {
		return "end of statement"
	}
	return fmt.Sprintf("%s %q", t.kind, t.text)
}

// ---------------------------------------------------------------------------
// Dump header
// ---------------------------------------------------------------------------

// headerLines is how far into a dump the mysqldump banner is looked for. The
// banner is written in the first few lines; scanning the whole file would only
// let a table comment further down impersonate it.
const headerLines = 64

// versionBanner is the header comment mysqldump writes about the SERVER it read
// from. The line reads "-- Server version\t8.0.46".
const versionBanner = "Server version"

// databaseBanner is the field of the header comment that names the database the
// dump was taken from. The line reads "-- Host: localhost    Database: shop".
const databaseBanner = "Database: "

// serverVersion reads the server version out of the header banner, with the
// line it was found on. It returns ("", 0) when the dump carries no banner,
// which is not an error: a hand-written, edited or concatenated dump simply
// cannot be version checked.
//
// It reads raw bytes because the lexer throws comments away, and the banner is
// a comment - the same reason internal/importer/postgres/parse.go's dumpVersion
// does.
func serverVersion(src []byte) (string, int) {
	for line, rest := 1, src; line <= headerLines && len(rest) > 0; line++ {
		var cur []byte
		cur, rest = nextLine(rest)
		if !bytes.HasPrefix(bytes.TrimSpace(cur), []byte("--")) {
			continue
		}
		if _, after, ok := bytes.Cut(cur, []byte(versionBanner)); ok {
			return string(bytes.TrimSpace(after)), line
		}
	}
	return "", 0
}

// headerDatabase returns the database name the banner names, or "".
//
// It is a hint and nothing more, and parse decides what beats it. A dump taken
// with --databases names only the FIRST database here and says the rest with
// USE statements, which is one of the two reasons a USE wins.
func headerDatabase(src []byte) string {
	for line, rest := 1, src; line <= headerLines && len(rest) > 0; line++ {
		var cur []byte
		cur, rest = nextLine(rest)
		if !bytes.HasPrefix(bytes.TrimSpace(cur), []byte("--")) {
			continue
		}
		if _, after, ok := bytes.Cut(cur, []byte(databaseBanner)); ok {
			return string(bytes.TrimSpace(after))
		}
	}
	return ""
}

// nextLine splits off one line, without its terminator, and returns the rest.
func nextLine(b []byte) (line, rest []byte) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return bytes.TrimSuffix(b[:i], []byte("\r")), b[i+1:]
	}
	return b, nil
}
