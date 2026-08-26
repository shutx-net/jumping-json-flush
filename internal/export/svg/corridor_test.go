package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// planned is positioned plus the channel plan, for doc's first component.
func planned(doc *model.Document) (laidOut, order, []Rect, corridors) {
	l := layOut(doc)
	o := orderComponent(l.g, l.members, l.ranks, l.chains)
	demands := slotDemand(l.g, l.members, l.ranks)
	rects, plan := positionComponent(l.g, o, l.ranks, l.chains, demands)
	return l, o, rects, plan
}

// plannedComponent is one component of a document laid out and routed in its
// own coordinate space, which is where the corridor statements below are true:
// packing translates a whole block rigidly, so the relations survive it, but
// the half-rank x values it was built from do not come out the other side.
type plannedComponent struct {
	g      *graph
	o      order
	ranks  []int
	chains []chain
	rects  []Rect
	plan   corridors
	routes []route
}

// plannedComponents runs the per-component pipeline over EVERY component,
// unlike planned, because the documents in the invariant table have several and
// a corridor belongs to one of them.
func plannedComponents(doc *model.Document) []plannedComponent {
	g := buildGraph(doc)
	out := make([]plannedComponent, 0, len(g.components))
	for c := range g.components {
		members := g.components[c]
		breakCycles(g, members)
		ranks := assignRanks(g, members)
		members, ranks, chains := insertVirtualNodes(g, members, ranks)
		o := orderComponent(g, members, ranks, chains)
		demands := slotDemand(g, members, ranks)
		rects, plan := positionComponent(g, o, ranks, chains, demands)
		out = append(out, plannedComponent{
			g:      g,
			o:      o,
			ranks:  ranks,
			chains: chains,
			rects:  rects,
			plan:   plan,
			routes: routeComponent(g, o, ranks, chains, rects, plan),
		})
	}
	return out
}

// columnEdges is each half-rank's x and the boundary past every node in it,
// read back off the finished rectangles.
//
// Derived from the drawing rather than taken from rankX and newRouter, which is
// the point: every statement below is about where the lines actually are, not
// about what the two passes that placed them believe.
func columnEdges(o order, rects []Rect) (xs, right []Coord) {
	xs = make([]Coord, len(o.layers))
	right = make([]Coord, len(o.layers))
	for r, layer := range o.layers {
		for i, v := range layer {
			if i == 0 {
				xs[r] = rects[v].X
			}
			xs[r] = min(xs[r], rects[v].X)
			right[r] = max(right[r], rects[v].Right())
		}
	}
	return xs, right
}

// TestGapWidth is the one place the width rule is stated as data rather than as
// arithmetic inside a pass.
func TestGapWidth(t *testing.T) {
	tests := []struct {
		channels int
		want     Coord
	}{
		// D45: a corridor no route bends in is exactly as wide as it has always
		// been, so a drawing whose routes all come out straight costs nothing.
		{0, rankGap},
		{1, 2 * channelMargin},
		{2, 2*channelMargin + channelPitch},
		{5, 2*channelMargin + 4*channelPitch},
	}

	for _, tt := range tests {
		if got := gapWidth(tt.channels); got != tt.want {
			t.Errorf("gapWidth(%d) = %d, want %d", tt.channels, got, tt.want)
		}
	}
}

// TestCorridorHoldsItsChannels is the fit guarantee between the pass that sized
// a corridor and the pass that puts lines in it.
//
// An equality rather than an inequality, deliberately: both sides come from the
// same two constants, so "the outermost channel is exactly channelMargin short
// of the next column" is what the arithmetic says, and anything else means
// gapWidth and channelX have come to disagree.
func TestCorridorHoldsItsChannels(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			for c, pc := range plannedComponents(tt.doc) {
				xs, _ := columnEdges(pc.o, pc.rects)
				rt := newRouter(pc.g, pc.o, pc.ranks, pc.rects, pc.plan)
				for r, n := range pc.plan.count {
					if n == 0 {
						continue
					}
					first := rt.channelX(r, 0)
					last := rt.channelX(r, n-1)
					if want := rt.columnRight[r] + channelMargin; first != want {
						t.Errorf("component %d corridor %d: channel 0 is at %d, want %d",
							c, r, first, want)
					}
					if want := xs[r+1] - channelMargin; last != want {
						t.Errorf("component %d corridor %d holds %d channel(s), the last at %d, want %d - the corridor and its channels disagree",
							c, r, n, last, want)
					}
				}
			}
		})
	}
}

// corridorOf is the corridor a vertical segment runs in - the half-rank whose
// right boundary it is past and whose successor's x it is short of - or -1 when
// it is on neither side of any of them.
//
// Strictly between, which is what makes this the statement the routing
// promises: a line ON either boundary is legal geometry and is exactly the
// collapse this phase undid, since every route approaching one column would
// share it.
func corridorOf(xs, right []Coord, s Segment) int {
	for gap := 0; gap+1 < len(xs); gap++ {
		if right[gap] < s.A.X && s.A.X < xs[gap+1] {
			return gap
		}
	}
	return -1
}

// TestEveryVerticalRunsInACorridor asserts the by-construction statement behind
// "edge x non-incident node interior = 0" directly, instead of inferring it
// from that invariant happening to pass.
//
// Inter-rank routes only. A staple's two legs are vertical as well, and they
// rise out of a box's top edge inside the column rather than in a corridor;
// allocateLanes is what keeps those apart.
func TestEveryVerticalRunsInACorridor(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			for c, pc := range plannedComponents(tt.doc) {
				xs, right := columnEdges(pc.o, pc.rects)
				for _, r := range pc.routes {
					if classify(pc.g, pc.ranks, r.edge) != routeInterRank {
						continue
					}
					for _, s := range segments(r) {
						if s.vertical() && corridorOf(xs, right, s) < 0 {
							t.Errorf("component %d relationship %d: the vertical %+v is not strictly inside any corridor; the column edges are %v and %v",
								c, r.edge, s, right, xs)
						}
					}
				}
			}
		})
	}
}

// verticalsInCorridors is every vertical segment of every inter-rank route,
// grouped by the corridor its x falls in, as the y intervals the allocator
// spoke about.
//
// Read off the FINISHED routes rather than out of the plan: the point of the
// test below is to compute the answer a second way, and re-reading the
// planner's own list would only restate it.
func verticalsInCorridors(pc plannedComponent) [][]Segment {
	xs, right := columnEdges(pc.o, pc.rects)
	out := make([][]Segment, len(xs))
	for _, r := range pc.routes {
		if classify(pc.g, pc.ranks, r.edge) != routeInterRank {
			continue
		}
		for _, s := range segments(r) {
			if gap := corridorOf(xs, right, s); s.vertical() && gap >= 0 {
				out[gap] = append(out[gap], s)
			}
		}
	}
	return out
}

// TestChannelCountIsTheMaximumOverlap is the "as wide as it has to be" claim,
// checked rather than restated.
//
// The independent count is a sweep over the interval endpoints: the largest
// number of the corridor's verticals that pairwise share a stretch of line.
// Pairwise is the same as "all through one point" for intervals on a line, so
// counting the intervals live at each interval's start is exact, and it is a
// different computation from the greedy that produced the answer. Left-edge
// greedy uses exactly the chromatic number of an interval graph, and for an
// interval graph the chromatic number is the clique number, so the two must
// agree; if they ever do not, the allocator is not doing what its comment says.
func TestChannelCountIsTheMaximumOverlap(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			for c, pc := range plannedComponents(tt.doc) {
				for gap, vs := range verticalsInCorridors(pc) {
					most := 0
					for _, at := range vs {
						start := min(at.A.Y, at.B.Y)
						live := 0
						for _, other := range vs {
							lo, hi := min(other.A.Y, other.B.Y), max(other.A.Y, other.B.Y)
							if lo <= start && start < hi {
								live++
							}
						}
						most = max(most, live)
					}
					if got := pc.plan.count[gap]; got != most {
						t.Errorf("component %d corridor %d holds %d channel(s) for %d vertical(s) whose largest pairwise overlap is %d",
							c, gap, got, len(vs), most)
					}
				}
			}
		})
	}
}

// TestStraightRoutesNeedNoChannel is D45 asserted, and it is also the
// regression test for the temptation to count the routes CROSSING a corridor
// instead of the ones bending in it: every route in this document comes out
// straight, so a per-crossing count would widen every corridor for channels
// nothing uses.
func TestStraightRoutesNeedNoChannel(t *testing.T) {
	doc := componentsDocument(3, 1, 1, 5, 2)
	for c, pc := range plannedComponents(doc) {
		for r, n := range pc.plan.count {
			if n != 0 {
				t.Errorf("component %d corridor %d holds %d channel(s), want none: every route in this document is straight",
					c, r, n)
			}
		}

		// And therefore the columns are spaced exactly as they were before
		// corridors had a width at all.
		xs, right := columnEdges(pc.o, pc.rects)
		for r := 0; r+1 < len(xs); r++ {
			if got := xs[r+1] - right[r]; got != rankGap {
				t.Errorf("component %d corridor %d is %d wide, want rankGap %d", c, r, got, rankGap)
			}
		}
	}
}

// TestSlotYIsIndependentOfX is the property the whole two-step order rests on.
//
// The corridor planner runs assignSlots on rectangles that have their final
// size and their final y and no x at all, and the router runs it again on the
// finished ones. Without this, the fact that both passes call it is two
// computations of one number - K10, and the reason Geometry's own comment
// warns about two measurements of one width. With it, it is one number computed
// twice from inputs that provably agree on the part that matters.
func TestSlotYIsIndependentOfX(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			for c, pc := range plannedComponents(tt.doc) {
				chainNodes := chainNodesOf(pc.g, pc.chains)

				withX := newRoutes(pc.g, pc.o, pc.ranks)
				assignSlots(pc.g, pc.o, chainNodes, pc.rects, withX)

				stripped := make([]Rect, len(pc.rects))
				for v := range pc.rects {
					stripped[v] = Rect{Y: pc.rects[v].Y, W: pc.rects[v].W, H: pc.rects[v].H}
				}
				withoutX := newRoutes(pc.g, pc.o, pc.ranks)
				assignSlots(pc.g, pc.o, chainNodes, stripped, withoutX)

				for k := range withX {
					if withX[k].childAt.Y != withoutX[k].childAt.Y {
						t.Errorf("component %d relationship %d: the child attachment is at y %d with the x values and %d without them",
							c, withX[k].edge, withX[k].childAt.Y, withoutX[k].childAt.Y)
					}
					if withX[k].parentAt.Y != withoutX[k].parentAt.Y {
						t.Errorf("component %d relationship %d: the parent attachment is at y %d with the x values and %d without them",
							c, withX[k].edge, withX[k].parentAt.Y, withoutX[k].parentAt.Y)
					}
				}
			}
		})
	}
}

// TestPlanCorridorsIsDeterministic: two runs, the same counts and the same
// channel indices. A plan that varied would move every x in the drawing, which
// is the one thing the goldens cannot survive.
func TestPlanCorridorsIsDeterministic(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			first := plannedComponents(tt.doc)
			second := plannedComponents(tt.doc)
			for c := range first {
				if !reflect.DeepEqual(first[c].plan, second[c].plan) {
					t.Errorf("component %d: two runs disagree:\n%+v\n%+v", c, first[c].plan, second[c].plan)
				}
			}
		})
	}
}
