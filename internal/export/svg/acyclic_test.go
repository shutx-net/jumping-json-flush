package svg

import (
	"reflect"
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// linked builds a table with an id primary key and one nullable foreign key
// column per target, in the order given, so that a document's edge list is the
// order the targets are written in.
//
// A target equal to the table's own name is a self-reference, which is how
// these tests reach the one edge layering never sees.
func linked(name string, targets ...string) model.Table {
	t := model.Table{
		Name:        name,
		LogicalName: name,
		Columns:     []model.Column{bigint("id")},
		PrimaryKey:  &model.PrimaryKey{Columns: []string{"id"}},
	}
	for _, target := range targets {
		column := target + "_id"
		t.Columns = append(t.Columns, nullableBigint(column))
		t.ForeignKeys = append(t.ForeignKeys, fk("fk_"+name+"_"+target, column, target))
	}
	return t
}

// layeringIsAcyclic reports whether the from/to direction of every edge
// layering sees has no cycle left in it, by counting how many nodes a
// topological walk can finish. A node on a cycle never reaches in-degree zero,
// so the count falls short exactly when a cycle remains.
func layeringIsAcyclic(g *graph) bool {
	out := make([][]int, len(g.nodes))
	pending := make([]int, len(g.nodes))
	for _, i := range g.layerEdges() {
		out[g.edges[i].from] = append(out[g.edges[i].from], i)
		pending[g.edges[i].to]++
	}

	queue := make([]int, 0, len(g.nodes))
	for v := range g.nodes {
		if pending[v] == 0 {
			queue = append(queue, v)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, i := range out[queue[head]] {
			pending[g.edges[i].to]--
			if pending[g.edges[i].to] == 0 {
				queue = append(queue, g.edges[i].to)
			}
		}
	}
	return len(queue) == len(g.nodes)
}

// relationshipEnds lists every edge's child and parent, which is the pair
// breakCycles must not touch.
func relationshipEnds(g *graph) [][2]int {
	ends := make([][2]int, len(g.edges))
	for i := range g.edges {
		ends[i] = [2]int{g.edges[i].child, g.edges[i].parent}
	}
	return ends
}

// TestBreakCyclesTwoNodeCycle is the case NEITHER existing fixture has: two
// distinct tables each holding a foreign key to the other.
func TestBreakCyclesTwoNodeCycle(t *testing.T) {
	g := buildGraph(document(linked("a", "b"), linked("b", "a")))
	before := relationshipEnds(g)

	// Node 0 is a and node 1 is b; edge 0 is a -> b and edge 1 is b -> a. The
	// walk starts at a, steps along edge 0 to b, and finds a still on the
	// stack, so edge 1 is the back edge.
	reversed := breakCycles(g, g.components[0])
	if !slices.Equal(reversed, []int{1}) {
		t.Fatalf("reversed %v, want exactly edge 1, the b -> a edge the walk meets second", reversed)
	}
	if !layeringIsAcyclic(g) {
		t.Error("a cycle is left after breaking one two-node cycle")
	}

	// The layering direction flipped and nothing else did.
	if g.edges[1].from != 0 || g.edges[1].to != 1 {
		t.Errorf("edge 1 layers %d -> %d, want 0 -> 1", g.edges[1].from, g.edges[1].to)
	}
	if got := relationshipEnds(g); !reflect.DeepEqual(got, before) {
		t.Errorf("child/parent = %v, want %v: reversing an edge must not move its crow's foot", got, before)
	}
	if g.edges[1].child != 1 || g.edges[1].parent != 0 {
		t.Errorf("the reversed edge is child %d -> parent %d, want 1 -> 0: b declares the foreign key",
			g.edges[1].child, g.edges[1].parent)
	}
}

func TestBreakCyclesThreeNodeCycle(t *testing.T) {
	g := buildGraph(document(linked("a", "b"), linked("b", "c"), linked("c", "a")))

	// Edges are 0: a -> b, 1: b -> c, 2: c -> a. The walk roots at a in
	// document order and descends a, b, c, so the edge it reaches last is
	// c -> a, and a is still on the stack when it gets there.
	reversed := breakCycles(g, g.components[0])
	if !slices.Equal(reversed, []int{2}) {
		t.Fatalf("reversed %v, want exactly edge 2, the c -> a edge", reversed)
	}
	if !layeringIsAcyclic(g) {
		t.Error("a cycle is left after breaking one three-node cycle")
	}
	if g.edges[2].from != 0 || g.edges[2].to != 2 {
		t.Errorf("edge 2 layers %d -> %d, want 0 -> 2", g.edges[2].from, g.edges[2].to)
	}
}

func TestBreakCyclesAcyclicIsUntouched(t *testing.T) {
	g := buildGraph(document(linked("a", "b"), linked("b", "c"), linked("c")))

	if reversed := breakCycles(g, g.components[0]); len(reversed) != 0 {
		t.Errorf("reversed %v on a chain a -> b -> c, want nothing", reversed)
	}
	for i := range g.edges {
		if g.edges[i].from != g.edges[i].child || g.edges[i].to != g.edges[i].parent {
			t.Errorf("edge %d layers %d -> %d against child %d -> parent %d: an acyclic component keeps every direction",
				i, g.edges[i].from, g.edges[i].to, g.edges[i].child, g.edges[i].parent)
		}
	}
}

// TestBreakCyclesSelfLoopIsNotSeen pins the boundary with the stage before
// this one: a self-reference is out of the edge list layering walks, so cycle
// breaking must not find a cycle in a table that references itself.
func TestBreakCyclesSelfLoopIsNotSeen(t *testing.T) {
	g := buildGraph(document(linked("categories", "categories")))

	if reversed := breakCycles(g, g.components[0]); len(reversed) != 0 {
		t.Errorf("reversed %v on a self-reference, want nothing: it is not layering's edge to break", reversed)
	}
	if g.edges[0].from != g.edges[0].to {
		t.Error("the self-reference lost its shape")
	}
}

func TestBreakCyclesIsDeterministic(t *testing.T) {
	// Two cycles at once, one of them entered from two different roots, plus a
	// self-reference and a table pointing at one the document does not define -
	// so the walk has several places it could differ if anything about it
	// depended on iteration order.
	doc := document(
		linked("a", "b", "gone"),
		linked("b", "c"),
		linked("c", "a", "c"),
		linked("d", "b"),
	)

	first, second := buildGraph(doc), buildGraph(doc)
	firstReversed := breakCycles(first, first.components[0])
	secondReversed := breakCycles(second, second.components[0])

	if !slices.Equal(firstReversed, secondReversed) {
		t.Errorf("two runs reversed %v and %v", firstReversed, secondReversed)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two runs left different graphs")
	}
	if !layeringIsAcyclic(first) {
		t.Errorf("a cycle is left after reversing %v", firstReversed)
	}
}

func TestBreakCyclesKeepsRelationshipEnds(t *testing.T) {
	doc := document(
		linked("a", "b"),
		linked("b", "c"),
		linked("c", "a"),
	)
	g := buildGraph(doc)
	want := relationshipEnds(buildGraph(doc))

	reversed := breakCycles(g, g.components[0])
	if len(reversed) == 0 {
		t.Fatal("nothing was reversed, so the assertion below proves nothing")
	}
	if got := relationshipEnds(g); !reflect.DeepEqual(got, want) {
		t.Errorf("child/parent = %v, want %v", got, want)
	}
	for _, i := range reversed {
		e := &g.edges[i]
		if e.from == e.child || e.to == e.parent {
			t.Errorf("edge %d layers %d -> %d and relates child %d to parent %d: a reversed edge's layering direction is the one that flips, and its relationship is the one that does not",
				i, e.from, e.to, e.child, e.parent)
		}
	}
}
