package postgres

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
func parse(src []byte, d *diagList) (*pgDump, error) {
	toks, connected, err := lexDump(src)
	if err != nil {
		return nil, err
	}
	out := &pgDump{Database: connected}
	out.DumpVersion, out.DumpVersionLine = dumpVersion(src)

	for _, stmt := range splitStatements(toks) {
		if skippable(stmt) {
			continue
		}
		p := &parser{toks: stmt}
		if err := p.dispatch(src, out, d); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// splitStatements cuts a token stream at every top-level semicolon.
//
// Only the paren depth has to be tracked here. Every other place a semicolon
// can hide - a string, a comment, a dollar-quoted function body - was already
// resolved into a single token or dropped by the lexer, which is precisely why
// this package tokenizes instead of scanning lines. Empty runs, as produced by
// ";;", are dropped, and a trailing statement without its semicolon is still
// returned.
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
		case tok.isPunct(";") && depth == 0:
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

// punctAt reports whether the token n ahead is the punctuation s.
func (p *parser) punctAt(n int, s string) bool {
	if p.i+n >= len(p.toks) {
		return false
	}
	return p.toks[p.i+n].isPunct(s)
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
// "not deferrable" without either stealing the other's first word.
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
// how an unrepresentable expression - a CHECK body, an identity option list - is
// stepped over without being understood.
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
// lower case and quoted ones keep their case, which is exactly how PostgreSQL
// resolves them.
//
// Nothing is validated here. Whether an identifier can be written to a design
// document at all is decided in build.go, where it is also known whether the
// object it names survived the schema filter.
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

// qualifiedName reads a name of one or two parts. Three parts
// (database.schema.table) are an error: pg_dump never writes them, and guessing
// which part is which would be worse than saying so.
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

// columnList reads "( ident [, ident]* )".
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

// exprText returns the verbatim source text of an expression, from the cursor to
// the first point at paren depth 0 where stop reports true.
//
// Slicing the source rather than joining token texts is the whole point: a
// default has to reach the document exactly as it was written, down to the
// quoting inside nextval('public.users_id_seq'::regclass). Internal whitespace
// is left alone here; build.go collapses it when it decides what fits in a
// document.
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

// stopAtColumnConstraint ends a column DEFAULT expression.
//
// The comma or the closing parenthesis of the column list is not enough:
// pg_dump writes "created_at timestamp without time zone DEFAULT now() NOT
// NULL", so the expression also ends where the next column constraint begins.
// start is the cursor position the expression began at, because the FIRST token
// may legitimately be one of those keywords - "DEFAULT NULL" is a default, not
// an empty one followed by a constraint.
func stopAtColumnConstraint(start int) func(*parser) bool {
	return func(p *parser) bool {
		tok := p.peek()
		if tok.isPunct(",") || tok.isPunct(")") {
			return true
		}
		return p.i > start && tok.kind == kindWord && startsColumnConstraint(p)
	}
}

// startsColumnConstraint reports whether the cursor is on the beginning of a
// column constraint rather than on a word a DEFAULT may continue with.
//
// Eleven of the thirteen words are reserved in PostgreSQL, so no expression can
// legitimately continue with one and the word alone decides. GENERATED and NO
// are not: pg_get_keywords() calls both unreserved in PostgreSQL 16, so either
// may be the name of a function or a table, and pg_dump then writes
// "DEFAULT public.no()" into a column definition. Read by the word alone, the
// expression is cut at "no" and the "(" that follows walks the column loop onto
// a ")" it reads as the end of the column list - so the columns after it are
// lost too, which is the shape #57 is about.
//
// The two are therefore decided by what follows, the way isConstraintStart
// already decides EXCLUDE: GENERATED begins a clause only in GENERATED ALWAYS
// and GENERATED BY DEFAULT, and NO only in NO INHERIT. A column honestly called
// either still parses, and so does a default that calls one.
func startsColumnConstraint(p *parser) bool {
	switch p.wordAt(0) {
	case "not", "null", "primary", "unique", "references", "check",
		"constraint", "collate", "deferrable", "initially", "default":
		return true
	case "generated":
		return p.wordAt(1) == "always" || p.wordAt(1) == "by"
	case "no":
		return p.wordAt(1) == "inherit"
	}
	return false
}

// stopAtComma ends an expression at the comma between two ALTER TABLE actions.
func stopAtComma(p *parser) bool { return p.peek().isPunct(",") }

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

// headerLines is how far into a dump the pg_dump banner is looked for. The
// banner is written in the first few lines; scanning the whole file would only
// let a comment further down impersonate it.
const headerLines = 64

// versionBanner is the header comment pg_dump writes about itself.
const versionBanner = "Dumped by pg_dump version "

// dumpVersion reads the pg_dump version out of the header banner, with the line
// it was found on. It returns ("", 0) when the dump carries no banner, which is
// not an error: a hand-written or edited dump simply cannot be version checked.
//
// It reads raw bytes because the lexer throws comments away, and the banner is
// a comment.
func dumpVersion(src []byte) (string, int) {
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

// nextLine splits off one line, without its terminator, and returns the rest.
func nextLine(b []byte) (line, rest []byte) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return bytes.TrimSuffix(b[:i], []byte("\r")), b[i+1:]
	}
	return b, nil
}
