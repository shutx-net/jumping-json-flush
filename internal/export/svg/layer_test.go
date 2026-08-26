package svg

import (
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

func TestAssignRanksSingleNode(t *testing.T) {
	tests := []struct {
		name string
		doc  *model.Document
	}{
		{"a table with no relationship at all", document(linked("lonely"))},
		// A self-reference joins its table to nothing, so this is still a
		// component of one - and the shortcut has to hold for it, because a
		// self-loop is exactly what the solver must never be handed.
		{"a table that references only itself", document(linked("categories", "categories"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)
			if len(g.components) != 1 || len(g.components[0]) != 1 {
				t.Fatalf("components = %v, want one component of one node", g.components)
			}
			if got := assignRanks(g, g.components[0]); !slices.Equal(got, []int{0}) {
				t.Errorf("ranks = %v, want [0]", got)
			}
		})
	}
}

// TestAssignRanksDocumentShape is the foreign-key shape of
// internal/export/svg/testdata/full.json: customer_profiles and orders both
// reference customers, orders also references coupons, and order_lines
// references orders.
//
// The expected ranks were derived by hand before the first run. Writing the
// objective out gives 2*rank(customers) - rank(customer_profiles) -
// rank(orders) + rank(coupons) - rank(order_lines), and minimising it makes
// every one of the four relationships exactly one rank long, which is the
// floor for four edges, so the ranking is the unique optimum and not one of
// several ties.
//
// Three layers, which agrees with the layer count the discussion in issue #32
// reports for this document - measured there on a prototype that is not in
// this tree, so it is a corroboration and not a source.
func TestAssignRanksDocumentShape(t *testing.T) {
	g := buildGraph(document(
		linked("customers"),
		linked("customer_profiles", "customers"),
		linked("orders", "customers", "coupons"),
		linked("order_lines", "orders"),
		linked("coupons"),
	))
	if len(g.components) != 1 {
		t.Fatalf("components = %v, want one", g.components)
	}

	// customers 2, customer_profiles 1, orders 1, order_lines 0, coupons 2.
	// order_lines is leftmost and the two tables nothing references are
	// rightmost, which is the direction assignRanks fixes - see the
	// rank-direction paragraph in layer.go.
	want := []int{2, 1, 1, 0, 2}
	if got := assignRanks(g, g.components[0]); !slices.Equal(got, want) {
		t.Errorf("ranks = %v, want %v", got, want)
	}
}

// TestAssignRanksChildIsLowerThanParent is the property that decides which
// side of the diagram the parent of a relationship is drawn on. Nothing else
// is allowed: a relationship the cycle breaker left alone runs from a lower
// rank to a higher one, and one it reversed runs the other way, which is what
// makes the reversal visible in the ranks and nowhere else.
func TestAssignRanksChildIsLowerThanParent(t *testing.T) {
	tests := []struct {
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
		{"a two-table cycle", document(linked("a", "b"), linked("b", "a"))},
		{"a three-table cycle with a tail", document(
			linked("a", "b"),
			linked("b", "c"),
			linked("c", "a"),
			linked("d", "b"),
		)},
		{"two components, one of them a self-reference", document(
			linked("categories", "categories"),
			linked("orders", "customers"),
		)},
		{"parallel relationships between one pair", document(
			linked("nodes"),
			model.Table{
				Name: "edges", LogicalName: "edges",
				Columns:    []model.Column{bigint("id"), nullableBigint("from_node"), nullableBigint("to_node")},
				PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
				ForeignKeys: []model.ForeignKey{
					fk("fk_edges_from", "from_node", "nodes"),
					fk("fk_edges_to", "to_node", "nodes"),
				},
			},
		)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)
			for _, component := range g.components {
				reversed := breakCycles(g, component)
				ranks := assignRanks(g, component)

				low := ranks[component[0]]
				for _, v := range component {
					low = min(low, ranks[v])
				}
				if low != 0 {
					t.Errorf("component %v starts at rank %d, want 0", component, low)
				}

				for _, i := range g.layerEdges() {
					e := &g.edges[i]
					if !slices.Contains(component, e.from) {
						continue
					}
					if ranks[e.from] >= ranks[e.to] {
						t.Errorf("edge %d layers rank %d -> %d: every edge must point forward once the cycles are broken",
							i, ranks[e.from], ranks[e.to])
					}
					child, parent := ranks[e.child], ranks[e.parent]
					if slices.Contains(reversed, i) {
						if parent >= child {
							t.Errorf("reversed edge %d has parent at rank %d and child at rank %d, want the parent lower",
								i, parent, child)
						}
						continue
					}
					if child >= parent {
						t.Errorf("edge %d has child at rank %d and parent at rank %d, want the child lower",
							i, child, parent)
					}
				}
			}
		})
	}
}

// TestAssignRanksIsDeterministic is the two-runs-agree assertion for this
// stage. It runs the whole of it - cycle breaking included - because the two
// are one answer: which edge was reversed decides the ranks.
func TestAssignRanksIsDeterministic(t *testing.T) {
	doc := document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c", "a", "c"),
		linked("d", "b", "gone"),
	)

	var first [][]int
	for run := range 4 {
		g := buildGraph(doc)
		var ranks [][]int
		for _, component := range g.components {
			breakCycles(g, component)
			ranks = append(ranks, assignRanks(g, component))
		}
		if run == 0 {
			first = ranks
			continue
		}
		for i := range ranks {
			if !slices.Equal(ranks[i], first[i]) {
				t.Fatalf("run %d ranked component %d %v, want %v", run, i, ranks[i], first[i])
			}
		}
	}
}
