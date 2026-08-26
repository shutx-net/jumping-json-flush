package svg

import (
	"slices"
	"testing"
)

// chainEdges builds a chain 0 -> 1 -> ... -> n-1, every edge minimum length 1
// and weight 1.
func chainEdges(n int) []simplexEdge {
	edges := make([]simplexEdge, 0, n-1)
	for v := range n - 1 {
		edges = append(edges, simplexEdge{from: v, to: v + 1, minLen: 1, weight: 1})
	}
	return edges
}

// longestPathRanks is the baseline the simplex has to beat or match: every
// node as early as its predecessors allow.
//
// It calls the package's own initRank rather than reimplementing it, on
// purpose. A second implementation of the baseline could be wrong in the same
// direction as the thing it is checking; this way the comparison is between
// the ranking the algorithm STARTS from and the ranking it ends with, which is
// exactly the quantity the simplex exists to improve.
func longestPathRanks(edges []simplexEdge, n int) []int {
	s := newSimplex(edges, n)
	s.initRank()
	s.normalize()
	return s.rank
}

// weightedLength is the quantity the simplex minimises.
func weightedLength(edges []simplexEdge, ranks []int) int {
	total := 0
	for _, e := range edges {
		total += e.weight * (ranks[e.to] - ranks[e.from])
	}
	return total
}

// firstInfeasible returns the index of an edge shorter than its minimum
// length, or -1 when the ranking satisfies every constraint.
func firstInfeasible(edges []simplexEdge, ranks []int) int {
	for i := range edges {
		if ranks[edges[i].to]-ranks[edges[i].from] < edges[i].minLen {
			return i
		}
	}
	return -1
}

func TestSimplexTightChain(t *testing.T) {
	edges := chainEdges(4)
	got := rank(edges, 4)

	if want := []int{0, 1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("rank = %v, want %v", got, want)
	}
	if i := firstInfeasible(edges, got); i >= 0 {
		t.Errorf("edge %d is shorter than its minimum length", i)
	}
}

// TestSimplexTightensAgainstLongestPath is the case longest-path ranking gets
// wrong, and the reason this file holds a simplex at all.
//
// The graph: a chain a -> b -> c, and c branching to d and e, so d and e are
// both pinned three ranks past a. Node v hangs off a with edges to both d and
// e. Longest path puts v at rank 1, as early as a allows. But v has one
// in-edge and two out-edges, so moving it one rank later shortens two edges
// and lengthens one, and the total drops from 9 to 8.
//
// Derived by hand before the first run. The objective is
// 2*rank(d) + 2*rank(e) - rank(c) - rank(v), so with d and e at their floor of
// 3 the optimum takes c and v as LATE as their successors allow, which is 2
// for both, and no other ranking ties with it.
func TestSimplexTightensAgainstLongestPath(t *testing.T) {
	const (
		a = iota
		b
		c
		d
		e
		v
	)
	edges := []simplexEdge{
		{from: a, to: b, minLen: 1, weight: 1},
		{from: b, to: c, minLen: 1, weight: 1},
		{from: c, to: d, minLen: 1, weight: 1},
		{from: c, to: e, minLen: 1, weight: 1},
		{from: a, to: v, minLen: 1, weight: 1},
		{from: v, to: d, minLen: 1, weight: 1},
		{from: v, to: e, minLen: 1, weight: 1},
	}

	baseline := longestPathRanks(edges, 6)
	if want := []int{0, 1, 2, 3, 3, 1}; !slices.Equal(baseline, want) {
		t.Fatalf("the longest-path baseline is %v, want %v", baseline, want)
	}

	got := rank(edges, 6)
	if want := []int{0, 1, 2, 3, 3, 2}; !slices.Equal(got, want) {
		t.Errorf("rank = %v, want %v", got, want)
	}
	if i := firstInfeasible(edges, got); i >= 0 {
		t.Errorf("edge %d is shorter than its minimum length", i)
	}

	tight, loose := weightedLength(edges, got), weightedLength(edges, baseline)
	if tight > loose {
		t.Errorf("total weighted length %d against the longest-path baseline's %d: the simplex must never be worse", tight, loose)
	}
	if tight != 8 || loose != 9 {
		t.Errorf("total weighted length %d and baseline %d, want 8 and 9", tight, loose)
	}
}

func TestSimplexRespectsMinLength(t *testing.T) {
	tests := []struct {
		name   string
		edges  []simplexEdge
		n      int
		expect []int
	}{
		{
			"two nodes two ranks apart",
			[]simplexEdge{{from: 0, to: 1, minLen: 2, weight: 1}},
			2,
			[]int{0, 2},
		},
		{
			// The long edge is what fixes the distance; the node on the short
			// path has slack, and with equal weights on both of its edges the
			// simplex is free to leave it at its earliest rank.
			"a long edge beside a short path",
			[]simplexEdge{
				{from: 0, to: 2, minLen: 3, weight: 1},
				{from: 0, to: 1, minLen: 1, weight: 1},
				{from: 1, to: 2, minLen: 1, weight: 1},
			},
			3,
			[]int{0, 1, 3},
		},
		{
			// Weight, not length: the heavy edge is the one that comes out
			// tight, because shortening it is worth three times as much.
			"a heavy edge against a light one",
			[]simplexEdge{
				{from: 0, to: 1, minLen: 1, weight: 1},
				{from: 1, to: 3, minLen: 1, weight: 3},
				{from: 0, to: 2, minLen: 1, weight: 1},
				{from: 2, to: 3, minLen: 3, weight: 1},
			},
			4,
			[]int{0, 3, 1, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rank(tt.edges, tt.n)
			if !slices.Equal(got, tt.expect) {
				t.Errorf("rank = %v, want %v", got, tt.expect)
			}
			if i := firstInfeasible(tt.edges, got); i >= 0 {
				t.Errorf("edge %d is shorter than its minimum length", i)
			}
			if tight, loose := weightedLength(tt.edges, got), weightedLength(tt.edges, longestPathRanks(tt.edges, tt.n)); tight > loose {
				t.Errorf("total weighted length %d against the longest-path baseline's %d", tight, loose)
			}
		})
	}
}

// TestSimplexIsDeterministic runs the same input repeatedly rather than twice.
// The pivot loop is the one place in the package where the answer depends on a
// SEQUENCE of choices, so a tie broken by anything but the edge index would
// show up as a run that disagrees with the others.
func TestSimplexIsDeterministic(t *testing.T) {
	// A graph with ties everywhere: three parallel two-hop paths between the
	// same pair of nodes, so every intermediate node has the same median, the
	// same weights and the same slack as the other two.
	edges := []simplexEdge{
		{from: 0, to: 1, minLen: 1, weight: 1},
		{from: 0, to: 2, minLen: 1, weight: 1},
		{from: 0, to: 3, minLen: 1, weight: 1},
		{from: 1, to: 4, minLen: 1, weight: 1},
		{from: 2, to: 4, minLen: 1, weight: 1},
		{from: 3, to: 4, minLen: 1, weight: 1},
		{from: 0, to: 4, minLen: 4, weight: 1},
	}

	first := rank(edges, 5)
	for range 8 {
		if got := rank(edges, 5); !slices.Equal(got, first) {
			t.Fatalf("rank = %v on a later run, want %v", got, first)
		}
	}
	if i := firstInfeasible(edges, first); i >= 0 {
		t.Errorf("edge %d is shorter than its minimum length", i)
	}
}

// TestBalanceRanksMovesAFreeNode covers the step Gansner's paper calls balance
// and this package keeps out of the solver: a node that can move without
// changing the total length goes where there is most room.
func TestBalanceRanksMovesAFreeNode(t *testing.T) {
	// 0 -> 1 -> 3 with a minimum length of 3 from 0 to 3, so node 1 can sit at
	// rank 1 or rank 2 and the total is the same either way. Node 2 is parked
	// at rank 1 by its own long edge, which makes rank 2 the emptier of the
	// two.
	edges := []simplexEdge{
		{from: 0, to: 1, minLen: 1, weight: 1},
		{from: 1, to: 3, minLen: 1, weight: 1},
		{from: 0, to: 3, minLen: 3, weight: 1},
		{from: 0, to: 2, minLen: 1, weight: 1},
		{from: 2, to: 3, minLen: 2, weight: 1},
	}
	ranks := []int{0, 1, 1, 3}
	before := weightedLength(edges, ranks)

	balanceRanks(edges, ranks)

	if want := []int{0, 2, 1, 3}; !slices.Equal(ranks, want) {
		t.Errorf("ranks = %v, want %v: node 1 is free to move and rank 2 is emptier than rank 1", ranks, want)
	}
	if after := weightedLength(edges, ranks); after != before {
		t.Errorf("total weighted length went from %d to %d: balancing moves only nodes whose in-weight and out-weight are equal, so it cannot change the total", before, after)
	}
	if i := firstInfeasible(edges, ranks); i >= 0 {
		t.Errorf("edge %d is shorter than its minimum length after balancing", i)
	}
}

func TestBalanceRanksLeavesPinnedNodes(t *testing.T) {
	// Node 1 has room to move - anywhere from rank 1 to rank 3 - and must not,
	// because it has two out-edges against one in-edge: moving it later would
	// shorten two edges and lengthen one, and that is a change in the total,
	// which is the one thing balancing may not do. Rank 2 and rank 3 are both
	// empty, so a rule that looked only at where there was room would move it.
	edges := []simplexEdge{
		{from: 0, to: 1, minLen: 1, weight: 1},
		{from: 1, to: 2, minLen: 1, weight: 1},
		{from: 1, to: 3, minLen: 1, weight: 1},
		{from: 0, to: 2, minLen: 4, weight: 1},
		{from: 0, to: 3, minLen: 4, weight: 1},
	}
	ranks := []int{0, 1, 4, 4}
	want := slices.Clone(ranks)

	balanceRanks(edges, ranks)

	if !slices.Equal(ranks, want) {
		t.Errorf("ranks = %v, want %v unchanged", ranks, want)
	}
}
