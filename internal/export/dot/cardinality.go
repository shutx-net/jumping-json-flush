package dot

import "github.com/shutx-net/jumping-json-flush/internal/export/erd"

// ---------------------------------------------------------------------------
// Crow's foot cardinality
// ---------------------------------------------------------------------------

// arrow names the graphviz arrow type that draws e.
//
// graphviz composes an arrow type from up to four primitives and draws the
// FIRST one closest to the node. That is what puts the cardinality mark - the
// crow's foot or the bar - against the table box and the optionality mark
// inboard of it, which is what crow's foot notation requires. Rendering one
// diagram confirms it: for arrowhead="crowodot" the crow's polygon touches the
// node's boundary and the circle sits further from the node.
//
// Every one of the four is exactly two primitives, so both ends of every edge
// carry the same amount of ink however they were derived, which keeps the
// diagram visually uniform.
//
// The mapping is written as a switch on the two bools rather than as the
// concatenation of two halves so that each of the four arrow type strings
// appears literally, exactly once, and can be found by grep.
//
// All four cases stay although the two derivations that feed it -
// erd.ChildEnd and erd.ParentEnd - reach only three of them: a child end is
// always optional and a parent end is never many, so nothing asks for crowtee.
// This is a total mapping over the type rather than a list of the answers that
// happen to be wanted, and TestEndArrow pins all four so the fourth cannot
// quietly change meaning.
func arrow(e erd.End) string {
	switch {
	case e.Many && e.Optional:
		return "crowodot"
	case e.Many:
		return "crowtee"
	case e.Optional:
		return "teeodot"
	default:
		return "teetee"
	}
}
