package svg

import (
	"reflect"
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// The documents below are Go literals rather than fixtures: what is being
// asserted is the shape of the graph, and a literal keeps the shape and the
// assertion next to each other. The fixtures under testdata/ exist for the
// rendered output, which is a different question.

// document wraps tables in the smallest document that carries them.
func document(tables ...model.Table) *model.Document {
	return &model.Document{
		FormatVersion: model.CurrentFormatVersion,
		Database:      model.Database{Name: "shop", LogicalName: "店"},
		Tables:        tables,
	}
}

// bigint is a NOT NULL BIGINT column, and nullableBigint the same column made
// nullable - which is the one thing that decides whether the parent end of a
// relationship over it is optional.
func bigint(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "BIGINT"}
}

func nullableBigint(name string) model.Column {
	return model.Column{Name: name, LogicalName: name, Type: "BIGINT", Nullable: true}
}

// fk builds a foreign key on one column pointing at target's id.
func fk(name, column, target string) model.ForeignKey {
	return model.ForeignKey{
		Name:       name,
		Columns:    []string{column},
		References: model.Reference{Table: target, Columns: []string{"id"}},
	}
}

// nodeNames lists the graph's nodes in index order, which is the order every
// assertion about ordering below is written against.
func nodeNames(g *graph) []string {
	names := make([]string, len(g.nodes))
	for i := range g.nodes {
		names[i] = g.nodes[i].name
	}
	return names
}

func TestBuildGraphNodeOrder(t *testing.T) {
	// Deliberately not alphabetical: document order is the contract, and a
	// sorted fixture could not tell the two apart.
	g := buildGraph(document(
		model.Table{Name: "orders", LogicalName: "受注", Columns: []model.Column{bigint("id")}},
		model.Table{Name: "customers", LogicalName: "顧客", Columns: []model.Column{bigint("id")}},
		model.Table{Name: "audit_log", LogicalName: "監査", Columns: []model.Column{bigint("id")}},
	))

	want := []string{"orders", "customers", "audit_log"}
	if got := nodeNames(g); !slices.Equal(got, want) {
		t.Errorf("nodes = %q, want %q in document order", got, want)
	}
	for i := range g.nodes {
		if g.nodes[i].kind != kindTable {
			t.Errorf("nodes[%d] (%s) has kind %d, want kindTable", i, g.nodes[i].name, g.nodes[i].kind)
		}
		if g.nodes[i].table == nil {
			t.Errorf("nodes[%d] (%s) has no table", i, g.nodes[i].name)
		}
	}
}

func TestBuildGraphStubsAfterTables(t *testing.T) {
	// zeta is named before alpha, so first-reference order and alphabetical
	// order disagree - which is the only way to see which one is in force.
	// missing is named three times and must produce one stub.
	g := buildGraph(document(
		model.Table{
			Name: "a", LogicalName: "A",
			Columns:     []model.Column{bigint("id"), nullableBigint("zeta_id"), nullableBigint("missing_id")},
			ForeignKeys: []model.ForeignKey{fk("fk_a_zeta", "zeta_id", "zeta"), fk("fk_a_missing", "missing_id", "missing")},
		},
		model.Table{
			Name: "b", LogicalName: "B",
			Columns:     []model.Column{bigint("id"), nullableBigint("alpha_id"), nullableBigint("missing_id")},
			ForeignKeys: []model.ForeignKey{fk("fk_b_alpha", "alpha_id", "alpha"), fk("fk_b_missing", "missing_id", "missing")},
		},
		model.Table{
			Name: "c", LogicalName: "C",
			Columns:     []model.Column{bigint("id"), nullableBigint("missing_id")},
			ForeignKeys: []model.ForeignKey{fk("fk_c_missing", "missing_id", "missing")},
		},
	))

	want := []string{"a", "b", "c", "zeta", "missing", "alpha"}
	if got := nodeNames(g); !slices.Equal(got, want) {
		t.Fatalf("nodes = %q, want %q: tables in document order, then stubs in first-reference order", got, want)
	}
	for i, n := range g.nodes {
		wantKind := kindTable
		if i >= 3 {
			wantKind = kindStub
		}
		if n.kind != wantKind {
			t.Errorf("nodes[%d] (%s) has kind %d, want %d", i, n.name, n.kind, wantKind)
		}
		// A stub knows a name and nothing else. It must not have invented a
		// table to stand for the one the document does not describe.
		if n.kind == kindStub && n.table != nil {
			t.Errorf("stub %s carries a table, want nil", n.name)
		}
	}

	// Every reference to missing points at the one stub.
	for _, e := range g.edges {
		if g.nodes[e.parent].name == "missing" && e.parent != 4 {
			t.Errorf("an edge points at node %d for missing, want the single stub at 4", e.parent)
		}
	}
}

func TestBuildGraphOneEdgePerForeignKey(t *testing.T) {
	parent := model.Table{
		Name: "parents", LogicalName: "親",
		Columns:    []model.Column{bigint("a"), bigint("b"), bigint("c")},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"a", "b", "c"}},
	}
	// One composite foreign key over three columns, then two separate keys
	// between the same pair of tables.
	child := model.Table{
		Name: "children", LogicalName: "子",
		Columns: []model.Column{nullableBigint("a"), nullableBigint("b"), nullableBigint("c")},
		ForeignKeys: []model.ForeignKey{
			{
				Name:       "fk_children_composite",
				Columns:    []string{"a", "b", "c"},
				References: model.Reference{Table: "parents", Columns: []string{"a", "b", "c"}},
			},
			fk("fk_children_first", "a", "parents"),
			{
				Columns:    []string{"b"},
				References: model.Reference{Table: "parents", Columns: []string{"a"}},
			},
		},
	}

	g := buildGraph(document(parent, child))

	if len(g.edges) != 3 {
		t.Fatalf("built %d edge(s), want 3: one per foreign key, whatever its column count", len(g.edges))
	}
	for i, e := range g.edges {
		if e.child != 1 || e.parent != 0 {
			t.Errorf("edges[%d] runs %d -> %d, want child 1 -> parent 0", i, e.child, e.parent)
		}
	}

	// Two parallel edges are told apart by their labels and by nothing else,
	// which is why an unnamed foreign key falls back to its column list rather
	// than to the empty string.
	labels := []string{g.edges[0].label, g.edges[1].label, g.edges[2].label}
	want := []string{"fk_children_composite", "fk_children_first", "b"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels = %q, want %q", labels, want)
	}
}

// TestBuildGraphEdgeEnds asserts the wiring to erd, not the derivation: erd's
// own tests pin the rules, and what matters here is that both ends are asked
// for, in the right order, about the child table.
func TestBuildGraphEdgeEnds(t *testing.T) {
	// A plain nullable foreign key column, constrained by nothing: many
	// children, and a child may point at no parent.
	plain := model.Table{
		Name: "orders", LogicalName: "受注",
		Columns:     []model.Column{bigint("id"), nullableBigint("customer_id")},
		PrimaryKey:  &model.PrimaryKey{Columns: []string{"id"}},
		ForeignKeys: []model.ForeignKey{fk("fk_orders_customer", "customer_id", "customers")},
	}
	// The foreign key IS the primary key and is NOT NULL: one child at most,
	// and it always points at a parent.
	oneToOne := model.Table{
		Name: "order_details", LogicalName: "受注詳細",
		Columns:     []model.Column{bigint("order_id")},
		PrimaryKey:  &model.PrimaryKey{Columns: []string{"order_id"}},
		ForeignKeys: []model.ForeignKey{fk("fk_details_order", "order_id", "orders")},
	}

	g := buildGraph(document(plain, oneToOne))

	tests := []struct {
		name          string
		edge          int
		child, parent erd.End
	}{
		{"a plain nullable foreign key", 0, erd.End{Many: true, Optional: true}, erd.End{Many: false, Optional: true}},
		{"a NOT NULL foreign key that is the primary key", 1, erd.End{Many: false, Optional: true}, erd.End{Many: false, Optional: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.edges[tt.edge].childEnd; got != tt.child {
				t.Errorf("childEnd = %+v, want %+v", got, tt.child)
			}
			if got := g.edges[tt.edge].parentEnd; got != tt.parent {
				t.Errorf("parentEnd = %+v, want %+v", got, tt.parent)
			}
		})
	}
}

func TestExtractLoops(t *testing.T) {
	categories := model.Table{
		Name: "categories", LogicalName: "分類",
		Columns:     []model.Column{bigint("id"), nullableBigint("parent_id")},
		PrimaryKey:  &model.PrimaryKey{Columns: []string{"id"}},
		ForeignKeys: []model.ForeignKey{fk("fk_categories_parent", "parent_id", "categories")},
	}
	twice := categories
	twice.Columns = append(slices.Clone(categories.Columns), nullableBigint("root_id"))
	twice.ForeignKeys = append(slices.Clone(categories.ForeignKeys), fk("fk_categories_root", "root_id", "categories"))

	flat := model.Table{
		Name: "orders", LogicalName: "受注",
		Columns:     []model.Column{bigint("id"), nullableBigint("customer_id")},
		ForeignKeys: []model.ForeignKey{fk("fk_orders_customer", "customer_id", "customers")},
	}

	tests := []struct {
		name  string
		doc   *model.Document
		loops []int
		layer []int
	}{
		{"one self-reference", document(categories), []int{0}, nil},
		{"two self-references on one table", document(twice), []int{0, 1}, nil},
		{"none at all", document(flat), nil, []int{0}},
		{"a self-reference beside an ordinary edge", document(categories, flat), []int{0}, []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildGraph(tt.doc)

			if !slices.Equal(g.loops, tt.loops) {
				t.Errorf("loops = %v, want %v", g.loops, tt.loops)
			}
			// A self-loop is out of what layering sees and still present as a
			// relationship: every foreign key is an edge, always.
			if got := g.layerEdges(); !slices.Equal(got, tt.layer) {
				t.Errorf("layerEdges() = %v, want %v", got, tt.layer)
			}
			if len(g.edges) != len(tt.loops)+len(tt.layer) {
				t.Errorf("%d edge(s) against %d loop(s) and %d layered: every foreign key must still be an edge",
					len(g.edges), len(tt.loops), len(tt.layer))
			}
			for _, i := range g.loops {
				if g.edges[i].label == "" {
					t.Errorf("loops[%d] lost its label: a self-reference is still drawn, and labelled", i)
				}
			}
		})
	}
}

func TestFindComponentsSingletons(t *testing.T) {
	g := buildGraph(document(
		model.Table{Name: "a", LogicalName: "A", Columns: []model.Column{bigint("id")}},
		model.Table{Name: "b", LogicalName: "B", Columns: []model.Column{bigint("id")}},
		model.Table{Name: "c", LogicalName: "C", Columns: []model.Column{bigint("id")}},
	))

	want := [][]int{{0}, {1}, {2}}
	if !reflect.DeepEqual(g.components, want) {
		t.Errorf("components = %v, want %v: one per table, in document order", g.components, want)
	}
}

// TestFindComponentsConnected uses the shape of the edge fixture, which is the
// one that has everything at once: a stub two tables share, two parallel
// edges, a composite key and a self-reference.
func TestFindComponentsConnected(t *testing.T) {
	g := buildGraph(document(
		model.Table{Name: "graph", LogicalName: "グラフ", Columns: []model.Column{bigint("id")}},
		model.Table{
			Name: "edge", LogicalName: "辺",
			Columns: []model.Column{
				bigint("graph_id"), bigint("seq"), nullableBigint("from_node"), bigint("to_node"),
			},
			PrimaryKey: &model.PrimaryKey{Columns: []string{"graph_id", "seq"}},
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
		model.Table{
			Name: "categories", LogicalName: "分類",
			Columns:     []model.Column{bigint("id"), nullableBigint("parent_id")},
			PrimaryKey:  &model.PrimaryKey{Columns: []string{"id"}},
			ForeignKeys: []model.ForeignKey{fk("fk_categories_parent", "parent_id", "categories")},
		},
		model.Table{
			Name: "node_stats", LogicalName: "ノード統計",
			Columns:    []model.Column{bigint("id"), bigint("node_id"), nullableBigint("graph_id")},
			PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
			ForeignKeys: []model.ForeignKey{
				fk("fk_node_stats_node", "node_id", "nodes"),
				fk("fk_node_stats_graph", "graph_id", "graph"),
			},
		},
	))

	// 0 graph, 1 edge, 2 edge_labels, 3 categories, 4 node_stats, 5 the stub
	// for nodes. categories is alone: its self-reference joins it to nothing,
	// and it must not be dropped for having no other edge.
	want := [][]int{{0, 1, 2, 4, 5}, {3}}
	if !reflect.DeepEqual(g.components, want) {
		t.Errorf("components = %v, want %v", g.components, want)
	}

	// Every node belongs to exactly one component.
	seen := make([]int, len(g.nodes))
	for _, component := range g.components {
		for _, n := range component {
			seen[n]++
		}
	}
	for i, count := range seen {
		if count != 1 {
			t.Errorf("node %d (%s) is in %d component(s), want 1", i, g.nodes[i].name, count)
		}
	}
}

// TestFindComponentsIsDeterministic is the two-runs-agree assertion. There is
// no output to compare yet, so the graph itself is what is compared - and it
// is the value every later stage is a function of.
func TestFindComponentsIsDeterministic(t *testing.T) {
	doc := document(
		model.Table{
			Name: "a", LogicalName: "A",
			Columns:     []model.Column{bigint("id"), nullableBigint("b_id"), nullableBigint("gone_id")},
			ForeignKeys: []model.ForeignKey{fk("fk_a_b", "b_id", "b"), fk("fk_a_gone", "gone_id", "gone")},
		},
		model.Table{Name: "b", LogicalName: "B", Columns: []model.Column{bigint("id")}},
		model.Table{Name: "lonely", LogicalName: "孤立", Columns: []model.Column{bigint("id")}},
	)

	first, second := buildGraph(doc), buildGraph(doc)
	if !reflect.DeepEqual(first, second) {
		t.Error("two builds of one document produced different graphs")
	}
}

func TestContentAttachedToEveryNode(t *testing.T) {
	g := buildGraph(document(
		model.Table{
			Name: "orders", LogicalName: "受注",
			Columns:     []model.Column{bigint("id"), nullableBigint("customer_id")},
			ForeignKeys: []model.ForeignKey{fk("fk_orders_customer", "customer_id", "customers")},
		},
	))

	if len(g.nodes) != 2 {
		t.Fatalf("built %d node(s), want 2: the table and the stub for customers", len(g.nodes))
	}
	for i, n := range g.nodes {
		if n.content == nil {
			t.Fatalf("nodes[%d] (%s) has no content: nothing downstream can size it", i, n.name)
		}
		if n.content.width <= 0 || n.content.height <= 0 {
			t.Errorf("nodes[%d] (%s) measured %dx%d, want a positive size", i, n.name, n.content.width, n.content.height)
		}
	}
	// The stub's box is its two lines and nothing else.
	if got := g.nodes[1].content.headerLines; got != [2]string{"customers", stubNote} {
		t.Errorf("the stub's header is %q, want the table name and the note", got)
	}
}
