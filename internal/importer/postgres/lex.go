// Package postgres converts the output of "pg_dump --schema-only" into a jjf
// database design document.
//
// It targets the output SHAPE of pg_dump rather than the PostgreSQL grammar:
// the statements pg_dump emits, in the order it emits them, are what this
// package understands. Arbitrary hand written SQL is best effort at most.
//
// Nothing outside the standard library and internal/{model,exitcode} is
// imported, so the importer keeps the project's no-CGO, single-binary
// property.
//
// The package is layered. lex.go turns bytes into tokens, parse.go and stmt.go
// turn tokens into a PostgreSQL shaped intermediate representation, and
// build.go resolves that representation into a model.Document. Parsing never
// builds the document directly: pg_dump writes CREATE TABLE first and its
// constraints, indexes and comments afterwards, so forward references are
// unavoidable.
package postgres

import (
	"bytes"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

// kind classifies a token. The parser dispatches on it, so the set is kept as
// small as the statements this package understands allow.
type kind uint8

// Token kinds produced by lex.
const (
	kindEOF kind = iota
	kindWord
	kindQuotedIdent
	kindString
	kindNumber
	kindPunct
)

// String names a kind for test failure messages and diagnostics. It is not a
// SQL term, only a label.
func (k kind) String() string {
	switch k {
	case kindEOF:
		return "eof"
	case kindWord:
		return "word"
	case kindQuotedIdent:
		return "quoted-ident"
	case kindString:
		return "string"
	case kindNumber:
		return "number"
	case kindPunct:
		return "punct"
	}
	return "unknown"
}

// token is one lexical element of a dump.
//
// text holds the DECODED value: a quoted identifier without its quotes and
// with doubled quotes collapsed, a string without its delimiters and with
// escapes resolved, an unquoted word folded to lower case, and the verbatim
// source slice for numbers and punctuation.
//
// pos and end are byte offsets into the source and cover the token INCLUDING
// its delimiters. They exist so that an expression such as a DEFAULT clause can
// be reproduced verbatim by slicing src[first.pos:last.end], which is the only
// way to honour the schema's promise that a default is "written verbatim".
type token struct {
	kind kind
	text string
	line int
	pos  int
	end  int
}

// isWord reports whether t is the unquoted keyword w. w must already be lower
// case, because lex folds every unquoted word on the way in.
func (t token) isWord(w string) bool { return t.kind == kindWord && t.text == w }

// isPunct reports whether t is the punctuation s.
func (t token) isPunct(s string) bool { return t.kind == kindPunct && t.text == s }

// syntaxError is a failure to tokenize or parse, carrying the line of the dump
// it was found on. Every diagnostic this package produces names a line, because
// a dump is far too large to search by hand.
type syntaxError struct {
	Line int
	Msg  string
}

// Error formats the failure with its line number.
func (e *syntaxError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

// lexer is the function-local state of one lex call. It is a struct rather than
// a set of free functions so that the source, the read offset and the line
// counter cannot drift apart; there is no package-level state anywhere.
type lexer struct {
	src  []byte
	i    int
	line int
	// atLineStart reports whether the next byte begins a line. psql backslash
	// commands are only recognised there, exactly as psql itself does.
	atLineStart bool
}

// lex tokenizes a dump.
//
// No regular expression is used here, or anywhere else in this package. A
// line-oriented regexp cannot see that a ';' sits inside a dollar-quoted
// function body, inside a string with doubled quotes, or inside a nested block
// comment, and every one of those appears in real pg_dump output.
//
// The returned slice always ends with a kindEOF token, so the parser never has
// to bounds-check its lookahead.
func lex(src []byte) ([]token, error) {
	l := &lexer{src: src, line: 1, atLineStart: true}
	// One token per four bytes is a cheap guess that avoids most regrowth.
	toks := make([]token, 0, len(src)/4)

	for {
		if err := l.skipIgnorable(); err != nil {
			return nil, err
		}
		if l.i >= len(l.src) {
			break
		}
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
	}
	return append(toks, token{kind: kindEOF, line: l.line, pos: len(src), end: len(src)}), nil
}

// advance consumes n bytes. It is the ONLY place the read offset moves, so the
// line counter and atLineStart can never fall out of step with it.
func (l *lexer) advance(n int) {
	for ; n > 0 && l.i < len(l.src); n-- {
		if l.src[l.i] == '\n' {
			l.line++
			l.atLineStart = true
		} else {
			l.atLineStart = false
		}
		l.i++
	}
}

// peek returns the byte off positions ahead of the read offset, or 0 at end of
// input. A real NUL byte in a dump is rejected as an unexpected character, so
// conflating the two loses nothing.
func (l *lexer) peek(off int) byte {
	if l.i+off >= len(l.src) {
		return 0
	}
	return l.src[l.i+off]
}

// skipIgnorable consumes every run of whitespace, comments and psql backslash
// lines at the read offset. It loops because the four can follow one another.
func (l *lexer) skipIgnorable() error {
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case isSpace(c):
			l.advance(1)
		case l.atLineStart && c == '\\':
			// \connect, \restrict and friends are psql commands, not SQL. They
			// run to the end of the line and mean nothing to this importer.
			l.skipToEndOfLine()
		case c == '-' && l.peek(1) == '-':
			// Checked before the operator rule so "--" never becomes a token.
			l.skipToEndOfLine()
		case c == '/' && l.peek(1) == '*':
			if err := l.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// skipToEndOfLine consumes up to but not including the next newline, leaving
// the newline itself to the whitespace rule so that the line counter stays in
// one place.
func (l *lexer) skipToEndOfLine() {
	for l.i < len(l.src) && l.src[l.i] != '\n' {
		l.advance(1)
	}
}

// skipBlockComment consumes a /* ... */ comment.
//
// PostgreSQL block comments NEST, so scanning for the first "*/" is wrong: it
// would end the comment in the middle of a commented-out block that itself
// contains one.
func (l *lexer) skipBlockComment() error {
	openLine := l.line
	l.advance(2)
	for depth := 1; depth > 0; {
		if l.i >= len(l.src) {
			return &syntaxError{Line: openLine, Msg: "unterminated block comment"}
		}
		switch {
		case l.src[l.i] == '/' && l.peek(1) == '*':
			depth++
			l.advance(2)
		case l.src[l.i] == '*' && l.peek(1) == '/':
			depth--
			l.advance(2)
		default:
			l.advance(1)
		}
	}
	return nil
}

// next reads the one token at the read offset, which is known to be neither
// whitespace nor a comment.
func (l *lexer) next() (token, error) {
	c := l.src[l.i]
	switch {
	case c == '\'':
		return l.lexString()
	case (c == 'e' || c == 'E') && l.peek(1) == '\'':
		return l.lexEscapeString()
	case c == '$':
		return l.lexDollar()
	case c == '"':
		return l.lexQuotedIdent()
	case isIdentStart(c):
		return l.lexWord(), nil
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		return l.lexNumber(), nil
	case isSinglePunct(c):
		return l.lexRun(1), nil
	case isOperatorByte(c):
		// A maximal run, which is what makes "::" a single token.
		n := 0
		for l.i+n < len(l.src) && isOperatorByte(l.src[l.i+n]) {
			n++
		}
		return l.lexRun(n), nil
	}
	return token{}, &syntaxError{Line: l.line, Msg: fmt.Sprintf("unexpected character %q", string(rune(c)))}
}

// lexRun emits the next n bytes verbatim as punctuation.
func (l *lexer) lexRun(n int) token {
	start, line := l.i, l.line
	l.advance(n)
	return token{kind: kindPunct, text: string(l.src[start:l.i]), line: line, pos: start, end: l.i}
}

// lexString reads a single quoted string. A doubled quote inside is one literal
// quote and does not terminate the string.
func (l *lexer) lexString() (token, error) {
	start, line := l.i, l.line
	l.advance(1)

	var b strings.Builder
	for {
		if l.i >= len(l.src) {
			return token{}, &syntaxError{Line: line, Msg: "unterminated string literal"}
		}
		c := l.src[l.i]
		if c == '\'' {
			if l.peek(1) == '\'' {
				b.WriteByte('\'')
				l.advance(2)
				continue
			}
			l.advance(1)
			break
		}
		b.WriteByte(c)
		l.advance(1)
	}
	return token{kind: kindString, text: b.String(), line: line, pos: start, end: l.i}, nil
}

// lexEscapeString reads an E'...' string, in which a backslash escapes the byte
// after it.
//
// \xNN, \uNNNN and octal escapes are deliberately NOT decoded; they pass through
// as the characters that follow the backslash. The importer reads string VALUES
// only for COMMENT ON text and for the sequence name inside nextval(), so an
// imperfect decode can garble a comment but can never corrupt structure.
func (l *lexer) lexEscapeString() (token, error) {
	start, line := l.i, l.line
	l.advance(2) // E'

	var b strings.Builder
	for {
		if l.i >= len(l.src) {
			return token{}, &syntaxError{Line: line, Msg: "unterminated string literal"}
		}
		c := l.src[l.i]
		switch {
		case c == '\\':
			if l.i+1 >= len(l.src) {
				return token{}, &syntaxError{Line: line, Msg: "unterminated string literal"}
			}
			b.WriteByte(unescape(l.peek(1)))
			l.advance(2)
		case c == '\'' && l.peek(1) == '\'':
			b.WriteByte('\'')
			l.advance(2)
		case c == '\'':
			l.advance(1)
			return token{kind: kindString, text: b.String(), line: line, pos: start, end: l.i}, nil
		default:
			b.WriteByte(c)
			l.advance(1)
		}
	}
}

// unescape resolves one backslash escape. Anything unknown yields the escaped
// byte itself, which is what PostgreSQL does too.
func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	}
	return c
}

// lexDollar reads a dollar-quoted string, or a lone '$' when the input is not
// one.
//
// The opening delimiter is $tag$ with an optional tag; the body ends at the
// next occurrence of that exact delimiter and has no escapes at all, which is
// why a literal search is both correct and fast here.
func (l *lexer) lexDollar() (token, error) {
	start, line := l.i, l.line

	// Find the closing '$' of the opening delimiter. "$1" and a trailing "$"
	// are not dollar quotes.
	j := l.i + 1
	for j < len(l.src) && isTagByte(l.src[j], j == l.i+1) {
		j++
	}
	if j >= len(l.src) || l.src[j] != '$' {
		return l.lexRun(1), nil
	}

	delim := l.src[l.i : j+1]
	rest := l.src[j+1:]
	idx := bytes.Index(rest, delim)
	if idx < 0 {
		return token{}, &syntaxError{Line: line, Msg: "unterminated dollar-quoted string"}
	}
	body := string(rest[:idx])
	l.advance((j + 1 - l.i) + idx + len(delim))
	return token{kind: kindString, text: body, line: line, pos: start, end: l.i}, nil
}

// lexQuotedIdent reads a "..." identifier, preserving its case exactly. A
// doubled quote inside is one literal quote.
func (l *lexer) lexQuotedIdent() (token, error) {
	start, line := l.i, l.line
	l.advance(1)

	var b strings.Builder
	for {
		if l.i >= len(l.src) {
			return token{}, &syntaxError{Line: line, Msg: "unterminated quoted identifier"}
		}
		c := l.src[l.i]
		if c == '"' {
			if l.peek(1) == '"' {
				b.WriteByte('"')
				l.advance(2)
				continue
			}
			l.advance(1)
			break
		}
		b.WriteByte(c)
		l.advance(1)
	}
	// PostgreSQL rejects "" as well. Accepting it here would produce a nameless
	// table or column and an invalid document much further downstream.
	if b.Len() == 0 {
		return token{}, &syntaxError{Line: line, Msg: "zero-length quoted identifier"}
	}
	return token{kind: kindQuotedIdent, text: b.String(), line: line, pos: start, end: l.i}, nil
}

// lexWord reads an unquoted word and folds it to lower case.
//
// The fold is byte-wise ASCII rather than strings.ToLower so that no locale or
// Unicode case table can ever change which keyword a word is recognised as;
// bytes above 0x7F are left exactly as they are.
func (l *lexer) lexWord() token {
	start, line := l.i, l.line
	for l.i < len(l.src) && isIdentByte(l.src[l.i]) {
		l.advance(1)
	}
	return token{kind: kindWord, text: foldASCII(l.src[start:l.i]), line: line, pos: start, end: l.i}
}

// lexNumber reads a numeric literal verbatim.
//
// "public.users" is NOT a number: the '.' there is not followed by a digit, so
// it never starts one and never extends one. "1.5e3" must stay one token,
// which is why the exponent is consumed here rather than left to the word rule.
func (l *lexer) lexNumber() token {
	start, line := l.i, l.line
	if l.src[l.i] == '.' {
		l.advance(1)
	}
	l.skipDigits()
	if l.i < len(l.src) && l.src[l.i] == '.' && l.src[start] != '.' {
		l.advance(1)
		l.skipDigits()
	}
	if c := l.peek(0); c == 'e' || c == 'E' {
		switch {
		case isDigit(l.peek(1)):
			l.advance(2)
			l.skipDigits()
		case (l.peek(1) == '+' || l.peek(1) == '-') && isDigit(l.peek(2)):
			l.advance(3)
			l.skipDigits()
		}
	}
	return token{kind: kindNumber, text: string(l.src[start:l.i]), line: line, pos: start, end: l.i}
}

// skipDigits consumes a run of decimal digits.
func (l *lexer) skipDigits() {
	for l.i < len(l.src) && isDigit(l.src[l.i]) {
		l.advance(1)
	}
}

// ---------------------------------------------------------------------------
// Byte classes
// ---------------------------------------------------------------------------

// isSpace reports whether c is SQL whitespace. '\r' counts, so a CRLF dump
// numbers its lines exactly like a LF one.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isIdentStart reports whether c may begin an unquoted identifier. Bytes above
// 0x7F are allowed so that a UTF-8 identifier survives lexing; whether it can
// be written to a document at all is decided much later, against the schema's
// identifier pattern.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// isIdentByte reports whether c may continue an unquoted identifier. '$' is
// legal after the first byte, which is why a dollar quote is only recognised
// when the '$' begins a token.
func isIdentByte(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '$' }

// isTagByte reports whether c may appear in a dollar-quote tag. first selects
// the stricter rule for the leading byte.
func isTagByte(c byte, first bool) bool {
	if first {
		return isIdentStart(c)
	}
	return isIdentStart(c) || isDigit(c)
}

// isSinglePunct reports whether c is punctuation that is always one token on
// its own, never part of an operator run.
func isSinglePunct(c byte) bool {
	switch c {
	case '(', ')', ',', ';', '[', ']', '.':
		return true
	}
	return false
}

// isOperatorByte reports whether c is a PostgreSQL operator character. A run of
// them is one token, which is how "::" survives as a single unit.
//
// ':' is included for that reason alone: it is not an operator character in
// PostgreSQL, but the cast operator "::" is the one form of it pg_dump writes,
// and the parser needs to skip it as a whole.
func isOperatorByte(c byte) bool {
	switch c {
	case '+', '-', '*', '/', '<', '>', '=', '~', '!', '@', '#', '%', '^', '&', '|', '`', '?', ':':
		return true
	}
	return false
}

// foldASCII lower-cases ASCII letters and copies every other byte unchanged.
func foldASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
