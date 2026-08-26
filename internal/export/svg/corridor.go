package svg

import "slices"

// ---------------------------------------------------------------------------
// Channels: where an inter-rank route turns, and what that costs in width
// ---------------------------------------------------------------------------

// noChannel is the channel index of a route step that needs no channel: its two
// waypoints share a y, so the route runs straight across the corridor and never
// turns in it.
const noChannel = -1

// corridors is one component's channel plan: how many channels each corridor
// holds, and which channel each vertical segment of each route runs on.
//
// count is indexed by half-rank, and count[r] is the corridor AFTER half-rank r
// - the empty strip between column r's right boundary and column r+1's x. The
// last entry is always 0, because there is no corridor after the last column.
//
// channel is indexed by RELATIONSHIP id, holding one entry per STEP of that
// relationship's waypoint sequence: a channel index, or noChannel for a step
// that does not bend. Relationships belonging to another component are left
// nil, which is the convention the ranks and the order already follow.
//
// Indexed by relationship id rather than by position in a routes slice on
// purpose. The pass that plans and the pass that draws each build their own
// route list, and an index meaning "the third route in whichever list you
// happen to be holding" is exactly how the two would come to disagree about
// which line goes where - silently, because both lists are the same length.
type corridors struct {
	count   []int
	channel [][]int
}

// gapWidth is how wide the corridor after a half-rank has to be to hold n
// channels: channelMargin in from each of its two boundaries, and one
// channelPitch between each adjacent pair.
//
// n == 0 answers rankGap rather than 2*channelMargin, and that is the decision
// this function encodes rather than the arithmetic. A corridor no route bends
// in gains nothing from being wider, and widening it would charge the
// coordinate pass for its own success: the drawings where every route comes out
// straight are the ones that pass did best on. rankGap therefore keeps both of
// its meanings - the narrowest corridor the drawing contains, and the one an
// optionality glyph has to fit in.
//
// Measured over the invariant table at this commit, as the width of the widest
// component against the same drawing with every corridor forced to rankGap:
// nofk.json, "a table with two self-references" and "several components of
// different sizes" grow by nothing at all - not one x moves - and full.json
// grows 2%.
func gapWidth(n int) Coord {
	if n == 0 {
		return rankGap
	}
	return 2*channelMargin + Coord(n-1)*channelPitch
}

// waypoint is one point of the half-rank/y sequence an inter-rank route passes
// through: which column it is in, and the y it runs at there.
//
// The x is deliberately absent. That is the whole reason this type exists: the
// sequence is knowable before any column has a position, which is what lets the
// corridor planner run before rankX.
type waypoint struct {
	rank int
	y    Coord
}

// waypoints is the sequence for one inter-rank relationship: its child's
// attachment, then one entry per node of its chain at that node's route line,
// then its parent's attachment. Consecutive entries are always one half-rank
// apart, because a chain holds exactly one node per half-rank strictly between
// the two ends.
//
// It is written once and called from both planCorridors and interRankPoints,
// which is not a tidiness preference. If the planner and the router disagreed
// by one step about what a route passes through, the plan would size a corridor
// for a vertical the drawing puts somewhere else, and every gate in the package
// would stay green while the picture came out wrong.
func waypoints(g *graph, ranks []int, chain []int, rects []Rect, r *route) []waypoint {
	e := &g.edges[r.edge]
	out := make([]waypoint, 0, len(chain)+2)
	out = append(out, waypoint{rank: ranks[e.child], y: r.childAt.Y})
	for _, v := range chain {
		out = append(out, waypoint{rank: ranks[v], y: anchorY(g.nodes[v].kind, rects[v])})
	}
	return append(out, waypoint{rank: ranks[e.parent], y: r.parentAt.Y})
}

// corridorVertical is one vertical segment waiting for a channel: the y
// interval it covers, which end of it is on the corridor's LEFT boundary, and
// the route step it belongs to.
//
// descending says the interval's lo is its left end - the route enters the
// corridor from the left column at the higher point and leaves into the right
// column at the lower one. It is a sort key rather than a decoration; see
// compareCorridorVerticals.
type corridorVertical struct {
	lo, hi     Coord
	descending bool
	edge       int
	step       int
}

// compareCorridorVerticals is the total order channels are handed out in, and
// it is total for the reason compareSlotEntries is: two runs that disagree
// about two channels produce two different diagrams, and the difference is
// invisible to every test that does not compare bytes.
//
// The second key is not a tie-break for tidiness. When one route's vertical
// starts at exactly the y another's ends at, the two horizontal runs that meet
// there both reach into this corridor - one from the left column, one from the
// right - and they are drawn along each other unless the one arriving from the
// LEFT turns first, on the lower-indexed channel. Putting descending before
// ascending at an equal lo is what makes the lowest-free rule below produce
// that assignment by construction rather than by luck. TestNoCollinearEdgeOverlap
// has the four-way case analysis and names the three sub-cases this does not
// cover.
//
// It is written as an argument and not defended by a test because the
// configuration it guards against has not occurred: measured over the invariant
// table and two larger hubs, reversing this one key changed no channel index on
// any of them. A key that is currently unobservable is still the difference
// between "safe by construction" and "safe so far".
func compareCorridorVerticals(a, b corridorVertical) int {
	switch {
	case a.lo != b.lo:
		return int(a.lo - b.lo)
	case a.descending != b.descending:
		if a.descending {
			return -1
		}
		return 1
	case a.hi != b.hi:
		return int(a.hi - b.hi)
	case a.edge != b.edge:
		return a.edge - b.edge
	default:
		return a.step - b.step
	}
}

// planCorridors decides how many channels each corridor of one component holds
// and which channel every vertical segment of every route runs on.
//
// # Why a routing decision is taken inside the coordinate pass
//
// Because this is the one moment where the answer is knowable and the question
// is still open. x and y are independent in this package: rankX reads only
// widths, assignY reads only heights, and assignSlots reads only a rectangle's
// Y and H on the sides that matter (TestSlotYIsIndependentOfX holds that up).
// So every y in the drawing is final before any x exists, and the exact set of
// vertical segments - with their exact y intervals - can be computed while the
// corridor they will sit in still has no width. A corridor's width is a
// function of precisely that set.
//
// The alternative is to count one channel per route CROSSING the corridor,
// which needs no y at all and is therefore the only count available before
// assignY runs. It is measurably worse, and both numbers below were measured at
// this commit by building the two plans over the invariant table and comparing
// them. Worst corridor of the drawing, crossing count against this exact count:
// 15 against 5 on the fifteen-table hub, 8 against 2 on the layered mesh, 5
// against 3 on edge.json, 3 against 1 on cycle.json. In component width against
// the same drawing with every corridor at rankGap: +20% against +8% on the hub.
//
// Decisively, on "several components of different sizes" every route comes out
// straight and needs no channel at all: the exact count leaves that drawing
// byte-identical in x, and the crossing count widens every one of its corridors
// by a total of 9% for channels no route uses. That is the coordinate pass being
// charged for its own success, which is the same mistake A5 found in slotAt.
//
// # Why the greedy is enough
//
// The verticals of one corridor are intervals on a line, so their conflict
// graph is an interval graph, and for an interval graph left-edge greedy uses
// exactly the chromatic number - the largest number of them that pairwise share
// a stretch of line. "The corridor is as wide as it has to be and no wider" is
// therefore a theorem rather than a measurement.
//
// A channel is free for a new vertical exactly when overlapsMoreThanAPoint says
// their two intervals may share a line, and that is the predicate itself and not
// a copy of it: collinearOverlap asks the same question of two vertical
// segments, so the allocator's rule and the invariant's rule are one function
// and cannot drift apart. A stricter rule - demand a positive gap between two
// intervals on one channel - was considered and rejected: it needs a separation
// constant with no defensible value, in integers "one tenth of a pixel apart"
// would pass it while looking exactly like touching, and - measured at this
// commit over the invariant table and two larger hubs, by colouring every
// corridor both ways - it never once produced a different channel count.
//
// One pool per corridor, shared by both kinds of vertical - the approach into a
// chain node and the final approach into a parent box - for the reason
// allocateLanes gives for sharing a pool between self-references and same-rank
// relationships: two pools would order each kind within itself and leave one
// kind's line free to land on the other's.
func planCorridors(g *graph, o order, ranks []int, chains []chain, widths, heights, tops []Coord) corridors {
	// The rectangles as they will finally be, minus the one thing that does not
	// exist yet. Every x is 0, and nothing below may read one.
	rects := make([]Rect, len(g.nodes))
	for _, layer := range o.layers {
		for _, v := range layer {
			rects[v] = Rect{Y: tops[v], W: widths[v], H: heights[v]}
		}
	}

	chainNodes := chainNodesOf(g, chains)
	routes := newRoutes(g, o, ranks)
	assignSlots(g, o, chainNodes, rects, routes)

	// A slice indexed by half-rank rather than a map keyed by one: the package's
	// determinism rule is that no map is ranged over where the order could reach
	// the answer, and the channel indices reach every x in the drawing.
	verticals := make([][]corridorVertical, len(o.layers))
	channel := make([][]int, len(g.edges))
	for k := range routes {
		r := &routes[k]
		if classify(g, ranks, r.edge) != routeInterRank {
			continue
		}
		ws := waypoints(g, ranks, chainNodes[r.edge], rects, r)
		steps := make([]int, len(ws)-1)
		for i := range steps {
			steps[i] = noChannel
			if ws[i].y == ws[i+1].y {
				continue
			}
			// The corridor is the min of the two half-ranks whichever way the
			// relationship runs, so one the cycle breaker reversed is not a
			// special case here.
			gap := min(ws[i].rank, ws[i+1].rank)
			leftY, rightY := ws[i].y, ws[i+1].y
			if ws[i].rank > ws[i+1].rank {
				leftY, rightY = rightY, leftY
			}
			verticals[gap] = append(verticals[gap], corridorVertical{
				lo:         min(leftY, rightY),
				hi:         max(leftY, rightY),
				descending: leftY < rightY,
				edge:       r.edge,
				step:       i,
			})
		}
		channel[r.edge] = steps
	}

	count := make([]int, len(o.layers))
	for gap, vs := range verticals {
		slices.SortFunc(vs, compareCorridorVerticals)

		// occupied[k] is the last interval placed on channel k, which is also
		// the one reaching furthest down it: the intervals on one channel are
		// pairwise clear of each other and arrive in increasing lo, so they are
		// in increasing hi too. Testing the last one is therefore testing all
		// of them.
		var occupied []corridorVertical
		for _, v := range vs {
			k := 0
			for ; k < len(occupied); k++ {
				if !overlapsMoreThanAPoint(occupied[k].lo, occupied[k].hi, v.lo, v.hi) {
					break
				}
			}
			if k == len(occupied) {
				occupied = append(occupied, v)
			} else {
				occupied[k] = v
			}
			channel[v.edge][v.step] = k
		}
		count[gap] = len(occupied)
	}

	return corridors{count: count, channel: channel}
}
