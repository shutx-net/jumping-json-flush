package svg

import (
	"reflect"
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// handOrder builds an order out of nothing but layers and route steps, with no
// document behind it. The crossing count and the sweeps read exactly these
// four fields, so a hand-built value is the honest way to assert what they do:
// a fixture would put a ranking and a virtual insertion between the assertion
// and the thing it is about.
//
// Every step is given as the pair of nodes it joins, in either order; which of
// the two is the lower-ranked end comes from the layers.
func handOrder(layers [][]int, steps ...[2]int) order {
	n := 0
	for _, layer := range layers {
		for _, v := range layer {
			n = max(n, v+1)
		}
	}
	o := order{
		layers: layers,
		pos:    make([]int, n),
		prev:   make([][]int, n),
		next:   make([][]int, n),
	}
	rank := make([]int, n)
	for r, layer := range layers {
		for i, v := range layer {
			o.pos[v] = i
			rank[v] = r
		}
	}
	for _, s := range steps {
		lower, upper := s[0], s[1]
		if rank[lower] > rank[upper] {
			lower, upper = upper, lower
		}
		o.next[lower] = append(o.next[lower], upper)
		o.prev[upper] = append(o.prev[upper], lower)
	}
	return o
}

func TestCrossingsBetweenNone(t *testing.T) {
	// Two steps that do not cross: the order of their ends agrees in both
	// layers.
	o := handOrder([][]int{{0, 1}, {2, 3}}, [2]int{0, 2}, [2]int{1, 3})

	if got := crossingsBetween(o, 0); got != 0 {
		t.Errorf("crossingsBetween = %d, want 0", got)
	}
	if got := crossings(o); got != 0 {
		t.Errorf("crossings = %d, want 0", got)
	}
}

func TestCrossingsBetweenOne(t *testing.T) {
	// The same two steps with the upper layer's order swapped.
	o := handOrder([][]int{{0, 1}, {3, 2}}, [2]int{0, 2}, [2]int{1, 3})

	if got := crossingsBetween(o, 0); got != 1 {
		t.Errorf("crossingsBetween = %d, want 1", got)
	}
}

// TestCrossingsBetweenFourEdges pins an exact number rather than an
// inequality. Four steps joining four nodes to four nodes in exactly the
// reverse order: every one of the C(4,2) = 6 pairs is in the opposite order in
// the two layers, so all six cross.
func TestCrossingsBetweenFourEdges(t *testing.T) {
	o := handOrder([][]int{{0, 1, 2, 3}, {4, 5, 6, 7}},
		[2]int{0, 7}, [2]int{1, 6}, [2]int{2, 5}, [2]int{3, 4})

	if got := crossingsBetween(o, 0); got != 6 {
		t.Errorf("crossingsBetween = %d, want 6", got)
	}
}

// TestCrossingsBetweenSharedEnds is the case that makes the count a statement
// about ORDER rather than about ink: two steps out of one node, and two into
// one node, never cross, however their other ends are arranged.
func TestCrossingsBetweenSharedEnds(t *testing.T) {
	out := handOrder([][]int{{0}, {1, 2}}, [2]int{0, 1}, [2]int{0, 2})
	if got := crossingsBetween(out, 0); got != 0 {
		t.Errorf("two steps out of one node counted %d crossing(s), want 0", got)
	}

	in := handOrder([][]int{{0, 1}, {2}}, [2]int{0, 2}, [2]int{1, 2})
	if got := crossingsBetween(in, 0); got != 0 {
		t.Errorf("two steps into one node counted %d crossing(s), want 0", got)
	}
}

func TestCrossingsSumsOverLayers(t *testing.T) {
	// One crossing in each of the two boundaries: 0 and 1 reach 3 and 2, then
	// 2 and 3 reach 5 and 4.
	o := handOrder([][]int{{0, 1}, {2, 3}, {4, 5}},
		[2]int{0, 3}, [2]int{1, 2}, [2]int{2, 5}, [2]int{3, 4})

	if got := crossingsBetween(o, 0); got != 1 {
		t.Errorf("crossingsBetween(0) = %d, want 1", got)
	}
	if got := crossingsBetween(o, 1); got != 1 {
		t.Errorf("crossingsBetween(1) = %d, want 1", got)
	}
	if got := crossings(o); got != 2 {
		t.Errorf("crossings = %d, want 2", got)
	}
}

func TestMedianSweepReducesCrossings(t *testing.T) {
	// Three steps, each crossing the other two: three crossings. The medians
	// of 3, 4 and 5 are the positions of 2, 1 and 0, so one forward sweep puts
	// the upper layer in exactly the reverse of the order it had.
	o := handOrder([][]int{{0, 1, 2}, {3, 4, 5}},
		[2]int{0, 5}, [2]int{1, 4}, [2]int{2, 3})
	if got := crossings(o); got != 3 {
		t.Fatalf("crossings = %d before the sweep, want 3", got)
	}

	medianSweep(&o, true)

	if want := []int{5, 4, 3}; !slices.Equal(o.layers[1], want) {
		t.Errorf("layer 1 = %v, want %v", o.layers[1], want)
	}
	if got := crossings(o); got != 0 {
		t.Errorf("crossings = %d after the sweep, want 0", got)
	}
	for r := range o.layers {
		for i, v := range o.layers[r] {
			if o.pos[v] != i {
				t.Errorf("node %d is at index %d of layer %d and pos says %d", v, i, r, o.pos[v])
			}
		}
	}
}

// TestMedianSweepKeepsNeighbourlessNodes covers the rule a median of -1 would
// break: node 4 has no neighbour in the layer the sweep is reading, so it stays
// in the slot it had while 2 and 3 sort themselves into the slots around it.
func TestMedianSweepKeepsNeighbourlessNodes(t *testing.T) {
	o := handOrder([][]int{{0, 1}, {2, 3, 4}}, [2]int{0, 3}, [2]int{1, 2})

	medianSweep(&o, true)

	if want := []int{3, 2, 4}; !slices.Equal(o.layers[1], want) {
		t.Errorf("layer 1 = %v, want %v: node 4 keeps index 2 rather than being sorted to the front", o.layers[1], want)
	}
}

// TestMedianSweepTieKeepsTheEarlierOrder is the tie-break that can actually be
// reached from a layer: two nodes with the same median stay in the order they
// were in.
func TestMedianSweepTieKeepsTheEarlierOrder(t *testing.T) {
	o := handOrder([][]int{{0, 1}, {2, 3}}, [2]int{0, 2}, [2]int{0, 3})

	medianSweep(&o, true)

	if want := []int{2, 3}; !slices.Equal(o.layers[1], want) {
		t.Errorf("layer 1 = %v, want %v: both medians are 0, so the earlier order stands", o.layers[1], want)
	}
}

// TestOrderComponentTieBreakIsTotal asserts the comparator directly, because
// its last key cannot be reached from a real layer: two nodes in one layer
// always have different positions. It is asserted anyway so that the
// comparator stays a total order over its own type - the property that makes
// two runs of the whole exporter agree - rather than one that happens to work
// because of an invariant kept somewhere else.
func TestOrderComponentTieBreakIsTotal(t *testing.T) {
	tests := []struct {
		name string
		a, b medianKey
		want int
	}{
		{"the median decides", medianKey{median: 2, pos: 9, node: 9}, medianKey{median: 4, pos: 0, node: 0}, -1},
		{"an equal median falls to the current position", medianKey{median: 4, pos: 1, node: 9}, medianKey{median: 4, pos: 2, node: 0}, -1},
		{"an equal position falls to the node id", medianKey{median: 4, pos: 1, node: 7}, medianKey{median: 4, pos: 1, node: 8}, -1},
		{"one key against itself", medianKey{median: 4, pos: 1, node: 7}, medianKey{median: 4, pos: 1, node: 7}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareMedianKeys(tt.a, tt.b)
			if (got < 0) != (tt.want < 0) || (got == 0) != (tt.want == 0) {
				t.Errorf("compareMedianKeys(%+v, %+v) = %d, want the sign of %d", tt.a, tt.b, got, tt.want)
			}
			if back := compareMedianKeys(tt.b, tt.a); (back > 0) != (got < 0) {
				t.Errorf("the comparator is not antisymmetric: %d one way and %d the other", got, back)
			}
		})
	}
}

// TestTransposeOnlySwapsOnStrictImprovement: both possible swaps here leave the
// count exactly where it is, so neither may happen. With a <= in place of the <
// the layers would come back reordered and the result would depend on how many
// sweeps had run.
func TestTransposeOnlySwapsOnStrictImprovement(t *testing.T) {
	o := handOrder([][]int{{0, 1}, {2, 3}}, [2]int{0, 2}, [2]int{0, 3})
	if got := crossings(o); got != 0 {
		t.Fatalf("crossings = %d, want 0: the case is about a swap that changes nothing", got)
	}

	transpose(&o)

	if want := []int{0, 1}; !slices.Equal(o.layers[0], want) {
		t.Errorf("layer 0 = %v, want %v unchanged", o.layers[0], want)
	}
	if want := []int{2, 3}; !slices.Equal(o.layers[1], want) {
		t.Errorf("layer 1 = %v, want %v unchanged", o.layers[1], want)
	}
}

func TestTransposeTakesAnImprovement(t *testing.T) {
	// One crossing, removable by one adjacent swap - which is the case
	// transpose exists for and the reason the previous test proves something.
	//
	// Either layer's swap removes it, and the assertion names the one
	// transpose makes: it walks the layers in rank order and takes the first
	// improving swap it finds, so the crossing is undone in layer 0 and layer
	// 1 is then already right.
	o := handOrder([][]int{{0, 1}, {3, 2}}, [2]int{0, 2}, [2]int{1, 3})

	transpose(&o)

	want := [][]int{{1, 0}, {3, 2}}
	if !reflect.DeepEqual(o.layers, want) {
		t.Errorf("layers = %v, want %v", o.layers, want)
	}
	if got := crossings(o); got != 0 {
		t.Errorf("crossings = %d, want 0", got)
	}
}

// ordered runs every stage up to and including this one over doc's first
// component.
func ordered(doc *model.Document) (laidOut, order) {
	l := layOut(doc)
	return l, orderComponent(l.g, l.members, l.ranks, l.chains)
}

// TestOrderComponentOnDocumentShape asserts the layers exactly, for the two
// fixture shapes, derived by hand before the first run.
//
// Both come out as the order initOrder built: the sweeps find nothing strictly
// better, and a tie keeps the earlier order. That is the algorithm working as
// specified rather than doing nothing - a strict improvement rule is what makes
// the answer independent of how many sweeps ran - and it is why the sweeps
// themselves are asserted above on orders built by hand.
func TestOrderComponentOnDocumentShape(t *testing.T) {
	t.Run("the full fixture's shape", func(t *testing.T) {
		// 0 customers, 1 customer_profiles, 2 orders, 3 order_lines,
		// 4 coupons, then the label nodes 5..8 for the four relationships in
		// document order. Half-ranks: order_lines 0, its label 1,
		// customer_profiles and orders 2, three labels 3, customers and
		// coupons 4.
		_, o := ordered(document(
			linked("customers"),
			linked("customer_profiles", "customers"),
			linked("orders", "customers", "coupons"),
			linked("order_lines", "orders"),
			linked("coupons"),
		))

		want := [][]int{{3}, {8}, {2, 1}, {6, 7, 5}, {0, 4}}
		if !reflect.DeepEqual(o.layers, want) {
			t.Errorf("layers = %v, want %v", o.layers, want)
		}
		if got := crossings(o); got != 1 {
			t.Errorf("crossings = %d, want 1", got)
		}
	})

	t.Run("the edge fixture's shape", func(t *testing.T) {
		// 0 graph, 1 edge, 2 edge_labels, 3 categories, 4 node_stats, 5 the
		// stub for nodes, then the label nodes 6..11 for the six relationships
		// of this component in document order. categories is a component of
		// its own and is not in these layers.
		_, o := ordered(document(
			linked("graph"),
			model.Table{
				Name: "edge", LogicalName: "辺",
				Columns:    []model.Column{bigint("graph_id"), nullableBigint("from_node"), bigint("to_node")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"graph_id"}},
				ForeignKeys: []model.ForeignKey{
					fk("fk_edge_graph", "graph_id", "graph"),
					fk("", "from_node", "nodes"),
					fk("fk_edge_to_node", "to_node", "nodes"),
				},
			},
			model.Table{
				Name: "edge_labels", LogicalName: "辺ラベル",
				Columns:    []model.Column{bigint("graph_id"), bigint("seq")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"graph_id", "seq"}},
				ForeignKeys: []model.ForeignKey{{
					Name:       "fk_edge_labels_edge",
					Columns:    []string{"seq", "graph_id"},
					References: model.Reference{Table: "edge", Columns: []string{"graph_id", "seq"}},
				}},
			},
			linked("categories", "categories"),
			model.Table{
				Name: "node_stats", LogicalName: "ノード統計",
				Columns:    []model.Column{bigint("id"), bigint("node_id"), nullableBigint("missing_id")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				ForeignKeys: []model.ForeignKey{
					fk("fk_node_stats_node", "node_id", "nodes"),
					fk("fk_node_stats_unknown_column", "missing_id", "graph"),
				},
			},
		))

		want := [][]int{{2}, {9}, {1, 4}, {6, 7, 8, 11, 10}, {0, 5}}
		if !reflect.DeepEqual(o.layers, want) {
			t.Errorf("layers = %v, want %v", o.layers, want)
		}
		if got := crossings(o); got != 2 {
			t.Errorf("crossings = %d, want 2", got)
		}
	})
}

// TestOrderComponentNeverLosesANode is the assertion to read first when
// anything else in this file fails: a sweep that drops or duplicates a node
// makes every other statement about the order meaningless.
func TestOrderComponentNeverLosesANode(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l, o := ordered(tt.doc)

			var seen []int
			for _, layer := range o.layers {
				seen = append(seen, layer...)
			}
			slices.Sort(seen)
			want := slices.Clone(l.members)
			slices.Sort(want)
			if !slices.Equal(seen, want) {
				t.Errorf("the layers hold %v, want exactly the component's %v", seen, want)
			}

			for r, layer := range o.layers {
				for i, v := range layer {
					if o.pos[v] != i {
						t.Errorf("node %d is at index %d of layer %d and pos says %d", v, i, r, o.pos[v])
					}
					if l.ranks[v] != r {
						t.Errorf("node %d is in layer %d and its half-rank is %d", v, r, l.ranks[v])
					}
				}
			}
		})
	}
}

func TestOrderComponentIsDeterministic(t *testing.T) {
	doc := document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c", "a", "c"),
		linked("d", "b", "gone"),
	)

	_, first := ordered(doc)
	for range 4 {
		_, next := ordered(doc)
		if !reflect.DeepEqual(first.layers, next.layers) {
			t.Fatalf("layers = %v, want %v", next.layers, first.layers)
		}
		if !slices.Equal(first.pos, next.pos) {
			t.Fatalf("pos = %v, want %v", next.pos, first.pos)
		}
	}
}
