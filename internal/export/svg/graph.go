package svg

import (
	"slices"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// The layout graph
// ---------------------------------------------------------------------------

// nodeKind says what a node is drawn as, which is also what it is for.
type nodeKind int

// The four kinds. kindTable and kindStub are the boxes the document asks for;
// kindLabel and kindVirtual are placeholders later stages insert to reserve
// space, and are drawn as no ink at all - a label node reserves the band its
// text sits in, a virtual node reserves the corridor a long edge runs down.
const (
	kindTable nodeKind = iota
	kindStub
	kindLabel
	kindVirtual
)

// node is one box, or one placeholder standing in for one.
type node struct {
	kind nodeKind
	// table is the document's table for a kindTable node and nil for every
	// other kind; name is the physical name for both kinds of box, which is
	// the only thing a stub knows about the table it stands for.
	table *model.Table
	name  string
	// content is the measured interior: what is drawn inside the box and
	// where, plus the intrinsic size the box is laid out from.
	content *content
}

// edge is one relationship: one foreign key, whichever way the layout ends up
// pointing it.
//
// from/to and child/parent are two different questions and that is why they
// are two pairs of fields rather than one pair read twice. from/to is the
// LAYERING direction, and cycle breaking reverses it whenever a cycle has to
// be cut. child/parent is the RELATIONSHIP - which table declares the foreign
// key and which one it names - and nothing in this package may ever reverse
// it, because it is what the drawing reads: the crow's foot goes on the child
// end.
//
// Conflating them is the classic bug in the stage that follows. Reversing an
// edge for layering would move its crow's foot to the other end, and not one
// geometry invariant would notice, because the drawing stays perfectly legal:
// every segment still axis-aligned, every endpoint still on a boundary,
// nothing overlapping anything. It would simply describe a different database.
type edge struct {
	from, to      int
	child, parent int

	label     string
	childEnd  erd.End
	parentEnd erd.End
}

// graph is the whole document as the layout sees it.
//
// edges holds EVERY relationship, self-loops included, in document order, so
// that an edge's index is one stable identity for the whole pipeline - it is
// what routing breaks ties on and what makes "one drawn relationship per
// foreign key" an assertion about len(edges) rather than about a sum. loops
// names the self-referencing ones by index; layerEdges is everything else.
type graph struct {
	nodes      []node
	edges      []edge
	loops      []int
	components [][]int
}

// buildGraph turns a document into the graph the layout works on.
//
// The ordering contract, which every later stage inherits and none may break:
// table nodes in document order, then stub nodes in first-reference order, and
// each table's foreign keys in document order. That is what makes the output
// byte-for-byte reproducible, and it is why the name index below is only ever
// LOOKED UP and never ranged over - erd's definedTables gives the reason in
// full, and it is the same reason: ranging a Go map would hand the order of
// the nodes to the runtime's map iteration, and the golden files rest on that
// order.
//
// Text measurement happens here, once per node, rather than inside the
// per-component loop that lays each component out. A table's size does not
// depend on which component it is in - it is in exactly one - so measuring
// outside costs nothing and leaves that loop about layout alone.
func buildGraph(doc *model.Document) *graph {
	g := &graph{nodes: make([]node, 0, len(doc.Tables))}

	// index answers "which node is this table name", for the foreign key pass
	// below. A document may define one name twice - the schema does not forbid
	// it - and the FIRST definition wins, because document order is what every
	// other tie in this package is broken by and the first is the one a reader
	// meets.
	index := make(map[string]int, len(doc.Tables))

	for i := range doc.Tables {
		t := &doc.Tables[i]
		if _, seen := index[t.Name]; !seen {
			index[t.Name] = len(g.nodes)
		}
		g.nodes = append(g.nodes, node{
			kind:    kindTable,
			table:   t,
			name:    t.Name,
			content: measureTable(t),
		})
	}

	// A table a foreign key names and the document does not define gets a box
	// of its own, sized from the two lines it can honestly show. The stub
	// invents nothing else - no columns, no keys, no types - so the diagram
	// shows exactly what the JSON claims: that something points at a table
	// this document does not describe. It is a full participant from here on;
	// it is layered, ordered and attached to like any other box. Nothing is
	// checked and nothing is reported, ever: such a document is legal, and
	// "jjf validate" is where it is spoken about.
	for _, name := range erd.UndefinedTargets(doc) {
		index[name] = len(g.nodes)
		g.nodes = append(g.nodes, node{
			kind:    kindStub,
			name:    name,
			content: measureStub(name),
		})
	}

	// One edge per foreign key. A composite foreign key is ONE relationship
	// and gets one edge, never one per column pair; two foreign keys between
	// the same pair of tables are two edges, told apart by nothing but their
	// labels, which is why a label is not an optional refinement.
	//
	// The child node is the table's own index rather than a lookup of its
	// name, which is exact even when a name is defined twice.
	for i := range doc.Tables {
		t := &doc.Tables[i]
		for j := range t.ForeignKeys {
			fk := &t.ForeignKeys[j]
			target := index[fk.References.Table]
			g.edges = append(g.edges, edge{
				from:      i,
				to:        target,
				child:     i,
				parent:    target,
				label:     erd.EdgeLabel(fk),
				childEnd:  erd.ChildEnd(t, fk),
				parentEnd: erd.ParentEnd(t, fk),
			})
		}
	}

	g.extractLoops()
	g.findComponents()
	return g
}

// extractLoops records which relationships are self-references.
//
// They come out of the edge list layering sees, and the position of that is
// not arbitrary: layering assumes a graph with no self-loops - a loop has no
// direction to rank, and a rank difference of zero to itself is not a
// constraint anything can satisfy - and the routing taxonomy names them as one
// of its three categories, so the drawing has to be able to find them again.
//
// They are recorded as indices rather than deleted, because they are still
// relationships: each carries a label and two ends and gets drawn as a staple
// over the top of its box. Deleting them would also make the number of drawn
// relationships stop matching the number of foreign keys, which is one of the
// invariants.
func (g *graph) extractLoops() {
	for i := range g.edges {
		if g.edges[i].from == g.edges[i].to {
			g.loops = append(g.loops, i)
		}
	}
}

// layerEdges lists, in document order, the indices of the edges layering and
// ordering work on: every edge that is not a self-loop.
func (g *graph) layerEdges() []int {
	out := make([]int, 0, len(g.edges)-len(g.loops))
	for i := range g.edges {
		if g.edges[i].from != g.edges[i].to {
			out = append(out, i)
		}
	}
	return out
}

// findComponents splits the graph into the sets of nodes that are connected to
// each other, which is what gets laid out and then packed as one rigid block.
//
// Edges are UNDIRECTED here, and only here: everywhere else in the pipeline an
// edge points from the child to the table it references, but a component is a
// statement about what is drawn near what, and a reader asking whether two
// tables belong on the same shelf does not care which way the foreign key
// between them points. Self-loops are included and merge nothing, both of
// their ends being the same node.
//
// The walk is over the nodes in index order, so a component's lowest node
// index is the document-order position of its first table, and the components
// come back sorted by it. Each component's members are sorted too, so that
// every later pass that walks one walks it in document order. No map is
// involved at all.
func (g *graph) findComponents() {
	adjacent := make([][]int, len(g.nodes))
	for i := range g.edges {
		e := &g.edges[i]
		adjacent[e.from] = append(adjacent[e.from], e.to)
		adjacent[e.to] = append(adjacent[e.to], e.from)
	}

	visited := make([]bool, len(g.nodes))
	for start := range g.nodes {
		if visited[start] {
			continue
		}

		// Iterative rather than recursive: the depth of a recursion here would
		// be the size of the component, and nothing bounds how many tables one
		// document may connect.
		visited[start] = true
		component := []int{start}
		for queued := 0; queued < len(component); queued++ {
			for _, next := range adjacent[component[queued]] {
				if !visited[next] {
					visited[next] = true
					component = append(component, next)
				}
			}
		}

		slices.Sort(component)
		g.components = append(g.components, component)
	}
}
