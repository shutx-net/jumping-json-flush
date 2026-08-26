package svg

// ---------------------------------------------------------------------------
// Cycle breaking
// ---------------------------------------------------------------------------

// breakCycles makes one component acyclic by reversing the back edges a
// depth-first walk finds, and returns the indices of the edges it reversed, in
// the order it reversed them.
//
// Depth-first back-edge reversal rather than strongly connected components
// plus a greedy feedback-arc set: the two differ only in HOW MANY edges get
// reversed, both leave an acyclic graph, and at the size of a database schema
// - where a cycle at all is unusual and a cycle of more than three tables is
// rarer still - the extra machinery buys a smaller reversal set that nobody
// can see in the drawing. The issue that proposed the feedback-arc set
// withdrew the objection on exactly that ground.
//
// It is deterministic, and that is a property of the traversal order rather
// than of the algorithm: roots are the component's nodes in index order, which
// is document order, and each node's outgoing edges are visited in edge index
// order, which is table order and then foreign-key order within the table. No
// map is ranged over and nothing is seeded from anywhere.
//
// # A reversed edge keeps its true child and parent
//
// This function writes edge.from and edge.to and NOTHING else. from/to is the
// layering direction, and flipping it is the whole point; child/parent is the
// relationship the document states, and flipping that would move the crow's
// foot to the other end of the line.
//
// That mistake has no symptom. Not one geometry invariant would notice: the
// drawing stays perfectly legal - every segment axis-aligned, every endpoint
// on a boundary, nothing overlapping anything - and the picture simply
// describes a database in which the foreign key points the other way. The
// invariant suite cannot catch it and the golden files cannot catch it,
// because both would be internally consistent. This comment and the test that
// pins both ends are the whole of the defence.
func breakCycles(g *graph, component []int) []int {
	// out[v] lists the edges leaving v, in edge index order, as they stand
	// BEFORE any reversal. It is a snapshot on purpose: reversing an edge is
	// also the instruction not to traverse it, and leaving the snapshot alone
	// is what implements that. Re-filing a reversed edge under its new tail
	// would offer it to the walk a second time, from the ancestor it now
	// leaves, and the walk would find its other end still on the stack and
	// reverse it back.
	out := make([][]int, len(g.nodes))
	for _, i := range g.layerEdges() {
		out[g.edges[i].from] = append(out[g.edges[i].from], i)
	}

	// The three colours of a depth-first search. A node still on the stack is
	// an ancestor of the node being visited, so an edge reaching one closes a
	// cycle; a finished node is not, so an edge reaching one is a forward or
	// cross edge and closes nothing.
	const (
		unvisited = iota
		onStack
		finished
	)
	state := make([]int8, len(g.nodes))

	// An explicit stack rather than recursion. A schema deep enough to
	// overflow the goroutine stack is not a document anyone would write, but
	// the depth here is the size of a component and nothing in the format
	// bounds that, so the recursion depth would be a property of the input
	// rather than of the code.
	type frame struct {
		node int
		next int // how many of out[node] have been offered to the walk
	}
	var stack []frame
	var reversed []int

	for _, root := range component {
		if state[root] != unvisited {
			continue
		}
		state[root] = onStack
		stack = append(stack, frame{node: root})

		for len(stack) > 0 {
			top := len(stack) - 1
			v := stack[top].node
			if stack[top].next == len(out[v]) {
				state[v] = finished
				stack = stack[:top]
				continue
			}
			i := out[v][stack[top].next]
			stack[top].next++

			switch state[g.edges[i].to] {
			case onStack:
				g.edges[i].from, g.edges[i].to = g.edges[i].to, g.edges[i].from
				reversed = append(reversed, i)
			case unvisited:
				state[g.edges[i].to] = onStack
				stack = append(stack, frame{node: g.edges[i].to})
			}
		}
	}

	return reversed
}
