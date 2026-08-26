package svg

import "slices"

// ---------------------------------------------------------------------------
// Orthogonal routing
// ---------------------------------------------------------------------------

// route is one relationship as a drawn line.
//
// points is the polyline, from the child's attachment to the parent's. Every
// consecutive pair of points is an axis-aligned segment and no route ever
// contains a diagonal: that is the strongest line in the frozen invariant set -
// "every segment of every routed edge is axis-aligned", with no exception for
// self-references - and the reason a bend is always spelled out as two segments
// through a corner rather than as one step in both directions.
//
// childAt and parentAt are the same points as the two ends of points, kept
// separately because the drawing needs to know which end is which: the crow's
// foot goes on the child end, whichever direction the route happens to run.
//
// labelRect is where the relationship's name is drawn - but only for the two
// special categories. An ordinary inter-rank relationship has a label NODE,
// which reserved its own band back in rank doubling, and placing that label
// again here is how the two placements would come to disagree.
type route struct {
	edge   int
	points []Point

	childAt, parentAt     Point
	childSide, parentSide side

	labelRect Rect
}

// hasOwnLabel reports whether r carries its own label rectangle, which the two
// special routing categories do and an ordinary inter-rank relationship does
// not - its name is drawn in the strip at the top of its label NODE's band.
//
// A zero WIDTH is the test rather than a zero rectangle, because packing moves
// the rectangle: a rectangle left at its zero value picks up an origin the
// moment the block is translated, and would then read as a label rectangle
// somewhere near the drawing's corner. Zero width is the one thing a measured
// label can never have, since routeStaple gives even an empty name two
// cellPadH of padding.
func (r *route) hasOwnLabel() bool { return r.labelRect.W > 0 }

// laneY is the y of lane index k above a half-rank whose topmost node is at
// top.
//
// Lane 0 is nearest the boxes and they are numbered outward, so a staple's
// horizontal run climbs no higher than it has to. The pitch is coord.go's
// lanePitch = labelHeight + 2*labelGap, and that equation is what keeps a
// label centred on one lane's run from touching the lane next to it.
func laneY(top Coord, k int) Coord { return top - loopRise - Coord(k)*lanePitch }

// router is the state all three route shapes read: the graph and the ranks
// that say which shape a relationship is, the finished rectangles, and the
// right boundary of each half-rank.
//
// A struct rather than five arguments repeated at every call, for the same
// reason simplex.go's solver is one: every step of the routing reads most of
// it, and a signature long enough to wrap is a signature nobody checks.
type router struct {
	g     *graph
	o     order
	ranks []int
	rects []Rect
	plan  corridors

	// columnRight[r] is past every node in half-rank r, so it is the LEFT EDGE
	// of the corridor between half-rank r and half-rank r+1 - which is what
	// channelX measures a channel's x from.
	//
	// What makes it safe to measure from is rankX: it puts half-rank r+1 at
	// exactly columnRight[r] + gapWidth(count[r]), so the corridor's width is
	// that gapWidth exactly, and the outermost channel of the band lands
	// exactly channelMargin short of the next column. interRankPoints has the
	// argument for why a line anywhere in that corridor crosses no node.
	columnRight []Coord
}

func newRouter(g *graph, o order, ranks []int, rects []Rect, plan corridors) *router {
	rt := &router{g: g, o: o, ranks: ranks, rects: rects, plan: plan}
	rt.columnRight = make([]Coord, len(o.layers))
	for r, layer := range o.layers {
		for _, v := range layer {
			rt.columnRight[r] = max(rt.columnRight[r], rects[v].Right())
		}
	}
	return rt
}

// channelX is the x of channel k in the corridor after half-rank r.
//
// The band of channels is inset channelMargin from both boundaries of the
// corridor, so channel 0 is channelMargin past the last node of column r and
// the last one is channelMargin short of column r+1. gapWidth is the other
// half of that equality and TestCorridorHoldsItsChannels asserts the two agree.
func (rt *router) channelX(r, k int) Coord {
	return rt.columnRight[r] + channelMargin + Coord(k)*channelPitch
}

// staple is one same-rank relationship or self-reference waiting for a lane.
type staple struct {
	selfLoop bool
	pos      int // the order index of the lower of its two ends in the half-rank
	edge     int
	route    int
}

// allocateLanes gives every staple in the component the y of its horizontal run
// and the y of the top of its label plate, both indexed by position in routes. A
// route that is not a staple gets 0 in both, which nothing reads.
//
// # What the lanes prevent
//
// Both special categories run horizontally above their own half-rank. Two of
// them at the same height would be collinear, which makes two relationships
// indistinguishable to a reader and breaks the collinear-overlap invariant -
// the one invariant in the set that is about legibility rather than about
// geometry being possible.
//
// One pool per half-rank shared by both categories, rather than a pool each:
// the point is a total order across everything that runs above one half-rank,
// and two pools would only order each category within itself, leaving a
// self-reference's run free to land on a same-rank relationship's.
//
// The order is self-references first, then same-rank relationships; then by the
// order index of the lower of the relationship's two ends within the half-rank;
// then by relationship index, which is what makes it total. Self-references
// first because a staple over one box has the shortest horizontal run and
// belongs nearest the boxes, where it reads as belonging to that box.
//
// # Why the plates are stacked ABOVE every lane and not each above its own run
//
// One staple over a box puts its plate labelGap above its own run, which is what
// D19 says and what this still does - laneY(top, n-1+k) is laneY(top, 0) when
// n is 1 and k is 0, so the single-staple case is unchanged to the byte.
//
// Two staples over one box cannot both do that, and the reason is a measurement
// rather than an opinion. A staple is slotSpacing wide and a label is as wide as
// its text: fk_categories_parent_id measures 1831 against a staple spread of
// 140. So the plate of the inner staple reaches far past the legs of the outer
// one, and the outer staple's legs - which must come down to the box's top edge
// - cut straight through the inner staple's name. That is not a near miss: the
// first document with two self-references on one table hit it, and phase 13's
// TestNoEdgeThroughNonIncidentLabel is what found it.
//
// Stacking the plates above the topmost lane fixes it by construction: every leg
// stops at its own lane, every lane is at or below the topmost one, and every
// plate is strictly above the topmost one, so no leg can reach any plate. The
// cost is that with several staples over one box a name sits a band or two above
// its own line rather than immediately above it, in the same order as the lanes.
// The alternatives were worse: narrowing the plate makes the name unreadable,
// offsetting it sideways puts it over the neighbouring column, and detouring the
// legs around it turns the staple into a staircase and gives up the one shape
// both special categories share.
func (rt *router) allocateLanes(routes []route) (lanes, plates []Coord) {
	lanes = make([]Coord, len(routes))
	plates = make([]Coord, len(routes))

	for r, layer := range rt.o.layers {
		if len(layer) == 0 {
			continue
		}
		top := rt.rects[layer[0]].Y
		for _, v := range layer {
			top = min(top, rt.rects[v].Y)
		}

		var staples []staple
		for k := range routes {
			e := &rt.g.edges[routes[k].edge]
			kind := classify(rt.g, rt.ranks, routes[k].edge)
			if kind == routeInterRank || rt.ranks[e.child] != r {
				continue
			}
			staples = append(staples, staple{
				selfLoop: kind == routeSelfLoop,
				pos:      max(rt.o.pos[e.child], rt.o.pos[e.parent]),
				edge:     routes[k].edge,
				route:    k,
			})
		}

		slices.SortFunc(staples, func(a, b staple) int {
			switch {
			case a.selfLoop != b.selfLoop:
				if a.selfLoop {
					return -1
				}
				return 1
			case a.pos != b.pos:
				return a.pos - b.pos
			default:
				return a.edge - b.edge
			}
		})
		for k, s := range staples {
			lanes[s.route] = laneY(top, k)
			// The plate sits in the band above lane n-1+k: labelGap above that
			// line, labelHeight tall, which leaves labelGap between it and the
			// band above. lanePitch is what makes those three add up.
			plates[s.route] = laneY(top, len(staples)-1+k) - labelGap - labelHeight
		}
	}

	return lanes, plates
}

// routeStaple is the shape BOTH special categories are drawn as: up out of one
// attachment to the lane, across, and down into the other. Three segments, and
// the label centred horizontally on the run, in the plate allocateLanes
// reserved for it - which is one labelGap above the run itself whenever this is
// the only staple over its half-rank, and a band higher for each staple below it
// when it is not. allocateLanes has the measurement that forces the difference.
//
// It is written once and called twice on purpose. A self-reference is the
// degenerate case of a same-rank relationship where the two boxes are the same
// box, so the two categories are one shape reached with different arguments
// rather than two routers that have to be kept saying the same thing. Nothing
// in here asks which case it is in.
//
// # Why a staple and not an arc
//
// Two reasons, both about the invariants rather than about taste. An arc would
// be the one piece of geometry in the drawing that is not axis-aligned, which
// costs the strongest line in the frozen set its totality - it would have to
// read "every ORDINARY segment", and an invariant with an exception is an
// invariant nobody can check by looking. And testing an integer arc against a
// rectangle is not exact, so the overlap predicates would need a tolerance,
// which is the one thing integer geometry was chosen to avoid.
//
// # What this shape cannot do
//
// A half-rank is a COLUMN, so the two boxes of a same-rank relationship are
// stacked one above the other rather than side by side, and a staple over
// their tops has to come back down past the upper one to reach the lower one's
// top edge. There is no orthogonal route between two boxes in one column that
// leaves both of them from the top and avoids the box between, so this is a
// consequence of the side policy (D08) and not of this function. It is also
// unobservable: every layering edge carries a minimum length of one rank, so no
// document can put both ends of a relationship in one half-rank, and the only
// way to reach this state at all is to write the ranks by hand, which is what
// the test for it does. If a jjf document could ever produce a flat
// relationship, the fix would be a side policy for that category - out of the
// left and right faces, into a lane in the corridor - rather than a second
// router.
//
// # Why the label is placed here
//
// Rank doubling gives every relationship an intermediate half-rank to put a
// label node in - except one that spans no half-ranks at all, which is exactly
// these two categories. So their labels have nowhere to live but here. Leaving
// them out is not a deferrable refinement either: the edge fixture under
// internal/export/svg/testdata has categories -> categories carrying
// fk_categories_parent, and a staple drawn without it would be the one
// relationship in the picture a reader cannot name.
func routeStaple(from, to Point, lane, plate Coord, label string) (points []Point, labelRect Rect) {
	points = []Point{
		from,
		{X: from.X, Y: lane},
		{X: to.X, Y: lane},
		to,
	}

	width := measureText(label) + 2*cellPadH
	return points, Rect{
		X: (from.X+to.X)/2 - width/2,
		Y: plate,
		W: width,
		H: labelHeight,
	}
}

// interRankPoints joins the child's attachment, every node of the
// relationship's chain and the parent's attachment into one axis-aligned
// polyline.
//
// There is no obstacle test anywhere in here, and there is not meant to be:
// the corridor a route runs down was already reserved by the virtual nodes
// that were ordered and positioned like any other node, so nothing is in the
// way. An obstacle test here would be a second layout system arguing with the
// first.
//
// # Why the bends land where they do
//
// The corridor between one column and the next contains NO node at all, and
// that is an identity rather than an observation: rankX puts half-rank r+1 at
// columnRight[r] + gapWidth(count[r]), and every node in half-rank r is
// left-aligned at that half-rank's x and no wider than the widest of them, so
// columnRight[r] is past all of them and the whole strip up to the next column
// is empty. A vertical line ANYWHERE in that strip, over a y interval of any
// length, therefore crosses no node's interior.
//
// That is what makes "edge x non-incident node interior = 0" hold with no
// obstacle test, and it is a STRONGER argument than the one it replaces, not a
// weaker one: a whole node-free region instead of the two node-free lines a
// column has. The horizontal runs are covered by the other half of the same
// argument - no two nodes in one half-rank overlap in y, and the separation
// constraints put a chain node's route line clear of every other node's y
// extent in its column, so a horizontal run at a route line crosses nothing in
// the column either, and the corridors it reaches into on both sides are empty
// by the paragraph above.
//
// Every leg is horizontal and then vertical, the last one included. Both ends
// of every route therefore meet their box with a perpendicular segment, by one
// rule rather than by two: a route that slid along its parent's left edge
// before stopping would be collinear with the border and would draw its crow's
// foot onto a line the reader cannot separate from the box.
//
// # Why the vertical is in a channel and not on the boundary
//
// Because the boundary is one line and there can be many routes. One channel
// per overlapping y interval is what makes two relationships approaching one
// column two lines instead of one, which is the sixth frozen invariant -
// collinear edge-segment overlap = 0. Measured on the drawing this replaced, at
// commit bd917e1: 56 overlapping vertical pairs on a fifteen-table hub, the
// longest of them sharing 268 px of line, because the fifteen relationships
// into that lookup collapsed onto two trunks no reader could trace apart.
// planCorridors decides which channel each step gets; this function only reads
// the plan.
//
// A step whose two waypoints share a y is skipped: the route runs straight
// across that corridor, turns nowhere in it, and holds no channel - which is
// why a drawing whose routes all come out straight costs no width at all.
func (rt *router) interRankPoints(chain []int, r *route) []Point {
	ws := waypoints(rt.g, rt.ranks, chain, rt.rects, r)

	points := []Point{r.childAt}
	for i := 0; i+1 < len(ws); i++ {
		if ws[i].y == ws[i+1].y {
			continue
		}
		gap := min(ws[i].rank, ws[i+1].rank)
		x := rt.channelX(gap, rt.plan.channel[r.edge][i])
		points = addCorner(points, Point{X: x, Y: ws[i+1].y})
	}
	return addCorner(points, r.parentAt)
}

// addCorner appends p, inserting the corner that keeps both new segments
// axis-aligned when p differs from the last point on both axes. The corner is
// always the HORIZONTAL leg first, which is the whole of the route shape:
// across to the channel, down or up it, and across again.
//
// It took a horizontalFirst flag until the channels arrived, because the last
// leg of a route used to slide down the parent's own column boundary and turn
// into its face. With the turn inside the corridor there is one rule for every
// leg, and a boolean parameter with one caller passing one value is dead code.
//
// Nor does it drop a point equal to the one already there any more. That guard
// existed because the old shape emitted a point at each chain node's entry AND
// its exit, which are the same point for a zero-width virtual node; this one
// emits one corner per BEND and nothing at all for a step that does not bend,
// so no call can repeat a point. Two consecutive bends are in two different
// corridors, whose channels are separated by at least a column plus two
// channelMargins.
func addCorner(points []Point, p Point) []Point {
	last := points[len(points)-1]
	if last.X != p.X && last.Y != p.Y {
		points = append(points, Point{X: p.X, Y: last.Y})
	}
	return append(points, p)
}

// newRoutes is one empty route per relationship of this component, in
// relationship order, with the two sides the side policy fixed for it.
//
// It is a function rather than four lines inside routeComponent because the
// corridor planner needs the same list, from the same input, before any x
// exists: it has to know which relationships this component draws and which
// face each end attaches to in order to work out where the routes bend. Two
// copies of this loop would be two answers to "which relationships are these",
// and the plan is indexed by relationship id precisely so that the two lists
// cannot be confused for each other.
func newRoutes(g *graph, o order, ranks []int) []route {
	member := make([]bool, len(g.nodes))
	for _, layer := range o.layers {
		for _, v := range layer {
			member[v] = true
		}
	}

	routes := make([]route, 0, len(g.edges))
	for i := range g.edges {
		if !member[g.edges[i].child] {
			continue
		}
		childSide, parentSide := sidesOf(g, ranks, i)
		routes = append(routes, route{edge: i, childSide: childSide, parentSide: parentSide})
	}
	return routes
}

// chainNodesOf re-keys the chains by relationship id, which is how every
// caller wants them: a relationship with no chain - a self-reference, or a
// same-rank relationship - answers nil rather than needing a lookup that can
// fail.
func chainNodesOf(g *graph, chains []chain) [][]int {
	out := make([][]int, len(g.edges))
	for _, c := range chains {
		out[c.edge] = c.nodes
	}
	return out
}

// routeComponent draws every relationship of one component: one route per
// relationship, in relationship order, self-references included.
//
// One route per relationship is the local form of the invariant that the number
// of drawn relationships equals the number of foreign keys in the document. It
// holds here by construction - newRoutes loops over the relationships - and the
// point of saying so is that nothing downstream may drop one.
func routeComponent(g *graph, o order, ranks []int, chains []chain, rects []Rect, plan corridors) []route {
	rt := newRouter(g, o, ranks, rects, plan)
	chainNodes := chainNodesOf(g, chains)
	routes := newRoutes(g, o, ranks)

	assignSlots(g, o, chainNodes, rects, routes)
	lanes, plates := rt.allocateLanes(routes)

	for k := range routes {
		r := &routes[k]
		switch classify(g, ranks, r.edge) {
		case routeInterRank:
			r.points = rt.interRankPoints(chainNodes[r.edge], r)
		case routeSameRank, routeSelfLoop:
			label := g.edges[r.edge].label
			r.points, r.labelRect = routeStaple(r.childAt, r.parentAt, lanes[k], plates[k], label)
		}
	}
	return routes
}
