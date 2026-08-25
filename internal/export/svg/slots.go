package svg

import "slices"

// ---------------------------------------------------------------------------
// Attachment slots: how many, and what they cost the box
// ---------------------------------------------------------------------------

// side is the face of a box a relationship attaches to. There are three, and
// there is deliberately no fourth: nothing in this package ever attaches to a
// box's bottom edge. See sidesOf for the policy and slotDemand for why it has
// to be a policy rather than a question.
type side int

const (
	sideLeft side = iota
	sideRight
	sideTop
)

// demand is how many attachment points one box needs on each of its three
// sides. Counts, not lengths - slotExtent turns one into the other.
type demand struct{ left, right, top int }

// add records one more attachment on side s.
//
// The switch is total over the three sides rather than defaulting, so that an
// invented fourth side would go nowhere visible instead of quietly landing on
// the top edge and widening a box for no reason anyone could find.
func (d *demand) add(s side) {
	switch s {
	case sideLeft:
		d.left++
	case sideRight:
		d.right++
	case sideTop:
		d.top++
	}
}

// sidesOf is the side policy: which face of the child box and which face of
// the parent box relationship i leaves and enters.
//
// It is fixed for all three routing categories, and it is decided from the
// RANKS alone - which exist by now - and never from a coordinate or from a
// router, which do not. slotDemand explains why that matters.
//
//   - An ordinary inter-rank relationship leaves the child's side that faces
//     the parent and enters the parent's side that faces the child. Ranks
//     increase from child to parent and rank 0 is drawn leftmost (D07), so
//     that is the child's RIGHT and the parent's LEFT for every relationship
//     the cycle breaker did not touch. For one it DID reverse, the child is
//     the higher-ranked end and therefore the right-hand box, so the two sides
//     swap. Reading the sides off the ranks rather than writing right/left
//     literally is what keeps a reversed relationship from attaching to the
//     face pointing away from its own route: the first thing that route
//     reaches is its label node, which sits a half-rank towards the parent.
//   - A same-rank relationship leaves the child's TOP and enters the parent's
//     TOP.
//   - A self-reference is the degenerate case of that: both of its ends are
//     the same box, so it takes two slots on that box's top side, which is
//     exactly what the staple in route.go draws.
//
// Top for both special categories, rather than bottom or one of each: a top
// slot spends WIDTH while a left or right slot spends HEIGHT, and the case
// that oversubscribes a side at all is a small lookup table whose LEFT side is
// crowded. Putting the two special categories on the side that spends the
// dimension such a box has to spare keeps them out of that fight. Both ends on
// the SAME side is the second reason for top: the route is then three
// axis-aligned segments - up, across, down - with no direction to choose.
func sidesOf(g *graph, ranks []int, i int) (childSide, parentSide side) {
	if classify(g, ranks, i) != routeInterRank {
		return sideTop, sideTop
	}
	if halfRankStep(ranks, &g.edges[i]) < 0 {
		return sideLeft, sideRight
	}
	return sideRight, sideLeft
}

// slotDemand counts, for every node of one component, how many attachments
// each of its sides has to carry. The result is indexed by node id, with the
// nodes of other components left at zero, which is the convention the ranks
// and the order already follow.
//
// # Why the pass exists
//
// Virtual nodes reserve a corridor per relationship, and a label node reserves
// the band its text sits in, but neither says anything about where a route
// TOUCHES its box. If every edge attached to the same point - the centre of a
// side - two relationships between the same pair of tables would leave that
// point along the same first segment and draw their crow's feet on top of each
// other, and the two would be one thick line at the boundary however
// distinctly the rest of their routes ran. internal/export/svg/testdata's edge
// fixture has exactly that pair, so it is the visible case rather than a
// hypothetical one.
//
// # Why the side policy is a constant and not a query
//
// This pass runs BEFORE coordinate assignment, because a box's size is an
// input to the coordinates and its slot demand is an input to its size. The
// same-rank and self-loop routers run AFTER coordinate assignment, because a
// staple's lane is measured from the boxes' finished positions. So this pass
// cannot ask a router which side it will use: at the moment the question would
// be asked there is no router and no coordinate to answer it with.
//
// Fixing the side for all three categories up front is therefore a
// PRECONDITION of the two-pass split, not a condition on it. The conditional
// phrasing the issue #32 discussion reached for - "if those sides need
// multiple attachments" - is the wrong shape: the demand pass has to know the
// answer for every category before any of them has a route, or it cannot count
// at all.
func slotDemand(g *graph, members []int, ranks []int) []demand {
	demands := make([]demand, len(g.nodes))
	for i := range g.edges {
		e := &g.edges[i]
		if !slices.Contains(members, e.child) {
			continue
		}
		childSide, parentSide := sidesOf(g, ranks, i)
		demands[e.child].add(childSide)
		demands[e.parent].add(parentSide)
	}
	return demands
}

// slotExtent is how much of a side n attachments need: a margin at each end
// and one spacing between each adjacent pair. Zero slots need no room at all,
// which is not the same as one slot needing none.
//
// The spacing is never compressed to make a crowded side fit, and that is the
// decision this function encodes rather than the arithmetic. Compressing is
// the obvious first instinct and it buys nothing: slotSpacing has a hard floor
// at the crow's-foot glyph's own height - coord.go states it as
// slotSpacing > 2*crowHalf - and below that floor the first segments and the
// glyphs begin to collide again, which is precisely the collision the slot
// pass exists to prevent. A spacing below the floor is a slot pass that has
// stopped buying anything. So the demand goes into the box's SIZE instead,
// where there is no floor to hit.
func slotExtent(n int) Coord {
	if n == 0 {
		return 0
	}
	return 2*slotMargin + Coord(n-1)*slotSpacing
}

// finalSize is the size a box is actually laid out at: the larger of what its
// content needs and what its attachments need, taken per axis.
//
// The top side spends width; the left and right sides spend height. Left and
// right are MAXED rather than summed, which is worth a sentence because
// summing looks plausible at a glance: they are opposite faces of one box, and
// each of them needs the box's whole height to spread its own slots along.
// Fifteen slots on the left and fifteen on the right need the height for
// fifteen, not for thirty.
//
// The case that produces surplus is not the wide table but the small one. With
// this package's constants a three-column lookup is 400 + 3*200 = 1000 tenths
// of a pixel tall, and fifteen incoming relationships demand
// slotExtent(15) = 2*80 + 14*140 = 2120, so the box comes out 112 px taller
// than anything drawn in it. That is arithmetic from coord.go's block and not
// a measurement of this code; the same case in comment 5 of issue #32 is
// quoted with different constants as roughly 110 px of content against roughly
// 184 px of demand, measured by nobody on nothing in this tree.
//
// A label node and a virtual node have no attachments at all, so their demand
// is zero and this returns exactly what node.size does - which is why every
// later stage can call it for every node without asking what kind it holds.
func finalSize(n *node, d demand) (w, h Coord) {
	w, h = n.size()
	return max(w, slotExtent(d.top)), max(h, max(slotExtent(d.left), slotExtent(d.right)))
}

// contentOffsetY is how far below a box's top edge its content is drawn. It is
// always zero: the content is TOP-ALIGNED and the surplus finalSize may have
// added is empty space at the BOTTOM of the box.
//
// It is a named function returning a constant rather than an implicit zero at
// the call sites, so that the choice is greppable and so that changing it is
// one line here and one test rather than a hunt through the drawing code.
//
// Top-aligned for three reasons, in order. A row of boxes then reads along a
// common top edge: the headers and the first columns of neighbouring tables
// stay at the same y whatever each box's slot demand is. Centring would have
// to split an odd surplus, which needs a floor-or-ceil tie-break in the sizing
// pass, and top alignment has no rounding in it at all. And distributing the
// surplus into the row heights - the third alternative, and the one that
// sounds tidiest - was rejected because two boxes in one diagram would then
// disagree about how tall a row is, so their text baselines would not line up:
// a second rounding source, and a drawing that reads as a mistake rather than
// as padding.
func contentOffsetY(n *node, d demand) Coord { return 0 }
