package ddl

import "strings"

// quoteIdent writes s as a SQL delimited identifier, always emitting the
// surrounding double quotes.
//
// There is no exception for a name that would be legal bare. The schema's
// $defs/identifier is ^[A-Za-z_][A-Za-z0-9_]*$, which permits order, user,
// table and every other reserved word, so quoting unconditionally removes the
// whole class of collisions without a keyword list to maintain. It also
// preserves case: an unquoted Orders would reach PostgreSQL as orders and the
// database would stop matching the document.
//
// Doubling an embedded quote is defensive rather than reachable: that pattern
// forbids one. It is here so that a future caller passing freer text cannot
// open a hole, and the unit tests are the only place it is exercised.
func quoteIdent(s string) string { return `"` + doubled(s, '"') + `"` }

// quoteLiteral writes s as a SQL string literal.
//
// Unlike quoteIdent's, this escaping IS reachable: it carries logicalName and
// description, which are free text in any language and may hold an apostrophe.
//
// A backslash is left alone on purpose. That is correct while
// standard_conforming_strings is on, which is PostgreSQL's default from 9.1
// onwards and therefore throughout the range jjf supports; the generated script
// does not SET it, because one setting invites search_path and client_encoding
// after it and the specification's list of what is not emitted did not
// contemplate any of them.
//
// A newline is left alone too, and has to be: a table comment is the logical
// name and the description joined by one, and the importer cuts it back apart
// at exactly that byte. An E'...\n...' escape string would look tidier and
// would break the round trip, because the importer's lexer keeps such a token
// raw.
func quoteLiteral(s string) string { return `'` + doubled(s, '\'') + `'` }

// doubled returns s with every occurrence of c doubled.
//
// One pass, byte by byte: chained replacements can escape their own output,
// and a sequence that handled the delimiter last would double what it had just
// doubled. Bytes of a multibyte UTF-8 sequence are all >= 0x80 and fall through
// untouched, so Japanese text survives unchanged.
func doubled(s string, c byte) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			b.WriteByte(c)
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
