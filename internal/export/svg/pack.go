package svg

import "slices"

// ---------------------------------------------------------------------------
// Packing the components
// ---------------------------------------------------------------------------

// packOrder is the order the components are placed in: descending node count
// first, then the document-order position of the component's first table.
//
// # Why this pass carries the worst case
//
// It does more of the work than anything upstream on a real schema. For pagila,
// 50 of 71 tables have no foreign key at all, so 50 of the components are a
// single box and this pass is what places most of that diagram; layering,
// ordering and routing are arranging the other 21. That number was measured by
// the author of issue #32 on that schema, not on this code. Without this pass
// the whole drawing is one column of 59 boxes.
//
// # The rule, and the three orders that were rejected
//
// Descending node count first because whatever goes down first is what the
// reader's eye lands on, and the structure is worth more of that than the
// singletons are; it is also the first-fit-decreasing argument, which is why a
// shelf packer wastes least space in this order.
//
// Document order as the tie-break, and it has to be a TOTAL tie-break rather
// than a stable sort's incidental one: at 50 single-node components a tie is
// the normal case, not the corner, and document order is the only order the
// document itself supplies - the same principle every other tie in this package
// is broken by. The comparator below is total on the second key alone, so
// SortStableFunc's stability is belt and braces and not the mechanism.
//
// Rejected: pure document order, which interleaves the singletons with the
// structure and so makes every shelf as tall as the tallest component that
// landed on it. Rejected: sorting by area, because area depends on the measured
// text - correcting one logicalName would reorder the whole diagram, and a
// diagram that rearranges itself when a word changes is one nobody can review.
// Rejected: a target width of the square root of the total area, which needs
// either a floating-point sqrt (D06 forbids one) or an integer sqrt helper, and
// which introduces an aspect-ratio constant - a layout knob wearing a different
// hat.
func packOrder(g *graph) []int {
	order := make([]int, len(g.components))
	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		if n := len(g.components[b]) - len(g.components[a]); n != 0 {
			return n
		}
		// The members of a component are sorted, so the first is its lowest
		// node id, which is the document-order position of its first table.
		return g.components[a][0] - g.components[b][0]
	})
	return order
}

// componentBounds is the smallest rectangle covering everything block b draws.
//
// Node rectangles are not enough, and that is the whole reason this is a
// function rather than a fold over rects. A staple runs in a lane ABOVE its
// half-rank and carries its label above the lane, so a self-reference on a box
// in the top rank puts both a route point and a label rectangle outside every
// node rectangle in the component. Bounds taken from the rectangles alone would
// clip exactly those documents, and "all drawn geometry lies inside the final
// bounds" would then fail on the drawing rather than on the check.
//
// The accumulation starts at the first member's rectangle rather than at a zero
// Rect, which is the trap Rect.union's own comment names: union takes both
// arguments as closed point sets, so starting from zero would drag the origin
// into every answer. A component always has a first member.
func componentBounds(b *block) Rect {
	r := b.rects[b.members[0]]
	for _, v := range b.members {
		r = r.union(b.rects[v])
	}
	for k := range b.routes {
		for _, p := range b.routes[k].points {
			r = r.union(at(p))
		}
		if b.routes[k].hasOwnLabel() {
			r = r.union(b.routes[k].labelRect)
		}
	}
	return r
}

// pack shelves the components left to right and returns the translation that
// moves each one onto its shelf, indexed the same way boxes is. order is the
// sequence they are placed in; boxes[i] is component i's bounding box, which is
// where its own coordinates happen to be and therefore what the translation is
// measured from.
//
// # The target width
//
// It is the width of the WIDEST single component, and the reason that is worth
// having is that it needs no constant: no component is ever split, scaled or
// reflowed, and the diagram comes out exactly as wide as the thing that forced
// it to be that wide.
//
// The honest limits of that choice, both ends of the same trade. One
// enormously wide component makes every shelf that wide and spreads the
// singletons thin across it (K06). And at the other end, when the components
// are all about the same width - two lookup tables with no relationships
// between them, say - no two of them can share a shelf at all, because
// w + componentGap + w exceeds max(w, w) for any positive gap, so the drawing
// comes out as one column. Neither is fixed here: the fix in both directions is
// a target aspect ratio, which is a layout knob, and there is no measurement in
// this tree that would justify choosing a value for one. If either turns out to
// matter, the change is a follow-up with a measurement attached.
func pack(boxes []Rect, order []int) []Point {
	var targetWidth Coord
	for _, b := range boxes {
		targetWidth = max(targetWidth, b.W)
	}

	deltas := make([]Point, len(boxes))
	var shelfX, shelfY, shelfHeight Coord
	for _, i := range order {
		// The shelfX > 0 guard is what makes the widest component fit: the
		// first component on a shelf goes on it whatever its width, so a
		// component exactly as wide as the target is never bounced onto an
		// empty shelf below.
		if shelfX > 0 && shelfX+boxes[i].W > targetWidth {
			shelfY += shelfHeight + componentGap
			shelfX, shelfHeight = 0, 0
		}

		deltas[i] = Point{X: shelfX - boxes[i].X, Y: shelfY - boxes[i].Y}
		shelfX += boxes[i].W + componentGap
		shelfHeight = max(shelfHeight, boxes[i].H)
	}
	return deltas
}
