package svg

import "slices"

// ---------------------------------------------------------------------------
// Layer assignment
// ---------------------------------------------------------------------------

// assignRanks gives every node of one component a rank, and returns the ranks
// indexed by NODE ID, with 0 in every entry that belongs to another component.
//
// Indexed by node id rather than by position within the component because the
// stages after this one add nodes to the graph - a label node for every
// relationship, a virtual node for every half-rank a long relationship crosses
// - and a rank slice indexed by node id grows with the graph, while one
// indexed by position within a component would have to be translated at every
// stage that touches it.
//
// The component must already be acyclic: breakCycles runs first, and the ranks
// come out of the layering direction it left behind, not out of child/parent.
//
// # Which way the ranks run
//
// One simplex edge per relationship, from -> to, minimum length 1 and weight 1,
// so rank(to) is at least one more than rank(from). from -> to is child ->
// parent for every relationship the cycle breaker did not touch, and rank 0 is
// drawn leftmost, so a child box sits to the LEFT of the table it references.
//
// That is the single line that decides which side of the diagram parents are
// on, and it is chosen to agree with `jjf export dot`, which sets rankdir=LR
// and emits child -> parent edges and therefore already draws it that way. Two
// diagrams of one document that read in opposite directions would be a worse
// failure than either being drawn the other way round.
//
// Every relationship gets the same weight because the document says nothing
// that would make one relationship more worth shortening than another. Weight
// is what the auxiliary graph in the coordinate pass uses to say that some
// distances matter more; here there is nothing to say.
func assignRanks(g *graph, component []int) []int {
	ranks := make([]int, len(g.nodes))

	// A component of one node is rank 0 and needs no solver. Not a
	// micro-optimisation and not the degenerate case: it is the COMMON case -
	// 50 of pagila's 71 tables have no foreign key at all, so 50 of its
	// components are single nodes (measured by the author of issue #32 on that
	// schema, not on this code). Taking the shortcut explicitly says so.
	if len(component) == 1 {
		return ranks
	}

	// Node ids local to the simplex are positions within the component, which
	// findComponents leaves sorted, so a binary search translates and no map
	// is needed for it.
	edges := make([]simplexEdge, 0, len(g.edges))
	for _, i := range g.layerEdges() {
		from, ok := slices.BinarySearch(component, g.edges[i].from)
		if !ok {
			continue
		}
		to, _ := slices.BinarySearch(component, g.edges[i].to)
		edges = append(edges, simplexEdge{from: from, to: to, minLen: 1, weight: 1})
	}

	local := rank(edges, len(component))
	balanceRanks(edges, local)

	// Normalised again after balancing, because balancing can move the one
	// node that held rank 0 and leave the component starting at 1. Later
	// stages index a slice of layers with these, so "the lowest rank is 0" has
	// to be true of the value that leaves this function.
	low := local[0]
	for _, r := range local {
		low = min(low, r)
	}
	for i, v := range component {
		ranks[v] = local[i] - low
	}
	return ranks
}
