package svg

import "slices"

// ---------------------------------------------------------------------------
// Rank doubling, label nodes and long-edge chains
// ---------------------------------------------------------------------------

// routeKind is which of the three shapes a relationship is drawn as. Every
// relationship in every document is exactly one of them; see classify for why
// that is provable rather than merely true so far.
type routeKind int

const (
	routeInterRank routeKind = iota
	routeSameRank
	routeSelfLoop
)

// chain is the ordered list of nodes one relationship's route passes through
// between its two ends: its label node first, then one zero-width node per
// remaining half-rank it crosses. The ends themselves are not in it - they are
// the relationship's child and parent, and they are nodes the document asked
// for rather than nodes this stage invented.
//
// The order is the DRAWING order, child first. For a relationship the cycle
// breaker reversed that runs from the higher rank to the lower one, which is
// the whole visible consequence of a reversal.
type chain struct {
	edge  int
	nodes []int
}

// doubleRanks multiplies every rank by two.
//
// This is what makes the routing taxonomy provably complete rather than
// complete-so-far, and the argument is short enough to keep here. Before
// doubling, an edge from rank r to rank r+k spans k, and k is 0 for a
// self-reference and at least 1 for everything else, because every
// relationship layering saw carried a minimum length of one rank. After
// doubling that same edge runs from 2r to 2r+2k, so every span is 0 or an even
// number of at least 2, and SPAN 1 IS UNREACHABLE. Three categories - one for
// span 0 with two ends, one for span 0 with one end, one for everything else -
// is therefore exhaustive, and the odd half-ranks doubling opened up are
// exactly the space span 1 would have needed.
//
// It is dot's own answer to where an edge label goes, and the reason it goes
// in with the virtual nodes rather than after them: the label becomes a node
// like any other, placed by the ordering and the coordinate assignment that
// already exist, with no placement pass of its own and no constraint to write.
//
// Entries for nodes in other components are 0 and stay 0: the ranks of one
// component are one slice, and doubling zero is zero.
func doubleRanks(ranks []int) {
	for i := range ranks {
		ranks[i] *= 2
	}
}

// classify says which of the three routing categories relationship i falls in.
//
// The three are exhaustive BECAUSE of rank doubling - see doubleRanks for the
// argument, which is not repeated here so that there is one copy of it to keep
// true. The default case is the span-2-or-more case and not a catch-all:
// TestDoubleRanksSpanIsNeverOne is what stands behind it, asserting the
// premise over every document in the test set rather than trusting a branch
// here to absorb a span the argument says cannot exist.
//
// A self-reference is recognised by its two ends being one node, which is the
// same test extractLoops used to take it out of the layering edge list, so the
// two cannot drift apart.
func classify(g *graph, ranks []int, i int) routeKind {
	e := &g.edges[i]
	switch {
	case e.from == e.to:
		return routeSelfLoop
	case ranks[e.from] == ranks[e.to]:
		return routeSameRank
	default:
		return routeInterRank
	}
}

// insertVirtualNodes doubles the component's ranks and then fills the space
// that opens up: one label node per ordinary relationship in the half-rank
// beside its child, and one zero-width node per further half-rank it crosses.
// It returns the component's node list extended with the nodes it created, the
// ranks slice grown to match, and one chain per relationship that has one.
//
// Both slices are returned rather than written through because appending to
// either can move it, and the node ids the new nodes get are positions in
// g.nodes - so the graph, the ranks and the component list all have to grow
// together.
//
// # Why labels are nodes at all, and why they are not optional
//
// Every relationship in the DOT output carries a label, and
// internal/export/svg/testdata/edge.json has two foreign keys between the same
// pair of tables that nothing but their labels tells apart. Dropping labels
// would not be a refinement left for later; it would be an information
// regression against `jjf export dot` on a document that already exists.
//
// Making the label a node, rather than placing it after routing, is what keeps
// that affordable: it is ordered and positioned by the passes that already
// run, it reserves its own space so nothing else can be drawn where the text
// goes, and it costs about a line here against a placement pass with its own
// constraints if it is retrofitted once routing exists.
//
// # Why the long-edge chain is the most load-bearing decision in the file
//
// A relationship crossing more than one half-rank gets a node in every
// half-rank it crosses. Without them a prototype of this exporter put 10 edge
// segments straight through table boxes on a 15-table schema (measured by the
// author of issue #32 on that prototype, not on this code).
//
// What fixes it is not a smarter router. The routing in this package does no
// obstacle test at all - it joins waypoints with axis-aligned segments and
// never asks what is in the way. It does not have to: once a virtual node is
// ordered and given a coordinate like any other node, the corridor its route
// runs down is already reserved, because nothing else can be placed where a
// node is. That is the difference between an orthogonal renderer that spends
// no lines on obstacle avoidance and graphviz spending 1,962 in lib/pathplan.
func insertVirtualNodes(g *graph, component []int, ranks []int) (members, doubled []int, chains []chain) {
	doubleRanks(ranks)

	// Cloned rather than appended to: component is one of g.components, and
	// growing it in place would write into a slice the graph still owns.
	members = slices.Clone(component)

	// Which relationships get a chain, decided once. A same-rank relationship
	// and a self-reference get neither a label node nor a chain, by design and
	// not by omission: rank doubling leaves a span-0 edge no half-rank to put
	// a label in, so both are the special-case routers' business, labels
	// included, and the demand pass already knows which side of a box they
	// attach to.
	interRank := make([]int, 0, len(g.edges))
	for i := range g.edges {
		if !slices.Contains(component, g.edges[i].from) {
			continue
		}
		if classify(g, ranks, i) == routeInterRank {
			interRank = append(interRank, i)
		}
	}

	// The label nodes first, all of them, then the chains: two passes in a
	// fixed order over one list in edge index order, so the node ids the new
	// nodes get are a function of the document alone.
	labelNode := make([]int, len(g.edges))
	for i := range labelNode {
		labelNode[i] = -1
	}
	for _, i := range interRank {
		e := &g.edges[i]
		labelNode[i] = len(g.nodes)
		width := measureText(e.label) + 2*cellPadH
		g.nodes = append(g.nodes, node{
			kind:  kindLabel,
			label: e.label,
			// The band is this rectangle plus labelGap below it, and the route
			// runs along the band's bottom edge - so the text sits labelGap
			// above the line rather than on it. Routing the line through the
			// band's middle instead, which is the obvious alternative, draws
			// it through the text.
			labelRect: Rect{W: width, H: labelHeight},
		})
		ranks = append(ranks, labelHalfRank(g, ranks, i))
		members = append(members, labelNode[i])
	}

	for _, i := range interRank {
		e := &g.edges[i]
		nodes := []int{labelNode[i]}
		// Every half-rank strictly between the two ends holds exactly one node
		// of this chain. step is which way that is: +1 for a relationship
		// drawn left to right, -1 for one the cycle breaker reversed, whose
		// child sits at the HIGHER rank.
		step := halfRankStep(ranks, e)
		for r := ranks[labelNode[i]] + step; r != ranks[e.parent]; r += step {
			v := len(g.nodes)
			g.nodes = append(g.nodes, node{kind: kindVirtual})
			ranks = append(ranks, r)
			nodes = append(nodes, v)
			members = append(members, v)
		}
		chains = append(chains, chain{edge: i, nodes: nodes})
	}

	return members, ranks, chains
}

// labelHalfRank is the half-rank a relationship's label node goes in: the one
// next to its CHILD, a step towards its parent.
//
// Next to the child rather than in the middle so that a reader following a
// line from a box finds the name of the relationship immediately, and so that
// two relationships between the same pair of tables label themselves at the
// end where they are still distinguishable. For a relationship the cycle
// breaker reversed, the child is the higher-ranked end, so the step towards
// the parent goes down - which is why this is not simply rank(child)+1.
func labelHalfRank(g *graph, ranks []int, i int) int {
	e := &g.edges[i]
	return ranks[e.child] + halfRankStep(ranks, e)
}

// halfRankStep is +1 when e's child is the lower-ranked of its two ends and -1
// when it is the higher. Doubling makes the span at least 2 for every
// relationship this is asked about, so the step always lands strictly between
// the ends.
func halfRankStep(ranks []int, e *edge) int {
	if ranks[e.child] > ranks[e.parent] {
		return -1
	}
	return 1
}
