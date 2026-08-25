package svg

import "github.com/shutx-net/jumping-json-flush/internal/model"

// ---------------------------------------------------------------------------
// The pipeline
// ---------------------------------------------------------------------------

// layout turns a document into the finished Geometry. It is the map of this
// package: every stage below has its own file and its own reasoning, and the
// order they run in is the argument this file makes.
//
//	document
//	  -> graph construction, text measurement, self-loop extraction, components
//	  -> per component:
//	       cycle breaking -> layering -> rank doubling -> label and long-edge
//	       virtual nodes -> ordering -> slot demand and final sizes ->
//	       coordinate assignment -> slot assignment -> orthogonal routing
//	  -> rigid component packing
//	  -> the translation to the margin, and the bounds
//	  -> Geometry
//
// It is a pure function of the document: no I/O, no clock, no map ranged over
// where the order could reach the answer, so two calls on one document produce
// the same value (TestLayoutIsDeterministic, which the goldens depend on from
// phase 12 onwards).
//
// Two placements in the list above are load-bearing and would look arbitrary
// otherwise. Text measurement is inside graph construction, ONCE per node, and
// outside the per-component loop: a table's size does not depend on which
// component it is in, since it is in exactly one, so measuring per component
// would be the same work in a place that knows more than it needs to. And
// self-loop extraction sits beside graph construction because both ends of the
// pipeline need it done first - layering assumes a graph with no self-loops, a
// loop having no direction to rank, and the routing taxonomy names them as one
// of its three categories.
func layout(doc *model.Document) Geometry {
	g := buildGraph(doc)
	blocks := layOutComponents(g)
	packComponents(g, blocks)
	return newGeometry(g, blocks, moveToMargin(blocks))
}

// layOutComponents runs the per-component pipeline over every component and
// returns one block each, in g.components order - which is the document order of
// each component's first table, so the blocks are indexed the same way
// packOrder's answers are.
//
// The order of the calls inside the loop is not a style: each one consumes what
// the one before it produced. Cycle breaking first, because layering needs the
// relationships acyclic. Ranks before the virtual nodes, because a chain is one
// node per half-rank crossed and there are no half-ranks until the ranks are
// doubled. Ordering before sizing, because slot demand is counted per side and
// a side's demand is what can make a box bigger than its content. Sizing before
// coordinates, because the separation constraints are stated in widths and
// heights. Coordinates before slot assignment and routing, because a slot's
// place on a side and a staple's lane are both measured from finished
// rectangles. Moving any one of them earlier leaves a later pass reading a
// value that no longer describes the drawing, and the failure is a plausible
// picture rather than a crash.
func layOutComponents(g *graph) []block {
	blocks := make([]block, 0, len(g.components))
	for c := range g.components {
		members := g.components[c]

		breakCycles(g, members)
		ranks := assignRanks(g, members)
		members, ranks, chains := insertVirtualNodes(g, members, ranks)
		o := orderComponent(g, members, ranks, chains)
		demands := slotDemand(g, members, ranks)
		rects := positionComponent(g, o, demands)
		routes := routeComponent(g, o, ranks, chains, rects)

		blocks = append(blocks, block{
			members: members,
			chains:  chains,
			rects:   rects,
			routes:  routes,
		})
	}
	return blocks
}

// packComponents moves every block bodily onto its shelf.
//
// What a component is measured from is its BOUNDING BOX rather than its
// rectangles, because its coordinates start at its own origin and its drawn
// extent does not - see componentBounds for the case that makes the difference.
// It is a step of layout rather than three lines inside it because the packing
// assertions have to see the blocks on both sides of it, which is also the only
// reason layout is not one function.
func packComponents(g *graph, blocks []block) {
	boxes := make([]Rect, len(blocks))
	for i := range blocks {
		boxes[i] = componentBounds(&blocks[i])
	}
	for i, delta := range pack(boxes, packOrder(g)) {
		translate(&blocks[i], delta)
	}
}

// moveToMargin shifts the packed drawing so that its top-left corner sits one
// margin in from the origin, and returns the page that leaves.
//
// Every coordinate in the finished Geometry is therefore non-negative, which is
// what lets the viewBox start at 0 0 - and the margin is measured from the
// DRAWN extent rather than from the boxes, so a staple's lane above the top
// rank gets its margin too. The bounds come out tight by construction: the
// least drawn x and the least drawn y are both exactly margin, which is what
// TestBoundsStartAtOrigin asserts.
func moveToMargin(blocks []block) Rect {
	drawn := bounds(blocks)
	for i := range blocks {
		translate(&blocks[i], Point{X: margin - drawn.X, Y: margin - drawn.Y})
	}
	return Rect{W: drawn.W + 2*margin, H: drawn.H + 2*margin}
}
