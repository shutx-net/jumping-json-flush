// Package dot renders a database design document as Graphviz DOT source
// describing an entity relationship diagram.
//
// jjf writes DOT TEXT and nothing else. It never runs graphviz, never links it
// and never embeds it, so the binary keeps the project's no-CGO, no-runtime
// -dependency property; turning a .dot file into an image is the reader's own
// step, with their own dot:
//
//	jjf export dot db-design.json -o er.dot
//	dot -Tsvg er.dot -o er.svg
//
// The output is deterministic: the same document always produces the same
// bytes, because nothing here reads the clock, no tool version is written into
// the file, and every loop walks a slice rather than a Go map.
//
// The crow's foot cardinality on each relationship is INFERRED from the keys
// and the nullability the CHILD table declares; the JSON never states it, and
// the referenced table is never read. See internal/export/erd for the rules
// and their limits; cardinality.go here holds only the mapping from a derived
// end to a graphviz arrow type.
package dot

import "strings"

// quoteID writes s as a DOT double-quoted string, always emitting the
// surrounding quotes.
//
// There is no exception for a name that would be a legal bare ID. $defs/
// identifier of the schema is ^[A-Za-z_][A-Za-z0-9_]*$, which permits the
// table names graph, node, edge, subgraph, strict and digraph - every one of
// them a DOT keyword. Quoting unconditionally costs one byte per side and
// removes the entire class of collisions.
func quoteID(s string) string { return `"` + escapeString(s) + `"` }

// escapeString escapes s for the inside of a DOT double-quoted string. It is
// quoteID without the quotes, and exists for the one caller that builds a
// longer string around document text: the stub node's label, which appends a
// DOT \n escape that must reach graphviz unaltered and so cannot be produced
// by quoting the whole label at once.
//
// The escaping is defensive rather than reachable: every quoted string this
// package emits is built from $defs/identifier values - the database name,
// table names, foreign key names and column names - which cannot contain a
// quote, a backslash, a newline or anything else needing an escape. It is here
// so that a future caller passing freer text cannot open a hole, and the unit
// tests are the only place it is exercised.
func escapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	// One pass, byte by byte: two successive Replace calls could be ordered
	// wrongly and escape their own output. Bytes of a multibyte UTF-8 sequence
	// are all >= 0x80 and so fall through untouched.
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// escapeHTML escapes document text destined for the inside of a graphviz
// HTML-like label.
//
// The apostrophe is deliberately left alone: a raw ' is valid text content,
// and graphviz's handling of &apos; is not worth relying on. A literal newline
// is left alone too - a logicalName is a JSON string and may legally contain
// one, and inside an HTML-like label a raw newline is whitespace, which is
// harmless and deterministic. It is not a bug waiting for a <BR/>.
//
// Multibyte UTF-8 passes through byte for byte, so Japanese logical names
// survive unchanged; the graph sets charset="UTF-8" so graphviz reads them
// back as such.
func escapeHTML(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	// One pass for the same reason quoteID is: a sequence of Replace calls that
	// handled '&' last would turn < into &amp;lt;.
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
