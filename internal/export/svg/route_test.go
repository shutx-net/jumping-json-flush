package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// routed runs every stage up to and including this one over doc's first
// component.
func routed(doc *model.Document) (laidOut, order, []Rect, []route) {
	l := layOut(doc)
	o := orderComponent(l.g, l.members, l.ranks, l.chains)
	demands := slotDemand(l.g, l.members, l.ranks)
	rects, plan := positionComponent(l.g, o, l.ranks, l.chains, demands)
	return l, o, rects, routeComponent(l.g, o, l.ranks, l.chains, rects, plan)
}

// handRouted does the same against ranks written out by hand, which is the only
// way to reach the same-rank category at all: every layering edge carries a
// minimum length of one rank, so no document can put both ends of a
// relationship in one half-rank. The ranks go in before doubling, exactly where
// the solver's answer would.
func handRouted(doc *model.Document, ranks []int) (*graph, order, []Rect, []route) {
	g := buildGraph(doc)
	members, ranks, chains := insertVirtualNodes(g, g.components[0], ranks)
	o := orderComponent(g, members, ranks, chains)
	demands := slotDemand(g, members, ranks)
	rects, plan := positionComponent(g, o, ranks, chains, demands)
	return g, o, rects, routeComponent(g, o, ranks, chains, rects, plan)
}

// segments is r's polyline as the segments the invariants speak about.
func segments(r route) []Segment {
	out := make([]Segment, 0, len(r.points)-1)
	for i := 0; i+1 < len(r.points); i++ {
		out = append(out, Segment{A: r.points[i], B: r.points[i+1]})
	}
	return out
}

// segmentContains reports whether the axis-aligned segment s passes through p,
// its two ends included.
//
// The routes no longer emit a POINT at every waypoint they pass - a step whose
// two ends share a y draws nothing at all now - so "the route goes through
// here" has to be asked of the segments rather than of the point list. That is
// the stronger question in any case: a point at a node's x was only ever a
// proxy for the route running down the corridor that node reserved.
func segmentContains(s Segment, p Point) bool {
	switch {
	case s.A.Y == s.B.Y && p.Y == s.A.Y:
		return min(s.A.X, s.B.X) <= p.X && p.X <= max(s.A.X, s.B.X)
	case s.A.X == s.B.X && p.X == s.A.X:
		return min(s.A.Y, s.B.Y) <= p.Y && p.Y <= max(s.A.Y, s.B.Y)
	default:
		return false
	}
}

// routeOf finds the route drawn for relationship i.
func routeOf(routes []route, i int) *route {
	for k := range routes {
		if routes[k].edge == i {
			return &routes[k]
		}
	}
	return nil
}

// selfLoopDocument is the one document shape that reaches the staple through
// the ordinary pipeline: a table referencing itself, which is the shape
// internal/export/svg/testdata's edge fixture carries as categories.
func selfLoopDocument() *model.Document {
	return document(linked("categories", "categories"))
}

// staplesDocument is a half-rank holding a self-reference and a same-rank
// relationship, so that both share one lane pool. Its ranks have to be written
// by hand; see handRouted.
func staplesDocument() *model.Document {
	return document(linked("a", "a", "b"), linked("b"))
}

// withRows is t with one more plain column per name, which is how one box in a
// document is made taller than another without giving it a relationship of its
// own.
func withRows(t model.Table, names ...string) model.Table {
	for _, name := range names {
		t.Columns = append(t.Columns, bigint(name))
	}
	return t
}

// TestRouteStaysStraightWhenAnchorsAlign is the property the attachment slots
// are centred for, and it is stated as a property rather than as arithmetic
// about a slot on purpose.
//
// Coordinate assignment exists to line a relationship's two anchors up with the
// route line between them, so that a route across them comes out as one
// straight line; an attachment point that is not on its box's anchor throws
// that away at the last step, and the route arrives with a jog at each end. A
// jog is still axis-aligned, still exactly on the boundary and still outside
// every other box, so every other assertion in this package passes with it in
// place - which is what makes "all of the route's points share one y" the
// assertion that catches it, and the reason it is written down.
//
// The two boxes have DIFFERENT heights deliberately. Two boxes of equal height
// would let attachments measured from the top edge line up with each other by
// accident, and the assertion would then say nothing about where an attachment
// sits relative to the anchor.
func TestRouteStaysStraightWhenAnchorsAlign(t *testing.T) {
	l, _, rects, routes := routed(document(
		withRows(linked("customers"), "a", "b", "c", "d", "e", "f", "g"),
		linked("orders", "customers"),
	))
	if len(routes) != 1 {
		t.Fatalf("%d route(s), want exactly 1", len(routes))
	}
	r := routes[0]
	e := &l.g.edges[r.edge]

	// The two premises. Neither is what this test asserts - they are what makes
	// the assertion below mean what it says - so a failure here is a Fatal
	// about the document rather than about the routing.
	child := anchorY(l.g.nodes[e.child].kind, rects[e.child])
	parent := anchorY(l.g.nodes[e.parent].kind, rects[e.parent])
	if child != parent {
		t.Fatalf("the solver put the two anchors at %d and %d; this document no longer aligns them", child, parent)
	}
	if rects[e.child].H == rects[e.parent].H {
		t.Fatalf("both boxes are %d high; this document no longer gives them different heights", rects[e.child].H)
	}

	for _, p := range r.points {
		if p.Y != child {
			t.Fatalf("the route runs %+v, want every point on the aligned anchor line y=%d: a route the solver made straight has to stay straight",
				r.points, child)
		}
	}
}

func TestAssignSlotsOrder(t *testing.T) {
	// Three children of one parent. Their label nodes sit at three different
	// y values in the half-rank next to the parent, and the parent's three
	// left attachments have to arrive in that order, top to bottom - which is
	// what stops the three routes crossing each other in their last segment.
	l, _, rects, routes := routed(document(
		linked("a", "p"),
		linked("b", "p"),
		linked("c", "p"),
		linked("p"),
	))

	var lastY, lastWaypoint Coord
	for i := range 3 {
		r := routeOf(routes, i)
		if r == nil {
			t.Fatalf("relationship %d has no route", i)
		}
		chain := l.chainOf(i)
		waypoint := anchorY(l.g.nodes[chain[len(chain)-1]].kind, rects[chain[len(chain)-1]])
		if i > 0 {
			if waypoint <= lastWaypoint {
				t.Fatalf("relationship %d's label node is at %d, not below the previous one at %d",
					i, waypoint, lastWaypoint)
			}
			if r.parentAt.Y <= lastY {
				t.Errorf("relationship %d attaches at y %d, not below the previous attachment at %d",
					i, r.parentAt.Y, lastY)
			}
		}
		lastY, lastWaypoint = r.parentAt.Y, waypoint
	}
}

// TestAssignSlotsTieBreak is the case the relationship index decides.
//
// It is built on the TOP side deliberately. On a vertical side two attachments
// cannot tie: their keys are the route lines of two different nodes in one
// half-rank, and no two nodes in a half-rank share a y. On the top side every
// key is 0 by construction, so the index decides everything - and what it has
// to give is each self-reference's two ends side by side, in document order.
func TestAssignSlotsTieBreak(t *testing.T) {
	_, _, rects, routes := routed(document(linked("t", "t", "t")))

	// Four slots centred on the top side: the run of them is three spacings
	// long, so the first sits half of what is left of the width in from the
	// left edge.
	first := rects[0].X + (rects[0].W-3*slotSpacing)/2
	want := []Coord{
		first,
		first + slotSpacing,
		first + 2*slotSpacing,
		first + 3*slotSpacing,
	}
	got := []Coord{
		routeOf(routes, 0).childAt.X,
		routeOf(routes, 0).parentAt.X,
		routeOf(routes, 1).childAt.X,
		routeOf(routes, 1).parentAt.X,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the four top attachments are at %v, want %v", got, want)
	}
}

func TestAssignSlotsSpacing(t *testing.T) {
	// The parent's three left attachments, whose order the test above pins.
	_, _, rects, routes := routed(document(
		linked("a", "p"),
		linked("b", "p"),
		linked("c", "p"),
		linked("p"),
	))

	// Three slots centred on the parent's left side, so the first is half the
	// leftover height in from the top edge.
	first := rects[3].Y + (rects[3].H-2*slotSpacing)/2
	for i := range 3 {
		r := routeOf(routes, i)
		if want := rects[3].X; r.parentAt.X != want {
			t.Errorf("relationship %d attaches at x %d, want the parent's left edge %d", i, r.parentAt.X, want)
		}
		if want := first + Coord(i)*slotSpacing; r.parentAt.Y != want {
			t.Errorf("relationship %d attaches at y %d, want %d (the centred first slot then %d spacings)",
				i, r.parentAt.Y, want, i)
		}
	}
}

func TestAssignSlotsOnBoundary(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, _, rects, routes := routed(tt.doc)
			for _, r := range routes {
				e := &l.g.edges[r.edge]
				// Exactly on the boundary, which phase 13 asserts as an
				// equality: a point a unit inside "to be safe" is a test
				// failure, not a safety margin.
				if !onBoundary(r.childAt, rects[e.child]) {
					t.Errorf("relationship %d's child end %+v is not on %+v", r.edge, r.childAt, rects[e.child])
				}
				if !onBoundary(r.parentAt, rects[e.parent]) {
					t.Errorf("relationship %d's parent end %+v is not on %+v", r.edge, r.parentAt, rects[e.parent])
				}
				if r.points[0] != r.childAt || r.points[len(r.points)-1] != r.parentAt {
					t.Errorf("relationship %d's polyline runs %+v..%+v, want %+v..%+v",
						r.edge, r.points[0], r.points[len(r.points)-1], r.childAt, r.parentAt)
				}
			}
		})
	}
}

func TestRouteInterRankIsAxisAligned(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, _, _, routes := routed(tt.doc)
			for _, r := range routes {
				if classify(l.g, l.ranks, r.edge) != routeInterRank {
					continue
				}
				for _, s := range segments(r) {
					if !s.axisAligned() {
						t.Errorf("relationship %d has a diagonal segment %+v", r.edge, s)
					}
				}
			}
		})
	}
}

// TestRouteInterRankPassesThroughItsChain is what makes the corridor the
// virtual nodes reserved the corridor the route runs down.
//
// The statement moved with the channels, and it moved to the stronger one. It
// used to be "the polyline holds a POINT at each chain node's own x, in chain
// order", which was a proxy: a point there meant the route had turned at that
// column. A route now turns inside the CORRIDOR instead, and a step whose two
// waypoints share a y emits nothing at all - a route the coordinate pass made
// straight is two points end to end. So what is asserted is the thing the proxy
// stood for: some SEGMENT of the route contains the node's point.
func TestRouteInterRankPassesThroughItsChain(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, _, rects, routes := routed(tt.doc)
			for _, r := range routes {
				for _, v := range l.chainOf(r.edge) {
					want := Point{X: rects[v].X, Y: anchorY(l.g.nodes[v].kind, rects[v])}
					found := false
					for _, s := range segments(r) {
						if segmentContains(s, want) {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("relationship %d's route %+v misses node %d at %+v", r.edge, r.points, v, want)
					}
				}
			}
		})
	}
}

// TestRouteSelfLoopShape is the staple through the ordinary pipeline.
func TestRouteSelfLoopShape(t *testing.T) {
	l, _, rects, routes := routed(selfLoopDocument())
	if len(routes) != 1 {
		t.Fatalf("%d route(s), want 1", len(routes))
	}
	r := routes[0]
	if classify(l.g, l.ranks, r.edge) != routeSelfLoop {
		t.Fatalf("relationship 0 is not classified as a self-reference")
	}

	if r.childSide != sideTop || r.parentSide != sideTop {
		t.Errorf("the two ends are on sides %d and %d, want both on the top", r.childSide, r.parentSide)
	}
	if r.childAt.Y != rects[0].Y || r.parentAt.Y != rects[0].Y {
		t.Errorf("the ends are at y %d and %d, want the box's top edge %d", r.childAt.Y, r.parentAt.Y, rects[0].Y)
	}
	if got := r.parentAt.X - r.childAt.X; got != slotSpacing {
		t.Errorf("the two ends are %d apart, want slotSpacing %d", got, slotSpacing)
	}

	segs := segments(r)
	if len(segs) != 3 {
		t.Fatalf("the staple has %d segment(s), want 3: up, across, down", len(segs))
	}
	if !segs[0].vertical() || segs[0].B.Y >= segs[0].A.Y {
		t.Errorf("the first segment %+v is not a rise out of the box", segs[0])
	}
	if !segs[1].horizontal() {
		t.Errorf("the second segment %+v is not the horizontal run", segs[1])
	}
	if !segs[2].vertical() || segs[2].B.Y <= segs[2].A.Y {
		t.Errorf("the third segment %+v is not a descent into the box", segs[2])
	}
	// The run is above the box, in the lane pool over that half-rank.
	if want := laneY(rects[0].Y, 0); segs[1].A.Y != want {
		t.Errorf("the horizontal run is at %d, want lane 0 at %d", segs[1].A.Y, want)
	}
}

// TestRouteSameRankShape is the same staple across two boxes, reached through
// hand-written ranks because no document can produce a flat relationship.
func TestRouteSameRankShape(t *testing.T) {
	// 0 a with a self-reference and a relationship to 1 b, both boxes on
	// rank 0. a's top side therefore carries three attachments and b's one.
	g, _, rects, routes := handRouted(staplesDocument(), []int{0, 0})

	r := routeOf(routes, 1)
	if r == nil {
		t.Fatalf("the same-rank relationship has no route")
	}
	if got := classify(g, []int{0, 0, 0}, 1); got != routeSameRank {
		t.Fatalf("relationship 1 classifies as %d, want the same-rank category", got)
	}
	if r.childSide != sideTop || r.parentSide != sideTop {
		t.Errorf("the two ends are on sides %d and %d, want both on the top", r.childSide, r.parentSide)
	}

	segs := segments(*r)
	if len(segs) != 3 {
		t.Fatalf("the staple has %d segment(s), want 3: up, across, down", len(segs))
	}
	if !segs[0].vertical() || segs[0].B.Y >= segs[0].A.Y {
		t.Errorf("the first segment %+v is not a rise out of the child", segs[0])
	}
	if !segs[1].horizontal() {
		t.Errorf("the second segment %+v is not the horizontal run", segs[1])
	}
	if !segs[2].vertical() || segs[2].B.Y <= segs[2].A.Y {
		t.Errorf("the third segment %+v is not a descent into the parent", segs[2])
	}
	if r.childAt.Y != rects[0].Y || r.parentAt.Y != rects[1].Y {
		t.Errorf("the ends are at y %d and %d, want the two top edges %d and %d",
			r.childAt.Y, r.parentAt.Y, rects[0].Y, rects[1].Y)
	}
}

// TestLanesDoNotCollide is what the lane pool exists for: nothing that runs
// horizontally above one half-rank shares a y with anything else that does.
func TestLanesDoNotCollide(t *testing.T) {
	g, _, rects, routes := handRouted(staplesDocument(), []int{0, 0})

	// The self-reference is relationship 0 and the same-rank one is 1, so the
	// pool hands lane 0 to the self-reference and lane 1 to the other.
	top := min(rects[0].Y, rects[1].Y)
	loop := routeOf(routes, 0)
	flat := routeOf(routes, 1)
	if got, want := loop.points[1].Y, laneY(top, 0); got != want {
		t.Errorf("the self-reference runs at %d, want lane 0 at %d", got, want)
	}
	if got, want := flat.points[1].Y, laneY(top, 1); got != want {
		t.Errorf("the same-rank relationship runs at %d, want lane 1 at %d", got, want)
	}
	if gap := loop.points[1].Y - flat.points[1].Y; gap != lanePitch {
		t.Errorf("the two runs are %d apart, want lanePitch %d", gap, lanePitch)
	}
	_ = g

	// And two self-references over one box, which share a pool with nothing
	// else, still get a lane each.
	_, _, own, twice := routed(document(linked("t", "t", "t")))
	first, second := routeOf(twice, 0).points[1].Y, routeOf(twice, 1).points[1].Y
	if first != laneY(own[0].Y, 0) || second != laneY(own[0].Y, 1) {
		t.Errorf("the two self-references run at %d and %d, want %d and %d",
			first, second, laneY(own[0].Y, 0), laneY(own[0].Y, 1))
	}
}

func TestSpecialCategoryLabelPlacement(t *testing.T) {
	// The size and the horizontal centring are the same however many staples
	// share the half-rank; the y is the caller's business, because that is what
	// the number of staples decides.
	check := func(t *testing.T, label string, r *route, wantY Coord) {
		t.Helper()
		run := Segment{A: r.points[1], B: r.points[2]}
		if want := measureText(label) + 2*cellPadH; r.labelRect.W != want {
			t.Errorf("the label rect is %d wide, want the measured text plus padding %d", r.labelRect.W, want)
		}
		if r.labelRect.H != labelHeight {
			t.Errorf("the label rect is %d high, want labelHeight %d", r.labelRect.H, labelHeight)
		}
		if r.labelRect.Y != wantY {
			t.Errorf("the label sits at y %d, want %d", r.labelRect.Y, wantY)
		}
		centre := (run.A.X + run.B.X) / 2
		if got := r.labelRect.X + r.labelRect.W/2; got != centre {
			t.Errorf("the label's centre is at %d, want the run's centre %d", got, centre)
		}
	}

	// The only staple over its half-rank, which is the case every fixture under
	// testdata/ is in: the plate is one labelGap above its own run, which is
	// what D19 says.
	t.Run("a lone self-reference", func(t *testing.T) {
		l, _, _, routes := routed(selfLoopDocument())
		run := routes[0].points[1].Y
		check(t, l.g.edges[0].label, &routes[0], run-labelGap-labelHeight)
	})

	// Two staples over one half-rank cannot both put their plate above their own
	// run: a label is wider than a staple is - fk_a_a measures 620 against a
	// spread of 140 - so the outer staple's legs would come down through the
	// inner staple's name. The plates therefore stack in the bands above the
	// TOPMOST lane, in lane order. allocateLanes has the full reasoning; this is
	// the arithmetic, and the loop below is the property it exists for.
	t.Run("two staples over one half-rank stack their plates above every lane", func(t *testing.T) {
		g, _, rects, routes := handRouted(staplesDocument(), []int{0, 0})
		top := min(rects[0].Y, rects[1].Y)

		check(t, g.edges[0].label, routeOf(routes, 0), laneY(top, 1)-labelGap-labelHeight)
		check(t, g.edges[1].label, routeOf(routes, 1), laneY(top, 2)-labelGap-labelHeight)

		for k := range routes {
			for _, s := range segments(routes[k]) {
				for j := range routes {
					if j == k || !routes[j].hasOwnLabel() {
						continue
					}
					if s.intersectsInterior(routes[j].labelRect) {
						t.Errorf("relationship %d's segment %+v runs through relationship %d's label %+v",
							routes[k].edge, s, routes[j].edge, routes[j].labelRect)
					}
				}
			}
		}
	})
}

// TestEveryEdgeHasExactlyOneRoute is the local form of the invariant that the
// number of drawn relationships equals the number of foreign keys. It routes
// every component, because a document's relationships are spread over all of
// them.
func TestEveryEdgeHasExactlyOneRoute(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)
			drawn := make([]int, len(g.edges))
			for _, component := range g.components {
				breakCycles(g, component)
				ranks := assignRanks(g, component)
				members, ranks, chains := insertVirtualNodes(g, component, ranks)
				o := orderComponent(g, members, ranks, chains)
				demands := slotDemand(g, members, ranks)
				rects, plan := positionComponent(g, o, ranks, chains, demands)
				for _, r := range routeComponent(g, o, ranks, chains, rects, plan) {
					drawn[r.edge]++
				}
			}

			foreignKeys := 0
			for i := range tt.doc.Tables {
				foreignKeys += len(tt.doc.Tables[i].ForeignKeys)
			}
			if len(g.edges) != foreignKeys {
				t.Fatalf("%d edge(s) for %d foreign key(s)", len(g.edges), foreignKeys)
			}
			for i, n := range drawn {
				if n != 1 {
					t.Errorf("relationship %d is drawn %d time(s), want exactly 1", i, n)
				}
			}
		})
	}
}

func TestRouteComponentIsDeterministic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, first := routed(tt.doc)
			_, _, _, second := routed(tt.doc)
			if !reflect.DeepEqual(first, second) {
				t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
			}
		})
	}
}

// TestRouteAvoidsNonIncidentBoxes is the geometry the corridor argument in
// interRankPoints promises, asserted per component rather than waiting for the
// whole-drawing invariant suite. If it passes here and fails there, the bug is
// in packing or translation rather than in routing.
//
// The argument it is checking: the strip between one column's right boundary
// and the next column's x holds no node at all, because rankX puts the next
// column exactly gapWidth past the widest node of this one and every node in a
// half-rank is left-aligned. So a vertical anywhere in that strip, and a
// horizontal at a chain node's route line, cross nothing - with no obstacle
// test in the router to make it so.
func TestRouteAvoidsNonIncidentBoxes(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, o, rects, routes := routed(tt.doc)
			for _, r := range routes {
				incident := map[int]bool{l.g.edges[r.edge].child: true, l.g.edges[r.edge].parent: true}
				for _, v := range l.chainOf(r.edge) {
					incident[v] = true
				}
				for _, layer := range o.layers {
					for _, v := range layer {
						if incident[v] {
							continue
						}
						for _, s := range segments(r) {
							if s.intersectsInterior(rects[v]) {
								t.Errorf("relationship %d's segment %+v passes through node %d %+v",
									r.edge, s, v, rects[v])
							}
						}
					}
				}
			}
		})
	}
}
