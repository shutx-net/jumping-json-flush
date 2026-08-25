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

	// columnRight[r] is past every node in half-rank r. Together with the
	// half-rank's own x - which every node in it shares, because rankX
	// left-aligns them - it is one of the two vertical lines in a column that
	// no box's interior touches. interRankPoints has the argument for why
	// those two are the only lines a vertical segment ever runs along.
	columnRight []Coord
}

func newRouter(g *graph, o order, ranks []int, rects []Rect) *router {
	rt := &router{g: g, o: o, ranks: ranks, rects: rects}
	rt.columnRight = make([]Coord, len(o.layers))
	for r, layer := range o.layers {
		for _, v := range layer {
			rt.columnRight[r] = max(rt.columnRight[r], rects[v].Right())
		}
	}
	return rt
}

// staple is one same-rank relationship or self-reference waiting for a lane.
type staple struct {
	selfLoop bool
	pos      int // the order index of the lower of its two ends in the half-rank
	edge     int
	route    int
}

// allocateLanes gives every staple in the component the y of its horizontal
// run, indexed by position in routes. A route that is not a staple gets 0,
// which nothing reads.
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
func (rt *router) allocateLanes(routes []route) []Coord {
	lanes := make([]Coord, len(routes))

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
		}
	}

	return lanes
}

// routeStaple is the shape BOTH special categories are drawn as: up out of one
// attachment to the lane, across, and down into the other. Three segments, and
// the label centred on the horizontal run one labelGap above it.
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
// fk_categories_parent, and `jjf export dot` draws that name today, so an
// unlabelled staple would be a regression against the exporter this one is
// meant to agree with.
func routeStaple(from, to Point, lane Coord, label string) (points []Point, labelRect Rect) {
	points = []Point{
		from,
		{X: from.X, Y: lane},
		{X: to.X, Y: lane},
		to,
	}

	width := measureText(label) + 2*cellPadH
	return points, Rect{
		X: (from.X+to.X)/2 - width/2,
		Y: lane - labelGap - labelHeight,
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
// Every leg is horizontal first and then vertical, except the last, which is
// vertical and then horizontal. That gives both ends a segment perpendicular to
// the box side it touches - a route that slid along its parent's left edge
// before stopping would be collinear with the border and would draw its crow's
// foot onto a line the reader cannot separate from the box.
//
// Where the VERTICAL part of each bend runs is the load-bearing choice, and it
// is what makes "edge x non-incident node interior = 0" hold with no obstacle
// test. Two facts about the layout do the work. Every node in a half-rank
// shares its left edge, because rankX left-aligns them, so a vertical line at
// a half-rank's x touches every box in that column on its boundary and enters
// none of their interiors. And no two nodes in one half-rank overlap in y, so a
// horizontal line at one node's route line - or at an attachment point on its
// own side - crosses no sibling either.
//
// So a route entering a chain node from the left bends at that node's own x -
// its half-rank's x - and one entering from the right bends at the column's
// right boundary, which is past every node in it. Those are the two lines in a
// column that are always safe, and every vertical segment of every route sits
// on one of them.
func (rt *router) interRankPoints(chain []int, childAt, parentAt Point) []Point {
	rightwards := childAt.X < parentAt.X

	points := []Point{childAt}
	for _, v := range chain {
		y := anchorY(rt.g.nodes[v].kind, rt.rects[v])
		lead, trail := rt.rects[v].X, rt.columnRight[rt.ranks[v]]
		if !rightwards {
			lead, trail = trail, rt.rects[v].X
		}
		points = addCorner(points, Point{X: lead, Y: y}, true)
		points = addCorner(points, Point{X: trail, Y: y}, true)
	}
	return addCorner(points, parentAt, false)
}

// addCorner appends p, inserting the corner that keeps both new segments
// axis-aligned when p differs from the last point on both axes.
//
// horizontalFirst says which way round the two segments go. A point equal to
// the one already there is dropped rather than repeated: a zero-width virtual
// node's entry and exit are the same point, and a repeated point would be a
// zero-length segment for every later pass to have an opinion about.
func addCorner(points []Point, p Point, horizontalFirst bool) []Point {
	last := points[len(points)-1]
	if last.X != p.X && last.Y != p.Y {
		corner := Point{X: last.X, Y: p.Y}
		if horizontalFirst {
			corner = Point{X: p.X, Y: last.Y}
		}
		points = append(points, corner)
	}
	if points[len(points)-1] != p {
		points = append(points, p)
	}
	return points
}

// routeComponent draws every relationship of one component: one route per
// relationship, in relationship order, self-references included.
//
// One route per relationship is the local form of the invariant that the number
// of drawn relationships equals the number of foreign keys in the document. It
// holds here by construction - the loop below is over the relationships - and
// the point of saying so is that nothing downstream may drop one.
func routeComponent(g *graph, o order, ranks []int, chains []chain, rects []Rect) []route {
	rt := newRouter(g, o, ranks, rects)

	member := make([]bool, len(g.nodes))
	for _, layer := range o.layers {
		for _, v := range layer {
			member[v] = true
		}
	}

	chainNodes := make([][]int, len(g.edges))
	for _, c := range chains {
		chainNodes[c.edge] = c.nodes
	}

	routes := make([]route, 0, len(g.edges))
	for i := range g.edges {
		if !member[g.edges[i].child] {
			continue
		}
		childSide, parentSide := sidesOf(g, ranks, i)
		routes = append(routes, route{edge: i, childSide: childSide, parentSide: parentSide})
	}

	assignSlots(g, o, chainNodes, rects, routes)
	lanes := rt.allocateLanes(routes)

	for k := range routes {
		r := &routes[k]
		switch classify(g, ranks, r.edge) {
		case routeInterRank:
			r.points = rt.interRankPoints(chainNodes[r.edge], r.childAt, r.parentAt)
		case routeSameRank, routeSelfLoop:
			label := g.edges[r.edge].label
			r.points, r.labelRect = routeStaple(r.childAt, r.parentAt, lanes[k], label)
		}
	}
	return routes
}
