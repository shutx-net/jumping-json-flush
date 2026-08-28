package check

import (
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// checkColumnDefault reports what one column's default says about itself: that
// it is there but empty (C7), or that it is not an expression at all (C8).
//
// Nothing here evaluates the default, type-checks it against the column's type
// or decides whether it is a sensible value - that is a judgement about the
// design, which AGENTS.md keeps out of jjf. The question asked is narrower and
// answerable from the document alone: could a database system read this text as
// an expression? A default of "now" cannot be one; PostgreSQL rejects it with
// "cannot use column reference in DEFAULT expression", so the finding moves a
// failure the author would have met later forward to where it is cheap.
//
// At most one finding comes out per column, and the order the four C8 cases
// are tried is the order in which one mistake stops the next question from
// meaning anything: past an unbalanced quote the tokens are not the ones the
// author wrote, past a comment introducer the rest of the text is not the
// expression at all, and inside an unbalanced parenthesis a bare word is a
// symptom rather than a second mistake. checkTable already declines to
// evaluate C5 for a column C1 just reported, for the same reason.
func checkColumnDefault(c *model.Column, pos int, table string, f *findingList) {
	// Absent is not a finding: no "default" key means no DEFAULT clause, which
	// is exactly what an author with nothing to default to should write.
	if c.Default == nil {
		return
	}
	label := constraintLabel("column", c.Name, pos, table)
	def := *c.Default

	// C7 - the key is present with nothing in it, which describes a DEFAULT
	// clause with no expression after it. Whitespace counts as empty because
	// "   " produces the same clause and is the same mistake; no document in
	// the repository is whitespace-only, so saying so costs nothing.
	if strings.TrimSpace(def) == "" {
		f.addf(label, `declares an empty default; omit the "default" key when the column has no DEFAULT clause`)
		return
	}

	toks, ok := scanExpr(def)
	if !ok {
		// C8a.
		f.addf(label, "declares the default %q, which has an unbalanced single quote", def)
		return
	}
	if text, found := firstExprEnd(toks); found {
		// C8b. Unlike keywordConstants, which only ever widens what passes,
		// this narrows it - so it earns its place only because all three
		// spellings mean the same thing in every system the schema's dbms enum
		// permits. A comment is nowhere part of an expression, and a statement
		// does not end inside one, so nothing a DEFAULT could legitimately
		// carry is refused here and the check stays a statement about the
		// document rather than about the database behind it.
		//
		// What it saves is the one failure in this file that nobody sees. The
		// generated DDL writes a column on one line, so a comment opened in
		// the default swallows the clauses written after it - and PostgreSQL
		// writes DEFAULT before NOT NULL, so the script succeeds, the column
		// is nullable, and the document and the database disagree with nothing
		// printed anywhere to say so.
		if text == ";" {
			f.addf(label, "declares the default %q, which ends its statement at %q; a DEFAULT is one expression", def, text)
			return
		}
		f.addf(label, "declares the default %q, which starts a comment at %q; the text after it is not part of the expression", def, text)
		return
	}
	if !balancedParens(toks) {
		// C8c.
		f.addf(label, "declares the default %q, which has an unbalanced parenthesis", def)
		return
	}
	if word, found := firstBareWord(toks); found {
		// C8d. Only the FIRST bare word is named, unlike reportMissingColumns,
		// which names every missing column: a constraint's column list is a
		// list of independent names the author fixes one by one, but a default
		// is ONE expression, so "hello world" is a single unquoted string
		// rather than two separate mistakes.
		f.addf(label, "declares the default %q, in which %q is a bare word; a string literal is written '%s'", def, word, word)
	}
}

// keywordConstants are the bare words a DEFAULT may legitimately consist of:
// SQL's niladic constants, the two boolean literals and NULL. Everything else
// written as a bare word is a column reference, which no DEFAULT may contain.
//
// The list is the UNION across the six systems schema/db-design.schema.json
// permits for database.dbms, and it is never selected by doc.Database.DBMS.
// That is deliberate, and it is what keeps the check DBMS-neutral in the sense
// AGENTS.md means: SYSDATE is nothing in PostgreSQL and CURRENT_CATALOG is
// nothing in Oracle, but every entry is a reason to ACCEPT an expression some
// other system would reject, never a reason to reject one a system would
// accept. The check therefore stays a statement about the document rather than
// about the database behind it. The same argument covers treating "::" as a
// cast and "[" as an array constructor below: both are PostgreSQL spellings,
// and both only ever widen what passes.
//
// A slice and a loop, not a map, for the reason usedNames gives: sixteen
// entries are not worth handing any part of this package's behaviour to Go's
// map iteration.
var keywordConstants = []string{
	"CURRENT_TIMESTAMP",
	"CURRENT_DATE",
	"CURRENT_TIME",
	"LOCALTIME",
	"LOCALTIMESTAMP",
	"CURRENT_USER",
	"SESSION_USER",
	"CURRENT_ROLE",
	"CURRENT_CATALOG",
	"CURRENT_SCHEMA",
	"USER",
	"SYSDATE",
	"SYSTIMESTAMP",
	"TRUE",
	"FALSE",
	"NULL",
}

// isKeywordConstant reports whether word is one of them, ignoring ASCII case.
//
// The fold is byte-wise ASCII rather than strings.EqualFold for the reason
// internal/importer/postgres/lex.go folds the same way: no Unicode case table
// may ever decide which word is a SQL keyword. The Kelvin sign folding to "k"
// is a correct answer to a question this package is not asking.
func isKeywordConstant(word string) bool {
	for _, k := range keywordConstants {
		if equalASCIIFold(word, k) {
			return true
		}
	}
	return false
}

// equalASCIIFold reports whether a and b are the same string once ASCII letters
// are compared without case. Every other byte must match exactly.
func equalASCIIFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if upperASCII(a[i]) != upperASCII(b[i]) {
			return false
		}
	}
	return true
}

// upperASCII upper-cases one ASCII letter and returns every other byte as it is.
func upperASCII(c byte) byte {
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	return c
}

// balancedParens reports whether every "(" in toks is closed and none is closed
// before it is opened.
//
// The depth counter is what makes ")(" a finding: counting the two characters
// and comparing the totals would call it balanced, and it is not an expression.
func balancedParens(toks []exprToken) bool {
	depth := 0
	for _, tk := range toks {
		if tk.kind != exprPunct {
			continue
		}
		switch tk.text {
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// firstBareWord returns the first word in toks that stands on its own, and
// whether there was one.
//
// A word is NOT bare, and so not a finding, when its neighbours give it a
// meaning other than "the value of a column":
//
//   - a keyword constant, which is the whole point of DEFAULT CURRENT_TIMESTAMP
//   - preceded by "::" or ".", which makes it a cast target or the tail of a
//     qualified name: "'{}'::jsonb", "'pending'::public.order_status"
//   - followed by "(" - a function call, "now()" - or by "." - the head of a
//     qualified name, "public.myfunc()"
//   - followed by "[", which is the array constructor: "ARRAY[]::text[]". This
//     one is not decoration. internal/importer/postgres emits exactly that
//     expression into documents, so without it jjf would warn about its own
//     output
//   - followed by a string literal, which is SQL's typed-literal form:
//     "INTERVAL '1 day'", "DATE '2020-01-01'", MySQL's "b'0'". Accepting the
//     SHAPE rather than allowlisting the type names is what keeps this from
//     baking one system's type vocabulary into the checker, and it costs no
//     detection power: "now" has no following token at all
//
// Adjacency is between TOKENS, not bytes. An author who writes "'pending' ::
// text" has written correct SQL, and the importer is free to space its casts
// however it likes without turning "text" into a finding.
func firstBareWord(toks []exprToken) (string, bool) {
	for i, tk := range toks {
		if tk.kind != exprWord || isKeywordConstant(tk.text) {
			continue
		}
		if i > 0 && toks[i-1].kind == exprPunct && (toks[i-1].text == "::" || toks[i-1].text == ".") {
			continue
		}
		if i+1 < len(toks) && followsAWord(toks[i+1]) {
			continue
		}
		return tk.text, true
	}
	return "", false
}

// firstExprEnd returns the text of the first token that ends the expression -
// a comment introducer or a statement terminator - and whether there was one.
//
// The first and not all of them, for the reason firstBareWord gives: a default
// is ONE expression, so the text past the first of these is not a series of
// further mistakes but the one mistake's remainder.
func firstExprEnd(toks []exprToken) (string, bool) {
	for _, tk := range toks {
		if tk.kind == exprEnd {
			return tk.text, true
		}
	}
	return "", false
}

// followsAWord reports whether tk, standing immediately after a word, is what
// gives that word a meaning. See firstBareWord for what each case is.
func followsAWord(tk exprToken) bool {
	if tk.kind == exprString {
		return true
	}
	return tk.kind == exprPunct && (tk.text == "(" || tk.text == "[" || tk.text == ".")
}

// ---------------------------------------------------------------------------
// The expression scanner
// ---------------------------------------------------------------------------

// exprKind classifies a token of a default expression. The set is exactly what
// firstBareWord, balancedParens and firstExprEnd have to distinguish and
// nothing more.
type exprKind uint8

// Token kinds produced by scanExpr.
const (
	exprString exprKind = iota
	exprNumber
	exprWord
	exprPunct
	// exprEnd is a comment introducer or a statement terminator: the point at
	// which the text stops being the expression. It is a kind of its own
	// rather than an exprPunct so that the finding can name what it found -
	// "--" and "/*" are read the same way but are not the same typo.
	exprEnd
)

// exprToken is one token, carrying the source slice it came from. Case is
// preserved, because the bare word a finding names must read as the author
// wrote it.
type exprToken struct {
	kind exprKind
	text string
}

// scanExpr splits a default expression into tokens, reporting false when a
// single-quoted string is never closed.
//
// This is NOT internal/importer/postgres/lex.go, and it deliberately does not
// call it. A DBMS-neutral document checker must not depend on a PostgreSQL dump
// importer - the direction of that dependency is the whole argument - and the
// importer's lexer carries statement splitting, dollar quoting, nested block
// comments, psql backslash lines, quoted identifiers, line numbers and byte
// offsets because it reads whole pg_dump files. None of those can change
// whether a single expression of at most 255 characters (the schema's maxLength
// on "default") is well formed. The package made the same call once before,
// with sameColumnSet, and for the same kind of reason.
//
// Known limitations, all accepted: a dollar-quoted literal ($tag$...$tag$) is
// not recognised, because pg_dump never writes one in a DEFAULT and so it
// cannot reach a document through jjf import; a hex literal such as 0x1F reads
// as a number and a word; and SQL Server's "NEXT VALUE FOR seq" is three bare
// words no allowlist could rescue - such a column is normally autoIncrement in
// a jjf document anyway.
//
// The scanner is total. It is reached for documents that never passed JSON
// Schema validation, so an expression of any length, ending mid-escape or
// carrying any byte at all, must return rather than panic or spin.
func scanExpr(s string) ([]exprToken, bool) {
	var toks []exprToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case isExprSpace(c):
			i++

		case c == '\'':
			end, ok := scanQuoted(s, i+1)
			if !ok {
				return nil, false
			}
			toks = append(toks, exprToken{kind: exprString, text: s[i:end]})
			i = end

		// E'...' before the word rule, or the E becomes a bare word of its own.
		// pg_dump writes that form for any string containing a backslash, so
		// getting it wrong would be a false positive on jjf's own output.
		case (c == 'E' || c == 'e') && i+1 < len(s) && s[i+1] == '\'':
			end, ok := scanEscaped(s, i+2)
			if !ok {
				return nil, false
			}
			toks = append(toks, exprToken{kind: exprString, text: s[i:end]})
			i = end

		case isExprDigit(c) || (c == '.' && i+1 < len(s) && isExprDigit(s[i+1])):
			end := scanNumber(s, i)
			toks = append(toks, exprToken{kind: exprNumber, text: s[i:end]})
			i = end

		case isWordStart(c):
			end := scanWord(s, i)
			toks = append(toks, exprToken{kind: exprWord, text: s[i:end]})
			i = end

		case c == ':' && i+1 < len(s) && s[i+1] == ':':
			toks = append(toks, exprToken{kind: exprPunct, text: s[i : i+2]})
			i += 2

		// Found by adjacency rather than by spacing: "1--2" opens a comment
		// exactly as "1 -- 2" does. The scan stops making tokens of what
		// follows, because what follows is not the expression - but it does
		// not stop scanning, because the caller reports the first of these and
		// a total scanner must return either way.
		case c == '-' && i+1 < len(s) && s[i+1] == '-',
			c == '/' && i+1 < len(s) && s[i+1] == '*':
			toks = append(toks, exprToken{kind: exprEnd, text: s[i : i+2]})
			i += 2

		case c == ';':
			toks = append(toks, exprToken{kind: exprEnd, text: s[i : i+1]})
			i++

		default:
			// One byte per token. The check only ever asks about "(", ")",
			// "[", "." and "::", so ",", "+", "|" and everything else needs no
			// name - it only needs to stop being part of a word.
			toks = append(toks, exprToken{kind: exprPunct, text: s[i : i+1]})
			i++
		}
	}
	return toks, true
}

// scanQuoted reads a single-quoted string whose opening quote is at i-1 and
// returns the index just past its closing quote.
//
// A quote doubled inside the string is one quote and does not end it, which is
// how SQL escapes a quote and how the importer spells "it's".
func scanQuoted(s string, i int) (int, bool) {
	for i < len(s) {
		if s[i] != '\'' {
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '\'' {
			i += 2
			continue
		}
		return i + 1, true
	}
	return 0, false
}

// scanEscaped reads the body of an E'...' string, whose opening quote is at
// i-1, and returns the index just past its closing quote.
//
// A backslash consumes the byte after it, so E'a\'b' does not end at the middle
// quote. Doubling still works too, because PostgreSQL accepts both there. A
// trailing backslash walks the index past the end of the string, which ends the
// loop and reports the string unterminated - that is the right answer and, just
// as importantly, indexes nothing.
func scanEscaped(s string, i int) (int, bool) {
	for i < len(s) {
		switch {
		case s[i] == '\\':
			i += 2
		case s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i += 2
		case s[i] == '\'':
			return i + 1, true
		default:
			i++
		}
	}
	return 0, false
}

// scanNumber reads a numeric literal starting at i and returns the index just
// past it.
//
// The exponent is consumed here rather than left to the word rule, so that 1e3
// is one number instead of a number followed by the bare word e3 - which would
// be a finding about a perfectly good default. A '.' only starts or extends a
// number when a digit follows it, which is what keeps public.users out.
func scanNumber(s string, i int) int {
	if end, ok := scanRadix(s, i); ok {
		return end
	}
	leadingDot := s[i] == '.'
	if leadingDot {
		i++
	}
	i = skipDigits(s, i)
	if !leadingDot && i < len(s) && s[i] == '.' {
		i = skipDigits(s, i+1)
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && isExprDigit(s[j]) {
			i = skipDigits(s, j)
		}
	}
	return i
}

// scanRadix reads a hexadecimal, binary or octal literal starting at i and
// reports whether there was one.
//
// mysqldump writes the hexadecimal form for any default it cannot spell in the
// connection charset - an emoji is the everyday way to meet one - so
// "DEFAULT 0xF09F8DA3" is a value the importer really produces. Without this,
// the scan split it into the number 0 and the word xF09F8DA3, and the word was
// reported as a bare word: jjf warning about a document jjf had just written,
// and internal/export/ddl refusing it. Worse, the fix the message suggested -
// quoting the word - would have stored the text instead of the emoji.
//
// PostgreSQL 16.13 reads all three prefixes and MySQL 8.0.46 the first two, so
// like keywordConstants this only ever widens what passes. A prefix with no
// digit after it is not a literal and is left to the ordinary number rule,
// which is what keeps "0x" alone reportable.
func scanRadix(s string, i int) (int, bool) {
	if s[i] != '0' || i+2 >= len(s) {
		return 0, false
	}
	var valid func(byte) bool
	switch s[i+1] {
	case 'x', 'X':
		valid = isHexDigit
	case 'b', 'B':
		valid = isBinaryDigit
	case 'o', 'O':
		valid = isOctalDigit
	default:
		return 0, false
	}
	end := i + 2
	for end < len(s) && valid(s[end]) {
		end++
	}
	if end == i+2 {
		return 0, false
	}
	return end, true
}

// isHexDigit, isBinaryDigit and isOctalDigit report whether c may appear in a
// literal of that radix.
func isHexDigit(c byte) bool {
	return isExprDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isBinaryDigit(c byte) bool { return c == '0' || c == '1' }

func isOctalDigit(c byte) bool { return c >= '0' && c <= '7' }

// skipDigits returns the index of the first byte at or after i that is not a
// digit.
func skipDigits(s string, i int) int {
	for i < len(s) && isExprDigit(s[i]) {
		i++
	}
	return i
}

// scanWord reads an identifier or keyword starting at i and returns the index
// just past it.
func scanWord(s string, i int) int {
	for i < len(s) && isWordPart(s[i]) {
		i++
	}
	return i
}

// isExprSpace reports whether c separates tokens.
func isExprSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// isExprDigit reports whether c is an ASCII decimal digit.
func isExprDigit(c byte) bool { return c >= '0' && c <= '9' }

// isWordStart reports whether c may begin a word. Bytes at or above 0x80 count,
// so that an identifier written in Japanese is one word rather than a run of
// punctuation - it would otherwise be reported as a bare word named by a single
// broken UTF-8 byte, which no reader could act on.
func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// isWordPart reports whether c may continue a word. '$' is included because
// PostgreSQL allows it inside identifiers.
func isWordPart(c byte) bool { return isWordStart(c) || isExprDigit(c) || c == '$' }
