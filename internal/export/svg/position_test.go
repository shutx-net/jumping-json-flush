package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// positioned runs every stage up to and including this one over doc's first
// component and hands back the graph, the order and the rectangles.
func positioned(doc *model.Document) (laidOut, order, []Rect) {
	l := layOut(doc)
	o := orderComponent(l.g, l.members, l.ranks, l.chains)
	demands := slotDemand(l.g, l.members, l.ranks)
	return l, o, positionComponent(l.g, o, demands)
}

// routeTravel is the quantity the coordinate pass minimises: the weighted
// vertical distance every route has to travel between the nodes it passes
// through. It is recomputed here from the finished rectangles rather than read
// out of the solver, so it says something about the drawing and not about the
// auxiliary graph.
func routeTravel(g *graph, o order, rects []Rect) Coord {
	var total Coord
	for _, layer := range o.layers {
		for _, v := range layer {
			for _, w := range o.next[v] {
				a := anchorY(g.nodes[v].kind, rects[v])
				b := anchorY(g.nodes[w].kind, rects[w])
				total += Coord(omega(g.nodes[v].kind, g.nodes[w].kind)) * max(a-b, b-a)
			}
		}
	}
	return total
}

// TestOmegaIsTotalOverTheKinds names all three weights, which is also what
// keeps staticcheck from reporting the ones this pipeline cannot reach: the
// real-real case needs two boxes in adjacent half-ranks, and rank doubling
// puts a label node between every pair.
func TestOmegaIsTotalOverTheKinds(t *testing.T) {
	tests := []struct {
		a, b nodeKind
		want int
	}{
		{kindTable, kindStub, omegaRealReal},
		{kindTable, kindTable, omegaRealReal},
		{kindTable, kindLabel, omegaRealVirtual},
		{kindStub, kindVirtual, omegaRealVirtual},
		{kindLabel, kindVirtual, omegaVirtualVirtual},
		{kindVirtual, kindVirtual, omegaVirtualVirtual},
		{kindLabel, kindLabel, omegaVirtualVirtual},
	}

	for _, tt := range tests {
		if got := omega(tt.a, tt.b); got != tt.want {
			t.Errorf("omega(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		// Symmetric, because a step's weight is a property of the pair and not
		// of which end the route reaches first. balanceY relies on it: it
		// asks for the weight with the moving node first, whichever direction
		// the step runs.
		if got := omega(tt.b, tt.a); got != tt.want {
			t.Errorf("omega(%d, %d) = %d, want %d (not symmetric)", tt.b, tt.a, got, tt.want)
		}
	}

	if !(omegaRealReal < omegaRealVirtual && omegaRealVirtual < omegaVirtualVirtual) {
		t.Errorf("the weights %d, %d, %d must increase: a bend between two virtual nodes is the most expensive one",
			omegaRealReal, omegaRealVirtual, omegaVirtualVirtual)
	}
}

func TestRankXMonotonic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, o, _ := positioned(tt.doc)
			demands := slotDemand(l.g, l.members, l.ranks)
			widths, _ := finalSizes(l.g, demands)
			xs := rankX(o, widths)

			for r := range xs {
				if r == 0 && xs[r] != 0 {
					t.Errorf("the first half-rank starts at %d, want 0", xs[r])
				}
				if r == 0 {
					continue
				}
				var widest Coord
				for _, v := range o.layers[r-1] {
					widest = max(widest, widths[v])
				}
				if xs[r] <= xs[r-1] {
					t.Errorf("half-rank %d starts at %d, which is not right of %d", r, xs[r], xs[r-1])
				}
				if got := xs[r] - xs[r-1] - widest; got != rankGap {
					t.Errorf("the corridor after half-rank %d is %d wide, want rankGap %d", r-1, got, rankGap)
				}
			}
		})
	}
}

// TestRankXUsesWidestNode pins the one thing a half-rank's width depends on:
// its own widest node, and nothing else.
func TestRankXUsesWidestNode(t *testing.T) {
	l, o, _ := positioned(document(
		linked("short", "p"),
		linked("a_considerably_longer_table_name", "p"),
		linked("p"),
	))
	demands := slotDemand(l.g, l.members, l.ranks)
	widths, _ := finalSizes(l.g, demands)
	xs := rankX(o, widths)

	narrow, wide := widths[0], widths[1]
	if narrow >= wide {
		t.Fatalf("the two tables in the first half-rank are %d and %d wide; the test needs them to differ", narrow, wide)
	}
	if want := wide + rankGap; xs[1] != want {
		t.Errorf("the second half-rank starts at %d, want %d (the wide table plus rankGap)", xs[1], want)
	}
}

func TestAssignYSeparation(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, o, rects := positioned(tt.doc)
			for r, layer := range o.layers {
				for i := 0; i+1 < len(layer); i++ {
					a, b := layer[i], layer[i+1]
					if gap := rects[b].Y - rects[a].Bottom(); gap < nodeSep {
						t.Errorf("half-rank %d: the gap between node %d and node %d is %d, want at least nodeSep %d",
							r, a, b, gap, nodeSep)
					}
				}
			}
		})
	}
}

// TestAssignYNoOverlapWithinRank is strictly stronger than the separation
// property and is the local form of the node-overlap invariant the whole suite
// will re-assert globally in phase 13.
func TestAssignYNoOverlapWithinRank(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, o, rects := positioned(tt.doc)
			for r, layer := range o.layers {
				for i := range layer {
					for j := i + 1; j < len(layer); j++ {
						a, b := rects[layer[i]], rects[layer[j]]
						if overlapsMoreThanAPoint(a.Y, a.Bottom(), b.Y, b.Bottom()) {
							t.Errorf("half-rank %d: nodes %d %+v and %d %+v overlap across the rank axis",
								r, layer[i], a, layer[j], b)
						}
					}
				}
			}
		})
	}
}

// TestAssignYStraightensALongEdge is the assertion that the omega weights are
// doing their job. A relationship crossing four half-ranks has a label node
// and two virtual nodes between its ends, and all three have to sit on one
// line or the route arrives at its parent as a staircase. If this fails the
// weights are wrong, not the test - read K01 before touching either.
func TestAssignYStraightensALongEdge(t *testing.T) {
	l, _, rects := positioned(document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c"),
	))

	var longest []int
	for _, c := range l.chains {
		if len(c.nodes) > len(longest) {
			longest = c.nodes
		}
	}
	if len(longest) != 3 {
		t.Fatalf("the longest chain holds %d node(s), want 3 (a label node and two virtual nodes)", len(longest))
	}

	want := anchorY(l.g.nodes[longest[0]].kind, rects[longest[0]])
	for _, v := range longest {
		if got := anchorY(l.g.nodes[v].kind, rects[v]); got != want {
			t.Errorf("node %d's route line is at %d, want %d - the chain is not straight", v, got, want)
		}
	}
}

// TestAssignYCentresABoxBetweenItsRoutes is the balance step: the solver
// returns one end of a range it is free to choose in, and a box with two
// incoming relationships belongs in the middle of that range rather than flush
// against one of them.
func TestAssignYCentresABoxBetweenItsRoutes(t *testing.T) {
	// 0 a and 1 b both point at 2 p, so p's two incoming route lines are its
	// two label nodes 3 and 4, and any y between them costs the same.
	l, _, rects := positioned(document(
		linked("a", "p"),
		linked("b", "p"),
		linked("p"),
	))

	upper := anchorY(l.g.nodes[3].kind, rects[3])
	lower := anchorY(l.g.nodes[4].kind, rects[4])
	if upper >= lower {
		t.Fatalf("the two label nodes are at %d and %d; the test needs them apart", upper, lower)
	}
	if got, want := anchorY(l.g.nodes[2].kind, rects[2]), (upper+lower)/2; got != want {
		t.Errorf("p sits at %d, want %d - midway between the two routes reaching it", got, want)
	}
}

// TestBalanceYNeverLengthensTheRoutes is the guard the balance step is allowed
// to exist because of: it may move a box only to a place that is no worse. The
// solver's answer is the baseline, and the finished drawing is compared
// against it over every document in the set.
func TestBalanceYNeverLengthensTheRoutes(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l := layOut(tt.doc)
			o := orderComponent(l.g, l.members, l.ranks, l.chains)
			demands := slotDemand(l.g, l.members, l.ranks)
			widths, heights := finalSizes(l.g, demands)

			// The solver's answer, unbalanced, turned into rectangles the same
			// way assignY would.
			edges, n, local := auxGraph(l.g, o, heights)
			solved := rank(edges, n)
			xs := rankX(o, widths)
			before := make([]Rect, len(l.g.nodes))
			for r, layer := range o.layers {
				for _, v := range layer {
					above, _ := anchorOffsets(l.g.nodes[v].kind, heights[v])
					before[v] = Rect{X: xs[r], Y: Coord(solved[local[v]]) - above, W: widths[v], H: heights[v]}
				}
			}

			after := positionComponent(l.g, o, demands)
			if got, want := routeTravel(l.g, o, after), routeTravel(l.g, o, before); got > want {
				t.Errorf("balancing lengthened the routes: %d after, %d before", got, want)
			}
		})
	}
}

func TestAssignYStartsAtZero(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, o, rects := positioned(tt.doc)
			var top Coord
			first := true
			for _, layer := range o.layers {
				for _, v := range layer {
					if first || rects[v].Y < top {
						top, first = rects[v].Y, false
					}
				}
			}
			// Every component is laid out in its own coordinate space with its
			// own origin; putting components next to each other is packing,
			// which is a later pass and would have nothing to translate if
			// this were not true.
			if top != 0 {
				t.Errorf("the component's highest node is at %d, want 0", top)
			}
		})
	}
}

func TestPositionComponentNodesStayInTheirRankBand(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, o, rects := positioned(tt.doc)
			demands := slotDemand(l.g, l.members, l.ranks)
			widths, _ := finalSizes(l.g, demands)
			xs := rankX(o, widths)

			for r, layer := range o.layers {
				var widest Coord
				for _, v := range layer {
					widest = max(widest, widths[v])
				}
				for _, v := range layer {
					// Left-aligned in the band, and never past its right edge:
					// the corridor between two half-ranks is rankGap wide
					// everywhere, which is what the routing relies on when it
					// joins waypoints without testing for obstacles.
					if rects[v].X != xs[r] {
						t.Errorf("node %d is at x %d, want its half-rank's %d", v, rects[v].X, xs[r])
					}
					if rects[v].Right() > xs[r]+widest {
						t.Errorf("node %d reaches x %d, past its half-rank's right edge %d", v, rects[v].Right(), xs[r]+widest)
					}
				}
			}
		})
	}
}

func TestPositionComponentVirtualNodesAreZeroWidth(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, o, rects := positioned(tt.doc)
			for _, layer := range o.layers {
				for _, v := range layer {
					if l.g.nodes[v].kind != kindVirtual {
						continue
					}
					// A virtual node is the point its route passes through, so
					// its rectangle has no interior at all and its anchor is
					// simply its y. That is what lets the router read a
					// waypoint off any node with no case analysis.
					if rects[v].W != 0 || rects[v].H != 0 {
						t.Errorf("virtual node %d is %dx%d, want 0x0", v, rects[v].W, rects[v].H)
					}
					if got := anchorY(kindVirtual, rects[v]); got != rects[v].Y {
						t.Errorf("virtual node %d's route line is %d, want its y %d", v, got, rects[v].Y)
					}
				}
			}
		})
	}
}

func TestPositionComponentSingleNode(t *testing.T) {
	l, o, rects := positioned(document(linked("alone")))
	if len(o.layers) != 1 || len(o.layers[0]) != 1 {
		t.Fatalf("layers = %v, want one node in one half-rank", o.layers)
	}
	want := Rect{W: l.g.nodes[0].content.width, H: l.g.nodes[0].content.height}
	if rects[0] != want {
		t.Errorf("rect = %+v, want %+v", rects[0], want)
	}
}

// TestPositionComponentOnDocumentShape is this phase's real gate, and it is
// deliberately laborious: the whole rectangle list for two shapes, written out
// rather than sampled.
//
// What is derived and what is pinned, because the difference matters here. The
// heights, the widths and every x follow from the constants and the
// measurement with no choice in them. The y values follow from two statements
// about the optimum, both of which can be checked by hand: a route runs
// straight through its label node where nothing stops it, so a label node's
// route line is its box's centre; and where two nodes share a half-rank the
// separation between them is tight, because slack there costs travel
// somewhere. What is NOT derived is which of several equally optimal answers
// the solver returns - for the second shape, label7 and label5 could sit
// straight against coupons instead of tight against label6 for exactly the
// same total - so those two values are a tie-break pinned here. A change in
// them is a change in the solver's behaviour, which is worth being told about
// even though it is not by itself a bug.
func TestPositionComponentOnDocumentShape(t *testing.T) {
	headerHeight := 2*lineHeight + 2*cellPadV
	oneColumn := headerHeight + rowHeight
	twoColumns := headerHeight + 2*rowHeight
	threeColumns := headerHeight + 3*rowHeight
	band := labelHeight + labelGap

	t.Run("one relationship", func(t *testing.T) {
		// 0 child, 1 parent, 2 the label node between them. Nothing shares a
		// half-rank, so the whole route is straight: the label node's route
		// line, the child's centre and the parent's centre are one y.
		l, _, rects := positioned(document(linked("child", "parent"), linked("parent")))
		w := func(v int) Coord { return rects[v].W }

		centre := twoColumns / 2
		want := []Rect{
			{X: 0, Y: 0, W: w(0), H: twoColumns},
			{X: w(0) + rankGap + w(2) + rankGap, Y: centre - oneColumn/2, W: w(1), H: oneColumn},
			{X: w(0) + rankGap, Y: centre - band, W: w(2), H: band},
		}
		if !reflect.DeepEqual(rects, want) {
			t.Errorf("rects =\n%+v\nwant\n%+v", rects, want)
		}
		if l.g.nodes[2].kind != kindLabel {
			t.Errorf("node 2 is kind %d, want the label node", l.g.nodes[2].kind)
		}
	})

	t.Run("the full fixture's shape", func(t *testing.T) {
		// 0 customers, 1 customer_profiles, 2 orders, 3 order_lines,
		// 4 coupons, 5..8 the label nodes of the four relationships in
		// document order. Half-ranks: order_lines 0, its label 1,
		// orders and customer_profiles 2, three labels 3, customers and
		// coupons 4 - the order TestOrderComponentOnDocumentShape pins.
		_, _, rects := positioned(document(
			linked("customers"),
			linked("customer_profiles", "customers"),
			linked("orders", "customers", "coupons"),
			linked("order_lines", "orders"),
			linked("coupons"),
		))
		w := func(v int) Coord { return rects[v].W }

		x1 := w(3) + rankGap
		x2 := x1 + w(8) + rankGap
		x3 := x2 + max(w(2), w(1)) + rankGap
		x4 := x3 + max(w(6), max(w(7), w(5))) + rankGap

		// orders is the tallest node of the half-rank everything else is
		// aligned against, and normalisation puts it at 0.
		centre := threeColumns / 2
		label6 := centre - band
		label7 := label6 + band + nodeSep
		customers := centre - oneColumn/2

		want := []Rect{
			{X: x4, Y: customers, W: w(0), H: oneColumn},
			{X: x2, Y: threeColumns + nodeSep, W: w(1), H: twoColumns},
			{X: x2, Y: 0, W: w(2), H: threeColumns},
			{X: 0, Y: centre - twoColumns/2, W: w(3), H: twoColumns},
			{X: x4, Y: customers + oneColumn + nodeSep, W: w(4), H: oneColumn},
			{X: x3, Y: label7 + band + nodeSep, W: w(5), H: band},
			{X: x3, Y: label6, W: w(6), H: band},
			{X: x3, Y: label7, W: w(7), H: band},
			{X: x1, Y: centre - band, W: w(8), H: band},
		}
		if !reflect.DeepEqual(rects, want) {
			for v := range rects {
				if rects[v] != want[v] {
					t.Errorf("rects[%d] = %+v, want %+v", v, rects[v], want[v])
				}
			}
		}
	})
}

func TestAssignYIsDeterministic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, _, first := positioned(tt.doc)
			_, _, second := positioned(tt.doc)
			if !reflect.DeepEqual(first, second) {
				t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
			}
		})
	}
}
