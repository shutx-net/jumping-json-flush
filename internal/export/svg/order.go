package svg

import "slices"

// ---------------------------------------------------------------------------
// Ordering within the half-ranks
// ---------------------------------------------------------------------------

// orderSweeps is how many times the median heuristic and transpose are run.
//
// A fixed internal constant, and NOT a knob. This exporter owns its layout
// completely - there is no way to ask for a different rank separation, a
// different font or a different iteration count, exactly as there is no way to
// ask internal/export/xlsx for a different column width. A number that
// depended on the graph's size would be a knob no document could see and every
// diff could.
const orderSweeps = 8

// order is one component's nodes arranged into their half-ranks.
//
// layers and pos are the answer and are kept in step: layers[r] is the nodes at
// half-rank r in the order they will be drawn, and pos[v] is v's index inside
// its own layer. pos exists so that the median and the crossing count can ask
// where a node is without searching its layer for it, which is the inner loop
// of everything below.
//
// prev and next are the ordering's view of the relationships: next[v] is the
// nodes one half-rank AFTER v that a route reaches directly from v, and prev[v]
// the ones one half-rank before. They come from the chains, so they are already
// broken into single-half-rank steps, and the two ends of every step are the
// two things that can cross. Both are part of the order rather than passed
// beside it because crossings(o) has to be answerable from an order alone:
// phase 13 holds one and nothing else.
//
// All three slices indexed by node id are sized for the whole graph, with the
// entries of other components left at their zero values, which is the same
// convention the ranks follow and for the same reason.
type order struct {
	layers     [][]int
	pos        []int
	prev, next [][]int
}

// initOrder is the order every sweep starts from: a breadth-first walk from the
// lowest-ranked nodes, taking them in node index order and appending each node
// to its layer the first time it is reached.
//
// Node index order is document order, so wherever the relationships do not say
// otherwise the initial order is the document's own - which is the same
// principle every other tie in this package is broken by. Breadth-first rather
// than by rank so that nodes joined by a relationship start near each other:
// beginning from a good order matters, because the sweeps below only ever
// accept a strict improvement and cannot climb out of a bad one.
func initOrder(g *graph, members []int, ranks []int, chains []chain) order {
	maxRank := 0
	for _, v := range members {
		maxRank = max(maxRank, ranks[v])
	}
	o := order{
		layers: make([][]int, maxRank+1),
		pos:    make([]int, len(g.nodes)),
		prev:   make([][]int, len(g.nodes)),
		next:   make([][]int, len(g.nodes)),
	}

	// One step of a route is one pair of adjacent half-ranks, and a route is
	// its child, its chain and its parent in that order. The relationships
	// with no chain - a self-reference, and a flat relationship if a ranking
	// ever produces one - constrain no ordering at all: a self-reference is
	// drawn as a staple over one box and cannot cross anything by being
	// ordered differently.
	for _, c := range chains {
		e := &g.edges[c.edge]
		route := make([]int, 0, len(c.nodes)+2)
		route = append(route, e.child)
		route = append(route, c.nodes...)
		route = append(route, e.parent)
		for k := 0; k+1 < len(route); k++ {
			lower, upper := route[k], route[k+1]
			if ranks[lower] > ranks[upper] {
				// A relationship the cycle breaker reversed runs from the
				// higher rank to the lower one, and prev/next are about ranks
				// rather than about which end declares the foreign key.
				lower, upper = upper, lower
			}
			o.next[lower] = append(o.next[lower], upper)
			o.prev[upper] = append(o.prev[upper], lower)
		}
	}

	placed := make([]bool, len(g.nodes))
	queue := make([]int, 0, len(members))
	for _, v := range members {
		if ranks[v] == 0 {
			placed[v] = true
			o.layers[0] = append(o.layers[0], v)
			queue = append(queue, v)
		}
	}
	for head := 0; head < len(queue); head++ {
		v := queue[head]
		for _, side := range [2][]int{o.next[v], o.prev[v]} {
			for _, w := range side {
				if placed[w] {
					continue
				}
				placed[w] = true
				o.layers[ranks[w]] = append(o.layers[ranks[w]], w)
				queue = append(queue, w)
			}
		}
	}

	// Anything the walk did not reach, in node index order. A component is
	// connected, so the only node this can be is one whose every relationship
	// is a self-reference - and such a node is a component of its own, seeded
	// above. The sweep is here so that "no node is lost" is true by
	// construction rather than by that argument.
	for _, v := range members {
		if !placed[v] {
			placed[v] = true
			o.layers[ranks[v]] = append(o.layers[ranks[v]], v)
		}
	}

	for r := range o.layers {
		o.reindex(r)
	}
	return o
}

// reindex writes layer r's positions back into pos. Every function that
// reorders a layer ends with this, because pos is what the next one reads.
func (o *order) reindex(r int) {
	for i, v := range o.layers[r] {
		o.pos[v] = i
	}
}

// clone is a copy whose layers can be reordered without touching o's.
//
// prev and next are shared rather than copied: they are the relationships, and
// no sweep writes them. layers and pos are the part that changes.
func (o order) clone() order {
	out := o
	out.layers = make([][]int, len(o.layers))
	for r := range o.layers {
		out.layers[r] = slices.Clone(o.layers[r])
	}
	out.pos = slices.Clone(o.pos)
	return out
}

// crossingsBetween counts the pairs of route steps between layer r and layer
// r+1 that cross.
//
// This is the ORDERING crossing count, and it is the quantity the per-fixture
// ceiling is measured in: a pair of steps crosses when the order of their ends
// in one layer is the opposite of the order of their ends in the other. A
// GEOMETRIC count - segments that actually intersect in the finished drawing -
// would be a different number, and a worse one to hold a ceiling on, because it
// would move when lane assignment or slot assignment changed and would report
// that as an ordering regression.
//
// Two steps out of the same node never cross: they share an end. Two steps into
// the same node likewise. That is what makes a pair with an equal position on
// either side not a crossing rather than a special case.
//
// The count is the naive O(k^2) walk over pairs. Deliberately: the widest layer
// in the five schemas measured in the issue #32 discussion held 2, 2, 4, 7 and
// 18 nodes (measured there on a prototype, not on this code), so the
// asymptotics are irrelevant and an inversion count with a merge sort inside it
// would be more code to get wrong than the thing it speeds up.
func crossingsBetween(o order, r int) int {
	if r+1 >= len(o.layers) {
		return 0
	}

	// The steps between the two layers, as pairs of positions, walked in the
	// order of the lower layer - so a pair with a lower position that is equal
	// is two steps out of one node, and one that is smaller is in order.
	var lower, upper []int
	for _, v := range o.layers[r] {
		for _, w := range o.next[v] {
			lower = append(lower, o.pos[v])
			upper = append(upper, o.pos[w])
		}
	}

	count := 0
	for a := range upper {
		for b := a + 1; b < len(upper); b++ {
			if lower[a] < lower[b] && upper[a] > upper[b] {
				count++
			}
		}
	}
	return count
}

// crossings is the total over every pair of adjacent layers. This is the number
// the ordering minimises and the number the per-fixture ceiling is recorded in.
func crossings(o order) int {
	total := 0
	for r := range o.layers {
		total += crossingsBetween(o, r)
	}
	return total
}

// crossingsAround is the count on the two boundaries a reordering inside layer
// r can change, which is all a single swap has to be judged on.
func crossingsAround(o order, r int) int {
	total := crossingsBetween(o, r)
	if r > 0 {
		total += crossingsBetween(o, r-1)
	}
	return total
}

// medianKey is one node's sort key in one sweep of one layer.
//
// median is TWICE the median position of the node's neighbours in the adjacent
// layer. Twice, because the median of an even number of neighbours is the
// average of the middle two and this package has no fractions; doubling every
// median is exact and orders them exactly as the averages would have.
type medianKey struct {
	median int
	pos    int
	node   int
}

// compareMedianKeys is the total order a layer is sorted into: median value,
// then current position, then node id.
//
// TOTAL is the requirement here, not the heuristic. Two runs of a partial order
// can disagree about two nodes that tie, and then the same document produces
// two different diagrams; the issue #32 discussion also warns that a difference
// against an external implementation of this algorithm will more often be a
// tie-break difference than a bug, which is only a useful thing to know if this
// side of the comparison is pinned.
//
// The current position is what makes a tie keep the order it already had, which
// is the same thing a stable sort would do - written as a key instead, so that
// the comparator is a total order on its own terms and does not depend on which
// sort function it is handed to.
//
// The node id can never decide anything while two nodes in one layer have
// distinct positions, which they always do. It is kept anyway, because a
// comparator that is total over its own type cannot quietly stop being one:
// the day some other pass gives two nodes the same position, this key is what
// keeps the order deterministic instead of handing it to whichever sort
// function is in use.
func compareMedianKeys(a, b medianKey) int {
	switch {
	case a.median != b.median:
		return a.median - b.median
	case a.pos != b.pos:
		return a.pos - b.pos
	default:
		return a.node - b.node
	}
}

// medianSweep reorders every layer by the median position of each node's
// neighbours in the layer before it (forward) or after it (backward).
//
// Forward walks up from the second layer so that each layer is ordered against
// one that has already been ordered in this sweep, which is the whole mechanism
// by which an improvement propagates along the ranks; backward walks down for
// the same reason in the other direction.
func medianSweep(o *order, forward bool) {
	if forward {
		for r := 1; r < len(o.layers); r++ {
			o.sortLayer(r, o.prev)
		}
		return
	}
	for r := len(o.layers) - 2; r >= 0; r-- {
		o.sortLayer(r, o.next)
	}
}

// sortLayer reorders layer r by the medians of the neighbours side gives.
//
// A node with no neighbour on that side KEEPS ITS SLOT, and the other nodes are
// sorted into the slots they occupied between them. Giving it a median of -1
// instead, which is the obvious way to write this, would sort every such node to
// the front of the layer and drag the nodes that do have neighbours around it -
// so a table whose only relationship points the other way would decide where
// its whole layer went.
func (o *order) sortLayer(r int, side [][]int) {
	layer := o.layers[r]
	keys := make([]medianKey, 0, len(layer))
	slots := make([]int, 0, len(layer))

	positions := make([]int, 0, len(layer))
	for i, v := range layer {
		positions = positions[:0]
		for _, w := range side[v] {
			positions = append(positions, o.pos[w])
		}
		if len(positions) == 0 {
			continue
		}
		slices.Sort(positions)
		mid := len(positions) / 2
		median := 2 * positions[mid]
		if len(positions)%2 == 0 {
			median = positions[mid-1] + positions[mid]
		}
		keys = append(keys, medianKey{median: median, pos: i, node: v})
		slots = append(slots, i)
	}

	slices.SortFunc(keys, compareMedianKeys)
	for i, k := range keys {
		layer[slots[i]] = k.node
	}
	o.reindex(r)
}

// transpose walks every layer swapping adjacent pairs, and keeps a swap only
// when it STRICTLY reduces the crossings on the two boundaries it can affect.
//
// Strictly, in both this and the best-order bookkeeping in orderComponent,
// because accepting an equal-crossing swap makes the answer depend on how many
// sweeps ran and in which direction the last one went, for no measured gain.
// Implementations that alternate a "reverse" flag to escape a local minimum by
// taking equal swaps are deliberately not followed here: reproducibility is
// worth more to this exporter than the last crossing or two, and the crossing
// ceiling is what says whether that ever stops being true.
//
// It terminates because every swap it keeps lowers a count that cannot go below
// zero, so a pass that changes nothing is reached in finitely many passes.
func transpose(o *order) {
	for improved := true; improved; {
		improved = false
		for r := range o.layers {
			layer := o.layers[r]
			for i := 0; i+1 < len(layer); i++ {
				before := crossingsAround(*o, r)
				layer[i], layer[i+1] = layer[i+1], layer[i]
				o.pos[layer[i]], o.pos[layer[i+1]] = i, i+1

				if crossingsAround(*o, r) < before {
					improved = true
					continue
				}
				layer[i], layer[i+1] = layer[i+1], layer[i]
				o.pos[layer[i]], o.pos[layer[i+1]] = i, i+1
			}
		}
	}
}

// orderComponent decides the order of the nodes within every half-rank.
//
// The median heuristic plus transpose, alternating direction, keeping the best
// order seen. Exhaustive per-layer minimisation is affordable at these widths
// and is NOT used: it was implemented and compared by the author of issue #32
// and it lost - 1 crossing against 0 on Chinook, 7 against 6 on a synthetic
// 15-table schema, 80 against 39 on pagila - because being exact within one
// layer is a local greed, while the quality comes from transpose sweeping
// across layers. Those numbers were measured on that prototype, not on this
// code.
//
// The initial order is a candidate, and a tie keeps the earlier order, so an
// eight-sweep run that finds nothing better returns what initOrder built.
func orderComponent(g *graph, members []int, ranks []int, chains []chain) order {
	o := initOrder(g, members, ranks, chains)
	best, fewest := o.clone(), crossings(o)

	for sweep := range orderSweeps {
		medianSweep(&o, sweep%2 == 0)
		transpose(&o)
		if count := crossings(o); count < fewest {
			best, fewest = o.clone(), count
		}
	}
	return best
}
