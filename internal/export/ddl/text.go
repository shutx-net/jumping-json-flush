package ddl

import "strings"

// ---------------------------------------------------------------------------
// Text handling shared by every dialect
// ---------------------------------------------------------------------------

// What is here is what does not depend on the target: the one-pass doubling
// every delimiter escape is built from, and the column list that appears
// inside a parenthesis in every dialect there is.
//
// Each dialect's own quoting lives in its own file, prefixed with that
// dialect's name, and not here. An unprefixed quoteIdent in this package would
// read as a claim that there is one way to quote an identifier; there is not,
// and the whole point of the dialect seam is that the difference has a name.
// The rule the file split follows is stated in postgres.go's header.

// quotedList renders a column list for the inside of a parenthesis.
//
// It takes the quoting function rather than choosing one, which is the shape
// doubled already has: a shared one-pass helper parameterised by the single
// thing that differs between callers. The alternative - one quotedList per
// dialect - would be the same loop written twice for the sake of one call
// inside it.
func quotedList(quote func(string) string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quote(c)
	}
	return strings.Join(quoted, ", ")
}

// constraintPrefix names a constraint, or returns nothing when the document
// leaves it unnamed and the database is left to invent one. The schema permits
// both for a primary key and for a unique key.
//
// It is shared and parameterised by the quoting function, the shape doubled and
// quotedList already have, because both dialects spell a named constraint
// CONSTRAINT <quoted> and one call inside a two-line function is not worth
// writing that function twice.
//
// MySQL accepts CONSTRAINT <name> PRIMARY KEY (...) and then calls the
// resulting key PRIMARY, because that is what it calls every primary key.
// Emitting the name anyway is deliberate - the document said it - and the fact
// that it does not survive a round trip is documented drift rather than a
// reason to drop it here.
func constraintPrefix(quote func(string) string, name string) string {
	if name == "" {
		return ""
	}
	return "CONSTRAINT " + quote(name) + " "
}

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
