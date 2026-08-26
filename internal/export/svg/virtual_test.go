package svg

import (
	"reflect"
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// laidOut runs the stages before this one on doc and then this one, for the
// component holding the first table, and hands back everything the assertions
// need. Every test below starts here, because rank doubling and virtual
// insertion are only meaningful on top of a real ranking.
type laidOut struct {
	g       *graph
	members []int
	ranks   []int
	chains  []chain
}

func layOut(doc *model.Document) laidOut {
	g := buildGraph(doc)
	component := g.components[0]
	breakCycles(g, component)
	ranks := assignRanks(g, component)
	members, ranks, chains := insertVirtualNodes(g, component, ranks)
	return laidOut{g: g, members: members, ranks: ranks, chains: chains}
}

// chainOf returns the chain recorded for edge i, or nil when it has none.
func (l laidOut) chainOf(i int) []int {
	for _, c := range l.chains {
		if c.edge == i {
			return c.nodes
		}
	}
	return nil
}

// spanDocuments is the document set the properties below are asserted over:
// full.json's and edge.json's foreign-key shapes, a cycle between two distinct
// tables, a relationship crossing more than one rank, and a self-reference
// beside an ordinary relationship.
func spanDocuments() []struct {
	name string
	doc  *model.Document
} {
	return []struct {
		name string
		doc  *model.Document
	}{
		{"the full fixture's shape", document(
			linked("customers"),
			linked("customer_profiles", "customers"),
			linked("orders", "customers", "coupons"),
			linked("order_lines", "orders"),
			linked("coupons"),
		)},
		{"the edge fixture's shape", document(
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
			linked("categories", "categories"),
		)},
		{"a two-table cycle", document(linked("a", "b"), linked("b", "a"))},
		{"a relationship spanning two ranks", document(
			linked("a", "b", "c"),
			linked("b", "c"),
			linked("c"),
		)},
		{"a three-table cycle with a tail", document(
			linked("a", "b"),
			linked("b", "c"),
			linked("c", "a"),
			linked("d", "b"),
		)},
	}
}

// TestDoubleRanksSpanIsNeverOne is the assertion the whole routing taxonomy
// rests on. If it ever fails, the taxonomy is wrong and not the test: some
// relationship would have exactly one half-rank between its ends, and neither
// a label node nor a plain virtual node could go there.
func TestDoubleRanksSpanIsNeverOne(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)
			for _, component := range g.components {
				breakCycles(g, component)
				ranks := assignRanks(g, component)
				doubleRanks(ranks)

				for i := range g.edges {
					e := &g.edges[i]
					if !slices.Contains(component, e.from) {
						continue
					}
					span := ranks[e.to] - ranks[e.from]
					if span < 0 {
						span = -span
					}
					if span == 1 || span%2 != 0 {
						t.Errorf("edge %d spans %d half-ranks, want 0 or an even number of at least 2", i, span)
					}
				}
			}
		})
	}
}

func TestClassifyIsExhaustive(t *testing.T) {
	// The three documents are one per category. The same-rank one cannot come
	// out of layering - every relationship layering sees carries a minimum
	// length of one rank, so no two tables an edge joins can share a rank -
	// so its ranks are written by hand here. That is the honest way to cover
	// the branch: it exists for a state the ranking cannot currently produce,
	// and it is kept because a classification total over its own type cannot
	// quietly stop being one the day the ranking changes.
	tests := []struct {
		name  string
		doc   *model.Document
		ranks []int
		want  []routeKind
	}{
		{
			"a plain parent and child",
			document(linked("orders", "customers")),
			nil,
			[]routeKind{routeInterRank},
		},
		{
			"two tables sharing a rank",
			document(linked("a", "b"), linked("b")),
			[]int{0, 0},
			[]routeKind{routeSameRank},
		},
		{
			"a self-reference",
			document(linked("categories", "categories")),
			nil,
			[]routeKind{routeSelfLoop},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)
			ranks := tt.ranks
			if ranks == nil {
				ranks = assignRanks(g, g.components[0])
				doubleRanks(ranks)
			}
			for i := range g.edges {
				got := classify(g, ranks, i)
				if got != tt.want[i] {
					t.Errorf("classify(edge %d) = %d, want %d", i, got, tt.want[i])
				}
			}
		})
	}

	// Over every document in the set, every relationship classifies as one of
	// the three and nothing else. A fourth value would mean the doubling
	// argument had broken.
	for _, tt := range spanDocuments() {
		g := buildGraph(tt.doc)
		for _, component := range g.components {
			breakCycles(g, component)
			ranks := assignRanks(g, component)
			doubleRanks(ranks)
			for i := range g.edges {
				if !slices.Contains(component, g.edges[i].from) {
					continue
				}
				switch classify(g, ranks, i) {
				case routeInterRank, routeSameRank, routeSelfLoop:
				default:
					t.Errorf("%s: edge %d classified as %d, which is none of the three",
						tt.name, i, classify(g, ranks, i))
				}
			}
		}
	}
}

func TestLabelNodePerInterRankEdge(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l := layOut(tt.doc)

			labels := 0
			for i := range l.g.nodes {
				if l.g.nodes[i].kind == kindLabel {
					labels++
				}
			}

			want := 0
			for i := range l.g.edges {
				if !slices.Contains(l.members, l.g.edges[i].from) {
					continue
				}
				if classify(l.g, l.ranks, i) == routeInterRank {
					want++
				}
			}
			if labels != want {
				t.Errorf("%d label node(s) against %d ordinary relationship(s)", labels, want)
			}
			if len(l.chains) != want {
				t.Errorf("%d chain(s) against %d ordinary relationship(s)", len(l.chains), want)
			}
		})
	}
}

// TestLabelNodeBand asserts the arithmetic of the band, not a rendered result:
// the text's rectangle is the top of it, the route runs along its bottom edge,
// and the distance between the two is exactly one labelGap.
func TestLabelNodeBand(t *testing.T) {
	l := layOut(document(linked("orders", "customers")))

	nodes := l.chainOf(0)
	if len(nodes) != 1 {
		t.Fatalf("the chain is %v, want exactly one node: a span of two half-ranks has one half-rank between its ends", nodes)
	}
	n := &l.g.nodes[nodes[0]]
	if n.kind != kindLabel {
		t.Fatalf("the chain's only node has kind %d, want kindLabel", n.kind)
	}
	if n.label != "fk_orders_customers" {
		t.Errorf("label = %q, want the foreign key's name", n.label)
	}

	wantRect := Rect{W: measureText("fk_orders_customers") + 2*cellPadH, H: labelHeight}
	if n.labelRect != wantRect {
		t.Errorf("labelRect = %+v, want %+v", n.labelRect, wantRect)
	}

	w, h := n.size()
	if w != wantRect.W {
		t.Errorf("the band is %d wide, want the label rectangle's %d", w, wantRect.W)
	}
	if h != labelHeight+labelGap {
		t.Errorf("the band is %d tall, want labelHeight+labelGap = %d", h, labelHeight+labelGap)
	}
	// The route runs along the band's bottom edge, which is the node's own
	// bottom edge, so the text's bottom sits exactly one labelGap above it.
	if h-n.labelRect.Bottom() != labelGap {
		t.Errorf("the text's bottom is %d above the route line, want labelGap = %d", h-n.labelRect.Bottom(), labelGap)
	}
	// labelHeight is one line of text with a padding above and below, which is
	// the arithmetic a table's row uses, so the two are the same shape.
	if labelHeight != lineHeight+2*cellPadV {
		t.Errorf("labelHeight is %d, want lineHeight+2*cellPadV = %d", labelHeight, lineHeight+2*cellPadV)
	}
}

// TestChainRanksAreConsecutive uses the one shape neither full.json nor
// edge.json has: a relationship that still spans more than one rank after the
// simplex has tightened the ranking. a references both b and c, and b
// references c, so a is at rank 0, b at 1 and c at 2, and the a -> c
// relationship crosses four half-ranks once the ranks are doubled.
func TestChainRanksAreConsecutive(t *testing.T) {
	l := layOut(document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c"),
	))

	// Edge 0 is a -> b, edge 1 is a -> c, edge 2 is b -> c.
	if got := []int{l.ranks[0], l.ranks[1], l.ranks[2]}; !slices.Equal(got, []int{0, 2, 4}) {
		t.Fatalf("a, b, c are at half-ranks %v, want [0 2 4]", got)
	}

	nodes := l.chainOf(1)
	if len(nodes) != 3 {
		t.Fatalf("the chain for a -> c is %v, want three nodes: one label node and two plain ones", nodes)
	}
	for k, v := range nodes {
		if want := 1 + k; l.ranks[v] != want {
			t.Errorf("chain node %d is at half-rank %d, want %d", k, l.ranks[v], want)
		}
	}
	if l.g.nodes[nodes[0]].kind != kindLabel {
		t.Errorf("the chain starts with kind %d, want kindLabel: the label goes next to the child", l.g.nodes[nodes[0]].kind)
	}
	for _, v := range nodes[1:] {
		if l.g.nodes[v].kind != kindVirtual {
			t.Errorf("chain node %d has kind %d, want kindVirtual", v, l.g.nodes[v].kind)
		}
	}
}

func TestChainCoversEveryIntermediateRank(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			l := layOut(tt.doc)

			for _, c := range l.chains {
				e := &l.g.edges[c.edge]
				lo, hi := min(l.ranks[e.child], l.ranks[e.parent]), max(l.ranks[e.child], l.ranks[e.parent])

				if want := hi - lo - 1; len(c.nodes) != want {
					t.Errorf("edge %d spans %d..%d and has %d chain node(s), want %d",
						c.edge, lo, hi, len(c.nodes), want)
				}
				// Exactly one node of the chain in every half-rank strictly
				// between the ends, which is what reserves the corridor.
				seen := make(map[int]int, len(c.nodes))
				for _, v := range c.nodes {
					if l.ranks[v] <= lo || l.ranks[v] >= hi {
						t.Errorf("edge %d has a chain node at half-rank %d, outside %d..%d", c.edge, l.ranks[v], lo, hi)
					}
					seen[l.ranks[v]]++
				}
				for r := lo + 1; r < hi; r++ {
					if seen[r] != 1 {
						t.Errorf("edge %d has %d chain node(s) at half-rank %d, want 1", c.edge, seen[r], r)
					}
				}
				// The label node is the end of the chain nearest the child,
				// whichever direction the relationship runs in.
				if got := l.ranks[c.nodes[0]]; got != l.ranks[e.child]+halfRankStep(l.ranks, e) {
					t.Errorf("edge %d puts its label node at half-rank %d, want beside its child at %d",
						c.edge, got, l.ranks[e.child])
				}
			}
		})
	}
}

func TestNoChainForSpecialCategories(t *testing.T) {
	t.Run("a self-reference", func(t *testing.T) {
		l := layOut(document(linked("categories", "categories")))

		if len(l.chains) != 0 {
			t.Errorf("chains = %v, want none: rank doubling leaves a span-0 relationship no half-rank to put a label in", l.chains)
		}
		if len(l.members) != 1 || len(l.g.nodes) != 1 {
			t.Errorf("%d component member(s) and %d node(s), want 1 and 1", len(l.members), len(l.g.nodes))
		}
	})

	// Hand-built ranks again, for the state layering cannot reach: two tables
	// an edge joins, sharing a rank. The point of the case is that insertion
	// asks classify and not the span arithmetic, so a flat relationship gets
	// nothing even though its two ends are distinct nodes.
	t.Run("two tables sharing a rank", func(t *testing.T) {
		g := buildGraph(document(linked("a", "b"), linked("b")))
		members, ranks, chains := insertVirtualNodes(g, g.components[0], []int{0, 0})

		if len(chains) != 0 {
			t.Errorf("chains = %v, want none", chains)
		}
		if len(g.nodes) != 2 {
			t.Errorf("%d node(s), want the 2 the document asked for and no label node", len(g.nodes))
		}
		if !slices.Equal(members, []int{0, 1}) {
			t.Errorf("members = %v, want [0 1]", members)
		}
		if !slices.Equal(ranks, []int{0, 0}) {
			t.Errorf("ranks = %v, want [0 0] doubled from [0 0]", ranks)
		}
	})
}

func TestVirtualNodesAreZeroWidth(t *testing.T) {
	l := layOut(document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c"),
	))

	virtual := 0
	for i := range l.g.nodes {
		n := &l.g.nodes[i]
		w, h := n.size()
		switch n.kind {
		case kindVirtual:
			virtual++
			if n.content != nil {
				t.Errorf("node %d is virtual and carries content", i)
			}
			if w != 0 || h != 0 {
				t.Errorf("node %d is virtual and measures %dx%d, want 0x0: it holds a corridor open and reserves nothing", i, w, h)
			}
		case kindLabel:
			if w <= 0 || h <= 0 {
				t.Errorf("node %d is a label and measures %dx%d, want a positive size", i, w, h)
			}
		default:
			if w != n.content.width || h != n.content.height {
				t.Errorf("node %d measures %dx%d against content %dx%d", i, w, h, n.content.width, n.content.height)
			}
		}
	}
	if virtual != 2 {
		t.Errorf("%d virtual node(s), want 2: the a -> c relationship crosses two half-ranks that are not its label's", virtual)
	}
}

// TestVirtualInsertionIsDeterministic is the two-runs-agree assertion for this
// stage: the graph the run leaves behind, the component's members, the ranks
// and the chains all have to be the same value twice.
func TestVirtualInsertionIsDeterministic(t *testing.T) {
	doc := document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c", "a", "c"),
		linked("d", "b", "gone"),
	)

	first := layOut(doc)
	for range 4 {
		next := layOut(doc)
		if !reflect.DeepEqual(first.g, next.g) {
			t.Fatal("two runs left different graphs")
		}
		if !slices.Equal(first.members, next.members) {
			t.Fatalf("members = %v, want %v", next.members, first.members)
		}
		if !slices.Equal(first.ranks, next.ranks) {
			t.Fatalf("ranks = %v, want %v", next.ranks, first.ranks)
		}
		if !reflect.DeepEqual(first.chains, next.chains) {
			t.Fatalf("chains = %v, want %v", next.chains, first.chains)
		}
	}
}
