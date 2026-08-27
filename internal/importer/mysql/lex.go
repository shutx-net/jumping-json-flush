// Package mysql converts the output of "mysqldump --no-data" into a jjf
// database design document.
//
// It targets the output SHAPE of mysqldump rather than the MySQL grammar: the
// statements mysqldump emits, in the order it emits them, are what this
// package understands. Arbitrary hand written SQL is best effort at most.
//
// Nothing outside the standard library and internal/{model,exitcode} is
// imported, so the importer keeps the project's no-CGO, single-binary
// property.
//
// The package is layered the way internal/importer/postgres is. lex.go turns
// bytes into tokens, parse.go and stmt.go turn tokens into a MySQL shaped
// intermediate representation, and build.go resolves that representation into
// a model.Document.
//
// Only half of the sibling's reason for that middle layer survives here, and
// ir.go says which half from the other side. Correlation does NOT arise:
// MySQL writes AUTO_INCREMENT on the column itself, so there is no CREATE
// SEQUENCE to tie a default back to and no ALTER SEQUENCE ... OWNED BY to
// decide which column owns it. Forward reference does arise: mysqldump orders
// tables by name and writes foreign keys inline, so a foreign key routinely
// names a table defined further down the file. That alone is why parsing does
// not build the document directly.
//
// This package shares no code with internal/importer/postgres, and that is
// deliberate. The two lexers agree on about half their bytes and disagree on
// every one that matters: what quotes an identifier, whether a block comment
// nests, whether a backslash escapes, whether '#' starts a comment, whether
// '$' can quote a string, whether "--" needs a space after it, and what ends a
// statement. A shared lexer would be a lexer with a dialect flag threaded
// through every predicate, which is the shape that makes both dialects hard to
// read and one of them quietly wrong. The tree has made this call before and
// written the argument down each time - see internal/export/ddl/precheck.go's
// tableLabel and internal/importer/postgres/typemap.go's foldASCII.
//
// Every byte class and every skip rule below therefore says how it differs
// from its PostgreSQL sibling, or that it is identical and why. A predicate
// with no such note is a predicate a reader has to guess about.
package mysql

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
	// kindTerminator means "this ends a statement". It has no counterpart in
	// the PostgreSQL sibling, where a statement always ends at a ';' and the
	// splitter can say so itself. See the lexer's delim field for why MySQL
	// needs a token kind for it.
	kindTerminator
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
	case kindTerminator:
		return "terminator"
	}
	return "unknown"
}

// token is one lexical element of a dump.
//
// text holds the DECODED value: a backtick identifier without its backticks
// and with doubled backticks collapsed, a string without its delimiters and
// with escapes resolved, an unquoted word folded to lower case, the verbatim
// source slice for numbers and punctuation, and the active delimiter for a
// terminator.
//
// pos and end are byte offsets into the source and cover the token INCLUDING
// its delimiters. They exist so that an expression such as a DEFAULT clause can
// be reproduced verbatim by slicing src[first.pos:last.end], which is the only
// way to honour the schema's promise that a default is "written verbatim".
// Unwrapping an executable comment is implemented so as not to disturb them,
// which is the practical reason the wrapper is consumed here rather than by a
// preprocessing pass over the bytes.
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

// defaultDelimiter is what ends a statement until a DELIMITER line says
// otherwise, which is the same default the mysql client starts with.
const defaultDelimiter = ";"

// delimiterDirective is the mysql client command that changes it. It is not
// SQL, and no server ever sees it; mysqldump writes it because a trigger body
// is full of semicolons and the client would otherwise cut the body in half.
const delimiterDirective = "delimiter"

// lexer is the function-local state of one lex call. It is a struct rather than
// a set of free functions so that the source, the read offset and the line
// counter cannot drift apart; there is no package-level state anywhere.
type lexer struct {
	src  []byte
	i    int
	line int
	// atLineStart reports whether the next byte begins a line. A DELIMITER
	// directive is only recognised there, exactly where mysqldump writes it,
	// and for the reason the PostgreSQL sibling only recognises a psql
	// backslash command there: a client directive that could be recognised
	// mid-statement would be a keyword, and MySQL has no such keyword.
	atLineStart bool
	// execDepth counts the open /*!NNNNN ... */ executable comments. Their
	// contents are lexed as ordinary SQL, so the count is what tells a '*/'
	// apart from a stray operator run; see skipIgnorable.
	execDepth int
	// execLine is the line the OUTERMOST open executable comment began on, so
	// that an unterminated one is reported where it was opened rather than at
	// end of input.
	execLine int
	// delim is the active statement delimiter. Everything above the lexer -
	// splitStatements especially - is written as though a statement always
	// ended at one token, which is what a DELIMITER line would otherwise take
	// away.
	delim string
}

// lex tokenizes a dump.
//
// No regular expression is used here, or anywhere else in this package. A
// line-oriented regexp cannot see that a ';' sits inside a trigger body under a
// custom delimiter, inside a string with doubled quotes or backslash escapes,
// or inside a block comment, and every one of those appears in real mysqldump
// output.
//
// The returned slice always ends with a kindEOF token, so the parser never has
// to bounds-check its lookahead.
func lex(src []byte) ([]token, error) {
	l := &lexer{src: src, line: 1, atLineStart: true, delim: defaultDelimiter}
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
	// A wrapper left open would have swallowed its closing "*/" silently and
	// accepted a truncated file, so it is a failure and not a warning.
	if l.execDepth > 0 {
		return nil, &syntaxError{Line: l.execLine, Msg: "unterminated executable comment"}
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

// skipIgnorable consumes every run of whitespace, comments, executable comment
// wrappers and DELIMITER directives at the read offset. It loops because all
// four can follow one another.
func (l *lexer) skipIgnorable() error {
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case isSpace(c):
			l.advance(1)
		case c == '#':
			// MySQL's own line comment, which PostgreSQL does not have. It is
			// checked before the operator rule so '#' never becomes a token,
			// and it is why isOperatorByte leaves '#' out.
			l.skipToEndOfLine()
		case c == '-' && l.peek(1) == '-' && endsDoubleDash(l.peek(2)):
			// MySQL requires whitespace after "--"; PostgreSQL does not. The
			// difference is reachable - "a--b" subtracts a negative in MySQL
			// and is a comment in PostgreSQL - and getting it wrong in the
			// tolerant direction silently discards the rest of a line that may
			// hold a DEFAULT expression.
			l.skipToEndOfLine()
		case c == '/' && l.peek(1) == '*':
			if l.peek(2) == '!' {
				// Not a comment at all: everything mysqldump hides in one of
				// these is real DDL. See openExecutableComment.
				l.openExecutableComment()
				continue
			}
			if err := l.skipBlockComment(); err != nil {
				return err
			}
		case c == '*' && l.peek(1) == '/':
			if l.execDepth == 0 {
				// Reachable only from a hand-edited file. Saying so beats
				// lexing it as the operator run "*/", which would put a token
				// nothing above the lexer can explain into the stream.
				return &syntaxError{Line: l.line, Msg: `unexpected "*/" outside a comment`}
			}
			l.execDepth--
			l.advance(2)
		case l.atLineStart && l.atDelimiterDirective():
			if err := l.readDelimiterDirective(); err != nil {
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
// MySQL block comments do NOT nest: the comment ends at the first "*/". This is
// the one place where copying internal/importer/postgres/lex.go's
// skipBlockComment would be actively wrong rather than merely redundant - a
// nesting scanner reads past the end of a comment that contains a "/*" and
// swallows the statements after it. The two files otherwise read alike, so the
// difference is stated here rather than left to be noticed.
func (l *lexer) skipBlockComment() error {
	openLine := l.line
	l.advance(2)
	for {
		if l.i >= len(l.src) {
			return &syntaxError{Line: openLine, Msg: "unterminated block comment"}
		}
		if l.src[l.i] == '*' && l.peek(1) == '/' {
			l.advance(2)
			return nil
		}
		l.advance(1)
	}
}

// openExecutableComment consumes the "/*!" introducer and the up to five
// version digits after it, and records that a wrapper is open.
//
// The version number is read and DISCARDED. jjf does not emulate MySQL's
// version gating: it has no server version to gate against, and a construct
// written for 5.1 is still a construct in the file. The contents then lex as
// ordinary SQL, which is the whole point - "/*!50100 PARTITION BY RANGE (...)
// */" is the shape of a table, and treating the wrapper as a comment would make
// partitioning invisible and let the importer describe a table that does not
// exist without saying a word.
//
// Blanking the wrappers in a preprocessing pass was the alternative. Finding
// the matching "*/" needs to know where strings and comments are, which needs
// the lexer, so the preprocessor would have been a second lexer.
//
// MariaDB's "/*M!NNNNN ... */" is deliberately NOT recognised. MariaDB is a
// separate dbms value with no importer, and recognising its wrapper would be a
// claim about a dialect jjf does not support.
func (l *lexer) openExecutableComment() {
	if l.execDepth == 0 {
		l.execLine = l.line
	}
	l.execDepth++
	l.advance(3) // "/*!"
	for n := 0; n < 5 && isDigit(l.peek(0)); n++ {
		l.advance(1)
	}
}

// atDelimiterDirective reports whether the read offset is on the word
// DELIMITER used as a client directive rather than as an identifier.
func (l *lexer) atDelimiterDirective() bool {
	if l.i+len(delimiterDirective) > len(l.src) {
		return false
	}
	if foldASCII(l.src[l.i:l.i+len(delimiterDirective)]) != delimiterDirective {
		return false
	}
	// "delimiters" is a column name, not a directive.
	return !isIdentByte(l.peek(len(delimiterDirective)))
}

// readDelimiterDirective consumes a whole DELIMITER line and installs the new
// delimiter.
//
// The rest of the line after the first whitespace run is taken verbatim, which
// is what the mysql client does and what makes "DELIMITER ;;" mean the two
// characters and not one repeated. A line that names nothing is a failure
// rather than a silent no-op: the statements after it would all run together
// into one, and the parse error that followed would name a line hundreds
// further down.
func (l *lexer) readDelimiterDirective() error {
	line := l.line
	l.advance(len(delimiterDirective))
	for l.i < len(l.src) && (l.src[l.i] == ' ' || l.src[l.i] == '\t') {
		l.advance(1)
	}
	start := l.i
	for l.i < len(l.src) && !isSpace(l.src[l.i]) {
		l.advance(1)
	}
	if l.i == start {
		return &syntaxError{Line: line, Msg: "DELIMITER names no delimiter"}
	}
	l.delim = string(l.src[start:l.i])
	l.skipToEndOfLine()
	return nil
}

// atActiveDelimiter reports whether the read offset is on the delimiter that is
// currently in force.
func (l *lexer) atActiveDelimiter() bool {
	return l.delim != "" && bytes.HasPrefix(l.src[l.i:], []byte(l.delim))
}

// next reads the one token at the read offset, which is known to be neither
// whitespace nor a comment.
//
// The delimiter is tested first, before every other rule. It has to be: with
// the default delimiter it would otherwise be caught by isSinglePunct, and with
// ";;" in force the first of the two semicolons would become a token of its
// own and the trigger body would split in the middle.
func (l *lexer) next() (token, error) {
	if l.atActiveDelimiter() {
		return l.lexTerminator(), nil
	}
	c := l.src[l.i]
	switch {
	case c == '\'':
		return l.lexString('\'')
	case c == '"':
		// A double quoted run is a STRING in MySQL, not an identifier, because
		// ANSI_QUOTES is off by default. This is the exact inverse of the
		// PostgreSQL sibling, where '"' is the identifier quote and '`' is an
		// operator character.
		return l.lexString('"')
	case c == '`':
		return l.lexQuotedIdent()
	case isIdentStart(c):
		return l.lexWord(), nil
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		return l.lexNumber(), nil
	case isSinglePunct(c):
		return l.lexRun(1), nil
	case isOperatorByte(c):
		// A maximal run, which is what makes "->>" and ":=" single tokens.
		n := 0
		for l.i+n < len(l.src) && isOperatorByte(l.src[l.i+n]) {
			// An executable comment may close directly after an operator
			// character - mysqldump writes "/*!50003 CREATE*/" - so a run
			// inside one stops at "*/" and leaves the wrapper to
			// skipIgnorable, which is the only place that knows the depth.
			if l.execDepth > 0 && n > 0 && l.src[l.i+n] == '*' && l.peek(n+1) == '/' {
				break
			}
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

// lexTerminator emits the active delimiter as the one token that ends a
// statement.
func (l *lexer) lexTerminator() token {
	start, line := l.i, l.line
	l.advance(len(l.delim))
	return token{kind: kindTerminator, text: l.delim, line: line, pos: start, end: l.i}
}

// lexString reads a quoted string. quote is the delimiter - a single or a
// double quote - and one implementation serves both because MySQL's rules for
// them are the same.
//
// A doubled quote of the SAME character is one literal character; a doubled
// quote of the other one is two ordinary characters and not an escape. A
// backslash escapes the byte after it - ALWAYS.
//
// sql_mode is never read and the behaviour never changes part way through a
// file. NO_BACKSLASH_ESCAPES is off by default, and the
// "/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */"
// that opens every mysqldump sets a mode that does not include it. Tracking it
// would make the meaning of a byte on line 900 depend on a SET on line 12, for
// a setting jjf can neither verify nor influence. The blast radius is bounded
// and worth stating: string VALUES are read only for COMMENT text and for table
// options, so an imperfect decode can garble a comment and can never corrupt
// structure - which is the argument
// internal/importer/postgres/lex.go's lexEscapeString makes word for word.
func (l *lexer) lexString(quote byte) (token, error) {
	start, line := l.i, l.line
	l.advance(1)

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
			// \% and \_ are the two escapes MySQL leaves the backslash on,
			// because they mean the literal two characters to LIKE.
			if n := l.peek(1); n == '%' || n == '_' {
				b.WriteByte('\\')
				b.WriteByte(n)
			} else {
				b.WriteByte(unescape(n))
			}
			l.advance(2)
		case c == quote && l.peek(1) == quote:
			b.WriteByte(quote)
			l.advance(2)
		case c == quote:
			l.advance(1)
			return token{kind: kindString, text: b.String(), line: line, pos: start, end: l.i}, nil
		default:
			b.WriteByte(c)
			l.advance(1)
		}
	}
}

// unescape resolves one backslash escape. Anything unknown yields the escaped
// byte itself, which is what MySQL does too.
//
// \0 and \Z are MySQL's own; \f is PostgreSQL's and MySQL has none, so it is
// absent here and a "\f" is a literal 'f' exactly as the server would read it.
// C's table is not a guide: \v and \a are the letters v and a to both servers.
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
	case '0':
		return 0
	case 'Z':
		return 26
	}
	return c
}

// lexQuotedIdent reads a `...` identifier, preserving its case exactly. A
// doubled backtick inside is one literal backtick.
func (l *lexer) lexQuotedIdent() (token, error) {
	start, line := l.i, l.line
	l.advance(1)

	var b strings.Builder
	for {
		if l.i >= len(l.src) {
			return token{}, &syntaxError{Line: line, Msg: "unterminated quoted identifier"}
		}
		c := l.src[l.i]
		if c == '`' {
			if l.peek(1) == '`' {
				b.WriteByte('`')
				l.advance(2)
				continue
			}
			l.advance(1)
			break
		}
		b.WriteByte(c)
		l.advance(1)
	}
	// MySQL rejects an empty pair as well. Accepting it here would produce a
	// nameless table or column and an invalid document much further downstream.
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
// "db.users" is NOT a number: the '.' there is not followed by a digit, so it
// never starts one and never extends one. "1.5e3" must stay one token, which is
// why the exponent is consumed here rather than left to the word rule, and
// 0x1F must stay one token for the same reason.
//
// MySQL's other two literal spellings, b'0101' and x'1f', are deliberately left
// as a word followed by a string. They reach a document only through a DEFAULT,
// and a default is sliced out of the source verbatim rather than rebuilt from
// tokens, so splitting them costs nothing.
func (l *lexer) lexNumber() token {
	start, line := l.i, l.line
	if l.src[l.i] == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') && isHexDigit(l.peek(2)) {
		l.advance(2)
		for isHexDigit(l.peek(0)) {
			l.advance(1)
		}
		return token{kind: kindNumber, text: string(l.src[start:l.i]), line: line, pos: start, end: l.i}
	}
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
// numbers its lines exactly like a LF one. Identical to the PostgreSQL
// sibling's rule, because whitespace is the one thing the two agree on.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isHexDigit reports whether c may appear in an 0x literal.
func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// endsDoubleDash reports whether c is what MySQL requires after "--" for the
// two characters to begin a comment: whitespace, a control character, or end of
// input, which peek reports as 0.
//
// Written as one comparison because every byte at or below a space is either
// whitespace or a control character, and there is no third thing down there.
func endsDoubleDash(c byte) bool { return c <= ' ' || c == 0x7f }

// isIdentStart reports whether c may begin an unquoted identifier. Bytes above
// 0x7F are allowed so that a UTF-8 identifier survives lexing; whether it can
// be written to a document at all is decided much later, against the schema's
// identifier pattern.
//
// '$' is here, where the PostgreSQL sibling excludes it. It has to be excluded
// there because a leading '$' introduces a dollar-quoted string; MySQL has no
// dollar quoting at all, so the byte is free to be what MySQL says it is - a
// legal identifier character in any position.
func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// isIdentByte reports whether c may continue an unquoted identifier.
func isIdentByte(c byte) bool { return isIdentStart(c) || isDigit(c) }

// isSinglePunct reports whether c is punctuation that is always one token on
// its own, never part of an operator run.
//
// '[' and ']' are absent, where the PostgreSQL sibling has them: MySQL has no
// array types, so nothing in a dump writes a square bracket and admitting one
// would only invent a token no caller handles.
func isSinglePunct(c byte) bool {
	switch c {
	case '(', ')', ',', ';', '.':
		return true
	}
	return false
}

// isOperatorByte reports whether c is a MySQL operator character. A run of them
// is one token, which is how ":=" and "->>" survive as single units.
//
// Two bytes differ from the PostgreSQL sibling's list, and both differences are
// load bearing. '`' is absent because it quotes an identifier here rather than
// being the operator character it is in PostgreSQL. '#' is absent because it
// opens a comment here, and skipIgnorable consumes it before this is reached.
// '@' stays, for the user variables the mysqldump preamble is made of.
func isOperatorByte(c byte) bool {
	switch c {
	case '+', '-', '*', '/', '<', '>', '=', '~', '!', '%', '^', '&', '|', '?', ':', '@':
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
