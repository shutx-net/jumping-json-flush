package svg

// ---------------------------------------------------------------------------
// The network simplex
// ---------------------------------------------------------------------------

// This file is the one optimiser in the package, and it is called twice.
//
// Gansner, Koutsofios, North and Vo, "A Technique for Drawing Directed
// Graphs", IEEE Transactions on Software Engineering 19(3), 1993, section 2.3.
// The problem: given nodes and directed edges, each edge carrying a minimum
// length and a weight, give every node an integer rank such that every edge is
// at least as long as its minimum and the total weighted edge length is as
// small as it can be.
//
// Layering calls it with one edge per relationship. The coordinate pass calls
// it on Gansner's auxiliary graph, where the "rank" that comes back is a
// position along the cross axis instead. That second caller is the reason this
// file knows NOTHING about tables, foreign keys, columns, boxes, ranks-as-
// columns or SVG: its whole input is integer node ids and integer edge weights.
// A table-shaped field slipped in here for the convenience of the layering
// caller would quietly end the second use, and the second use is the harder of
// the two.
//
// Why not Brandes-Kopf for the coordinates, which is the other standard answer:
// a 2020 erratum by Brandes, Walter and Zink (arXiv:2008.01252) reports two
// flaws in the 2001 paper, one of them previously undocumented and needing a
// non-trivial adaptation, so an implementation written from the original paper
// produces coordinates that are wrong. Wrong coordinates look plausible, which
// is the worst failure mode available. Reusing one optimiser that the layering
// tests have already exercised is the better bet at this size than a second
// algorithm whose published description is known to be incomplete.

// simplexEdge is one constraint: rank(to) - rank(from) >= minLen, contributing
// weight * (rank(to) - rank(from)) to the total being minimised.
//
// from and to are node ids in [0,n), dense and local to the call. Translating
// document node ids into them is the caller's job, which is what keeps this
// file free of any notion of what a node is.
type simplexEdge struct {
	from, to int
	minLen   int
	weight   int
}

// other is the end of e that is not v. A caller that asks about a node e does
// not touch gets v back, which every walk below already treats as "nowhere new
// to go".
func (e simplexEdge) other(v int) int {
	if e.from == v {
		return e.to
	}
	return e.from
}

// simplex is one run of the solver. It is a struct rather than a set of
// functions taking eight arguments each because the ranks, the spanning tree
// and the incidence lists are read by every step of the loop.
type simplex struct {
	n     int
	edges []simplexEdge

	// rank is the current answer. Every step of the algorithm keeps it
	// FEASIBLE - no edge shorter than its minimum - so the value is usable
	// even if the pivot loop stops early.
	rank []int

	// incident[v] lists, in ascending edge index, every edge with an end at v.
	// Undirected: the tree walks below have to cross a tree edge in whichever
	// direction they arrive at it from.
	incident [][]int

	// inTree[i] says whether edge i is in the current spanning tree - the
	// basis, in simplex terms. Exactly n-1 entries are true.
	inTree []bool

	// mark and stack are scratch for the tree walks. They are fields rather
	// than locals because tailSide is called once per tree edge per pivot, and
	// the walk is the whole cost of the algorithm at these sizes.
	mark  []bool
	stack []int
}

// rank assigns every node in [0,n) an integer rank, minimising the total
// weighted edge length subject to every edge's minimum length, and normalised
// so the smallest rank is 0.
//
// Preconditions, none of them checked: every edge's ends are in [0,n); no edge
// is a self-loop; no weight is negative; the graph is ACYCLIC read as directed
// and CONNECTED read as undirected. The connectivity one is load-bearing - the
// basis is a SPANNING tree, and a graph in two pieces has none - and it is why
// the layering caller passes one connected component at a time.
//
// A precondition violation is not defended against here. A self-loop, for
// instance, leaves its node out of the topological order and produces a
// visibly wrong ranking rather than a silently plausible one, which is what
// the caller needs to see: self-loops are extracted before layering, so one
// arriving here means the stage that removed them stopped working.
func rank(edges []simplexEdge, n int) []int {
	s := newSimplex(edges, n)
	if n == 0 {
		// Nothing to rank. Worth one line here because every tree walk below
		// starts at node 0, so this is the one input shape that would reach
		// them and find no node 0 to start from.
		return s.rank
	}
	s.feasibleTree()

	// The pivot loop, and the reason it is bounded.
	//
	// leaveEdge and enterEdge both take the LOWEST eligible edge index, which
	// is Bland's rule, the classical anti-cycling pivot rule. It matters
	// because a pivot whose entering edge has zero slack changes the tree
	// without changing the total length, and nothing but the choice rule stops
	// a run of those from returning to a tree it has already had. Bland's rule
	// is believed to exclude that; it is not PROVED here, and the failure mode
	// of a wrong belief is a command-line tool that hangs rather than one that
	// draws a slightly worse diagram. So the loop is bounded, generously: the
	// most pivots any graph in this package's tests reaches is 1 - on the
	// six-node graph in TestSimplexTightensAgainstLongestPath, measured by
	// counting them at the commit that added this file - against the bound of
	// 48 that n*(edges+1) gives for that same graph.
	//
	// Reaching the bound would leave a ranking that is still feasible and only
	// possibly not optimal, because every tree the loop passes through is a
	// tight spanning tree and the ranks are recomputed from it.
	limit := s.n * (len(s.edges) + 1)
	for range limit {
		leave := s.leaveEdge()
		if leave < 0 {
			// No tree edge has a negative cut value, which is the optimality
			// condition: no exchange can shorten the total.
			break
		}
		enter := s.enterEdge(leave)
		if enter < 0 {
			// Unreachable. A negative cut value is a sum of non-negative
			// weights in which the edges crossing back outweigh the ones
			// crossing forward, so at least one edge crosses back and is a
			// candidate. The guard is here because the reader should not have
			// to reconstruct that argument to see that the loop terminates.
			break
		}
		s.exchange(leave, enter)
	}

	s.normalize()
	return s.rank
}

// newSimplex sets up one run: the incidence lists, and room for the ranks.
//
// The incidence list is undirected - every edge appears at both of its ends -
// because the tree walks have to cross a tree edge from whichever end they
// reach it. The direction is still there in the edge itself, which is what the
// walks read to decide whether a rank goes up or down.
func newSimplex(edges []simplexEdge, n int) *simplex {
	s := &simplex{
		n:        n,
		edges:    edges,
		rank:     make([]int, n),
		incident: make([][]int, n),
		mark:     make([]bool, n),
	}
	for i := range edges {
		s.incident[edges[i].from] = append(s.incident[edges[i].from], i)
		if edges[i].to != edges[i].from {
			s.incident[edges[i].to] = append(s.incident[edges[i].to], i)
		}
	}
	return s
}

// slack is how much longer edge i is than it has to be. A feasible ranking has
// no negative slack, and a TIGHT edge has none to spare.
func (s *simplex) slack(i int) int {
	e := &s.edges[i]
	return s.rank[e.to] - s.rank[e.from] - e.minLen
}

// initRank is the longest-path ranking: every node as early as its
// predecessors allow.
//
// It is feasible, which is all the simplex needs from a starting point, and it
// is what the simplex is measured against in the tests - the total weighted
// length of the answer must never be worse than this. It is also why a
// "temporary" longest-path ranking is not a step on the way to a simplex: this
// IS that ranking, it is one function, and what the simplex adds is the part
// that moves nodes later when their out-weight exceeds their in-weight.
func (s *simplex) initRank() {
	pending := make([]int, s.n)
	for i := range s.edges {
		pending[s.edges[i].to]++
	}

	// Kahn's algorithm, seeded in node index order and appending as it frees
	// nodes, so the topological order is a function of the input alone. A
	// cyclic input leaves the nodes on the cycle out of the queue - visibly
	// wrong, and not a loop that fails to end.
	queue := make([]int, 0, s.n)
	for v := range s.n {
		if pending[v] == 0 {
			queue = append(queue, v)
		}
	}
	for head := 0; head < len(queue); head++ {
		v := queue[head]
		for _, i := range s.incident[v] {
			e := &s.edges[i]
			if e.from != v {
				continue
			}
			if r := s.rank[v] + e.minLen; r > s.rank[e.to] {
				s.rank[e.to] = r
			}
			pending[e.to]--
			if pending[e.to] == 0 {
				queue = append(queue, e.to)
			}
		}
	}
}

// tightTree grows the largest tree of TIGHT edges containing node 0 and
// reports which nodes it reached and which edges it used. It reaches count-1
// edges for count nodes, because an edge is taken only when it reaches a node
// the tree does not have yet.
func (s *simplex) tightTree() (in []bool, treeEdges []int) {
	in = make([]bool, s.n)
	in[0] = true
	stack := []int{0}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, i := range s.incident[v] {
			w := s.edges[i].other(v)
			if in[w] || s.slack(i) != 0 {
				continue
			}
			in[w] = true
			treeEdges = append(treeEdges, i)
			stack = append(stack, w)
		}
	}
	return in, treeEdges
}

// feasibleTree establishes the first basis: a feasible ranking whose tight
// edges span every node.
//
// Gansner's construction, unchanged. Grow the tight tree; if it does not span,
// pick the incident non-tree edge with the least slack and shift the whole
// tree until that edge is tight, which costs nothing in feasibility - shifting
// a tree rigidly leaves every edge inside it tight - and gains at least one
// node. So the loop runs at most n times.
func (s *simplex) feasibleTree() {
	s.initRank()
	for {
		in, treeEdges := s.tightTree()
		if len(treeEdges) < s.n-1 && s.tighten(in) {
			continue
		}
		s.inTree = make([]bool, len(s.edges))
		for _, i := range treeEdges {
			s.inTree[i] = true
		}
		return
	}
}

// tighten shifts the tree marked by in until the least-slack edge leaving it
// is tight, and reports whether there was such an edge at all. There is one
// for any connected graph whose tree does not yet span it; false means the
// precondition was broken, and the caller stops rather than spinning.
func (s *simplex) tighten(in []bool) bool {
	best, bestSlack := -1, 0
	for i := range s.edges {
		e := &s.edges[i]
		if in[e.from] == in[e.to] {
			continue
		}
		if sl := s.slack(i); best < 0 || sl < bestSlack {
			best, bestSlack = i, sl
		}
	}
	if best < 0 {
		return false
	}

	// The shift moves the tree towards the new node when the tree holds the
	// edge's tail, and away from it when the tree holds its head, which in
	// both cases lands the edge on exactly zero slack.
	delta := bestSlack
	if in[s.edges[best].to] {
		delta = -delta
	}
	for v := range s.n {
		if in[v] {
			s.rank[v] += delta
		}
	}
	return true
}

// tailSide marks every node the tree still reaches from edge i's tail once
// edge i is removed from it. That is one side of the cut edge i makes.
//
// The returned slice is scratch owned by s and is overwritten by the next
// call, so a caller must finish with it before asking again. It is shared
// because this walk is the whole cost of the algorithm: it runs once per tree
// edge per pivot.
func (s *simplex) tailSide(i int) []bool {
	clear(s.mark)
	start := s.edges[i].from
	s.mark[start] = true
	s.stack = append(s.stack[:0], start)
	for len(s.stack) > 0 {
		v := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		for _, j := range s.incident[v] {
			if j == i || !s.inTree[j] {
				continue
			}
			if w := s.edges[j].other(v); !s.mark[w] {
				s.mark[w] = true
				s.stack = append(s.stack, w)
			}
		}
	}
	return s.mark
}

// cutValue is tree edge i's cut value: the total weight of the edges crossing
// its cut in its own direction, less the total weight of those crossing back.
// A negative one means the tree can be improved by exchanging edge i for one
// of the edges crossing back.
//
// It is recomputed from scratch, per tree edge, per pivot - O(V*E) where
// Gansner's leaf-first method is O(V+E) for the whole tree, and O(1) per pivot
// after that if the incremental update is written too. That is the deliberate
// trade in this file: the incremental cut-value update is where an
// implementation of this algorithm goes subtly wrong, the failure is a
// plausible-looking ranking rather than a crash, and the sizes make the
// asymptotics irrelevant. The layering call is one edge per foreign key; the
// coordinate call, which is the larger of the two, is a few hundred nodes and
// edges for a 71-table schema.
func (s *simplex) cutValue(i int) int {
	tail := s.tailSide(i)
	cut := 0
	for j := range s.edges {
		e := &s.edges[j]
		switch {
		case tail[e.from] && !tail[e.to]:
			cut += e.weight
		case !tail[e.from] && tail[e.to]:
			cut -= e.weight
		}
	}
	return cut
}

// leaveEdge picks the tree edge to remove: the LOWEST-indexed one with a
// negative cut value, or -1 when none has, which is the optimality condition.
//
// Lowest rather than most negative is Bland's rule, and it is chosen for
// termination rather than for speed: see the pivot loop in rank. Most-negative
// would take fewer pivots on a large graph and would reintroduce the question
// this rule closes.
func (s *simplex) leaveEdge() int {
	for i := range s.edges {
		if s.inTree[i] && s.cutValue(i) < 0 {
			return i
		}
	}
	return -1
}

// enterEdge picks the edge to put in leave's place: of the non-tree edges
// crossing leave's cut in the opposite direction, the one with the least
// slack, ties going to the lowest index.
//
// Least slack because the slack is how far the ranks have to move to make the
// new edge tight, and moving further than necessary would push some other edge
// negative - the exchange has to keep the ranking feasible. Lowest index on a
// tie for the same reason leaveEdge takes the lowest: Bland's rule.
func (s *simplex) enterEdge(leave int) int {
	tail := s.tailSide(leave)
	best, bestSlack := -1, 0
	for i := range s.edges {
		if s.inTree[i] {
			continue
		}
		// Crossing back means: tail outside leave's tail side, head inside it.
		e := &s.edges[i]
		if tail[e.from] || !tail[e.to] {
			continue
		}
		if sl := s.slack(i); best < 0 || sl < bestSlack {
			best, bestSlack = i, sl
		}
	}
	return best
}

// exchange swaps leave out of the tree for enter and re-derives the ranks.
func (s *simplex) exchange(leave, enter int) {
	s.inTree[leave] = false
	s.inTree[enter] = true
	s.rankFromTree()
}

// rankFromTree recomputes every rank from the spanning tree alone, by making
// every tree edge exactly tight.
//
// This is the whole of Gansner's "rerank" step, and it replaces it. The tree
// determines the ranks up to one additive constant - that is what a tight
// spanning tree IS - so re-deriving them is exact, and it needs none of the
// low/lim numbering, none of the "which component is smaller" case analysis
// and no incremental shift, all of which exist to avoid a walk that costs O(V)
// on graphs a thousand times larger than any diagram here.
func (s *simplex) rankFromTree() {
	seen := make([]bool, s.n)
	seen[0] = true
	s.rank[0] = 0
	stack := []int{0}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, i := range s.incident[v] {
			if !s.inTree[i] {
				continue
			}
			e := &s.edges[i]
			w := e.other(v)
			if seen[w] {
				continue
			}
			seen[w] = true
			if e.from == v {
				s.rank[w] = s.rank[v] + e.minLen
			} else {
				s.rank[w] = s.rank[v] - e.minLen
			}
			stack = append(stack, w)
		}
	}
}

// normalize shifts every rank so that the smallest is 0.
//
// The ranking is only ever determined up to an additive constant, so this is
// what makes the answer a value rather than one of infinitely many equivalent
// ones - and every caller wants ranks it can index a slice of layers with.
func (s *simplex) normalize() {
	low := 0
	for i, r := range s.rank {
		if i == 0 || r < low {
			low = r
		}
	}
	for v := range s.rank {
		s.rank[v] -= low
	}
}

// balanceRanks moves every node that can move without changing the total
// weighted edge length to the least crowded rank it can legally take.
//
// Gansner's balance step, and it is deliberately NOT part of rank. The paper
// describes two different balance steps for the two uses of this solver - one
// that spreads nodes across ranks for layering, one that centres a node
// between its neighbours for coordinates - so a balance inside the solver
// would have to become a mode flag the moment the second caller arrives. The
// solver returns the optimum; each caller balances it the way its own axis
// wants.
//
// A node is free to move exactly when its in-weight equals its out-weight,
// because shifting a node by d changes the total by (in-weight - out-weight)*d.
// The range it can move in is set by its neighbours: no earlier than its
// latest predecessor allows, no later than its earliest successor allows. A
// node with no edges at all is free by this definition and is placed wherever
// there is most room, which is the same answer for the same reason.
//
// Ranks are updated as it goes and the scan starts from the lowest legal rank,
// so a tie between two equally crowded ranks goes to the earlier one and the
// result is a function of the input alone.
//
// The ranks must already be normalised - no negative rank - because the
// crowding is counted in a slice indexed by rank. rank returns them that way.
func balanceRanks(edges []simplexEdge, ranks []int) {
	maxRank := 0
	for _, r := range ranks {
		maxRank = max(maxRank, r)
	}
	count := make([]int, maxRank+1)
	for _, r := range ranks {
		count[r]++
	}

	incident := make([][]int, len(ranks))
	for i := range edges {
		incident[edges[i].from] = append(incident[edges[i].from], i)
		if edges[i].to != edges[i].from {
			incident[edges[i].to] = append(incident[edges[i].to], i)
		}
	}

	for v := range ranks {
		inWeight, outWeight := 0, 0
		low, high := 0, maxRank
		for _, i := range incident[v] {
			e := &edges[i]
			if e.to == v {
				inWeight += e.weight
				low = max(low, ranks[e.from]+e.minLen)
			}
			if e.from == v {
				outWeight += e.weight
				high = min(high, ranks[e.to]-e.minLen)
			}
		}
		if inWeight != outWeight || low >= high {
			continue
		}
		choice := low
		for r := low + 1; r <= high; r++ {
			if count[r] < count[choice] {
				choice = r
			}
		}
		count[ranks[v]]--
		count[choice]++
		ranks[v] = choice
	}
}
