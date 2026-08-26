package svg

import "slices"

// ---------------------------------------------------------------------------
// Coordinates
// ---------------------------------------------------------------------------

// The omega weights of Gansner et al. 1993, section 4.2: how much straighter
// one kind of route step is worth than another.
//
// They are not geometry - they are the objective the coordinate pass
// minimises - which is why they are here rather than in coord.go's block, the
// same reason measure.go's advance widths are not there either.
//
// What they are FOR: making a long relationship come out as one straight line
// rather than a staircase. A bend between two virtual nodes is eight times as
// expensive as a bend between two boxes, so when something has to give, the
// solver moves the boxes and leaves the corridor straight. Without the
// weighting a long route bends at every virtual node it passes, which no
// invariant catches and every reader sees. Dropping them to 1/1/1 "for
// simplicity" is therefore not a simplification, it is the feature being
// removed.
//
// omegaRealReal is unreachable through this pipeline and is kept anyway: rank
// doubling puts a label node between the two ends of every relationship, so no
// step ever joins two boxes directly. It is here for the same reason
// internal/export/dot's arrow keeps the case neither of its callers can reach
// - a mapping that is total over its input cannot quietly stop being one - and
// TestOmegaIsTotalOverTheKinds names all three.
const (
	omegaRealReal       = 1
	omegaRealVirtual    = 2
	omegaVirtualVirtual = 8
)

// isReal says whether a node is a box the reader sees. A label node and a
// virtual node are both placeholders, and the omega weights treat them alike:
// a label node is a piece of a route that happens to carry text.
func isReal(k nodeKind) bool { return k == kindTable || k == kindStub }

// omega is the straightness weight of a route step between two node kinds.
func omega(a, b nodeKind) int {
	switch {
	case isReal(a) && isReal(b):
		return omegaRealReal
	case isReal(a) || isReal(b):
		return omegaRealVirtual
	default:
		return omegaVirtualVirtual
	}
}

// finalSizes is every node's finished size, indexed by node id: its content
// where that is the larger, and its slot demand where that is.
//
// Sizes are INPUTS to everything below - the half-rank columns are as wide as
// their widest box and the separation constraints are stated in heights - so
// sizing has to be finished before this pass runs. That is why text
// measurement happens at graph construction and the routing minimum in
// slotDemand's phase; a change that moved either of them after this point
// would leave the separation constraints computed from sizes the boxes no
// longer have, and the drawing would overlap in a way nothing here could
// notice.
func finalSizes(g *graph, demands []demand) (widths, heights []Coord) {
	widths = make([]Coord, len(g.nodes))
	heights = make([]Coord, len(g.nodes))
	for v := range g.nodes {
		widths[v], heights[v] = finalSize(&g.nodes[v], demands[v])
	}
	return widths, heights
}

// anchorOffsets splits a node's height into the part above the line a route
// meets it on and the part below.
//
// For a box the anchor is its centre, which is Gansner's convention and what
// makes the separation constraint below the familiar (h(a)+h(b))/2 + nodeSep.
// For a LABEL node the anchor is the bottom edge of its band, because that is
// where the route actually runs: the label rectangle is the strip at the top
// of the band and the text sits labelGap above the line (D18). Anchoring a
// label at its centre instead would align the middle of the text with the
// corridor and leave every long route with a jog of half a label height at the
// one node that exists to sit on it.
//
// The two parts always sum to exactly h, whatever h's parity, which is what
// keeps the separation exact: an implementation that used h/2 on both sides
// would lose a unit on an odd height and let two boxes end up nodeSep-1 apart.
// A drawing where two boxes are one tenth of a pixel too close is not visible
// and does not fail here - it fails later, in the invariant suite, on some
// other document. Heights in this package are all multiples of unit today, so
// the case is unreachable; the point is that it stays unreachable if that ever
// stops being true.
func anchorOffsets(k nodeKind, h Coord) (above, below Coord) {
	if k == kindLabel {
		return h, 0
	}
	return h / 2, h - h/2
}

// anchorY is the y at which a route meets a node, read back off its finished
// rectangle: the centre of a box, the bottom edge of a label node's band -
// where the route runs, with the text in the strip above it - and the point
// itself for a virtual node, which has no extent at all.
//
// This is the value the coordinate pass aligns, which is why the router asks
// for it rather than for a rectangle's centre: a route that aimed at the
// centre of a label node would cross its own text.
func anchorY(k nodeKind, r Rect) Coord {
	_, below := anchorOffsets(k, r.H)
	return r.Bottom() - below
}

// separation is the least distance allowed between the anchors of a and b,
// where a is the node immediately above b in one half-rank: everything of a
// that hangs below its anchor, the gap, and everything of b that stands above
// its own anchor.
func separation(g *graph, heights []Coord, a, b int) Coord {
	_, belowA := anchorOffsets(g.nodes[a].kind, heights[a])
	aboveB, _ := anchorOffsets(g.nodes[b].kind, heights[b])
	return belowA + nodeSep + aboveB
}

// rankX is the x of each half-rank's left edge.
//
// A half-rank is as wide as its widest node and the next one starts one
// corridor further on, so the corridor between two columns is the same width
// all the way down. A half-rank holding only virtual nodes is zero wide and
// costs exactly one corridor, which is what makes a long route's corridor no
// wider than it needs to be.
//
// The corridor's width is gapWidth(count[r]) and not a constant, which is the
// one place the routing reaches coordinate assignment: a corridor is as wide as
// the channels the routes bending in it need, and a corridor no route bends in
// is exactly rankGap, the width it has always been. This is a running sum and
// not a constraint, so nothing about the simplex, the auxiliary graph or the
// separation constraints is involved.
//
// A node narrower than its half-rank is LEFT-aligned in it rather than
// centred: the first segment of every route leaving a column is then the same
// length, so the crow's feet all sit at the same distance from their boxes and
// the corridor reads as one gap rather than as a ragged one. It is also what
// makes columnRight[r] a real boundary - past every node in the column - which
// is what the channels are measured from.
func rankX(o order, widths []Coord, count []int) []Coord {
	xs := make([]Coord, len(o.layers))
	var x Coord
	for r, layer := range o.layers {
		xs[r] = x
		var widest Coord
		for _, v := range layer {
			widest = max(widest, widths[v])
		}
		x += widest + gapWidth(count[r])
	}
	return xs
}

// auxGraph builds Gansner et al. 1993 section 4.2's auxiliary graph for one
// component, and reports the simplex node count and the local id of every node
// of the component (-1 for a node in another component).
//
// The trick, in one paragraph, because it is not obvious from the edge list.
// What the coordinate pass wants to minimise is a sum of ABSOLUTE differences
// - |y(u) - y(v)| over the steps of every route - and the simplex minimises a
// sum of signed lengths. So each step (u,v) gets a node of its own, n_e, with
// two edges n_e -> u and n_e -> v of minimum length zero. Both edges constrain
// n_e to sit at or below both ends, and the cost w*(y(u)-y(n_e)) +
// w*(y(v)-y(n_e)) is smallest when n_e is exactly at the higher of the two, at
// which point it equals w*|y(u)-y(v)|. The absolute value falls out of the
// graph rather than out of the objective function.
//
// The separation edges are the other half: within one half-rank, each node is
// constrained to sit at least far enough below the one before it that their
// boxes cannot touch. They carry weight 0 because they are constraints and not
// preferences.
//
// Reusing the layering solver rather than writing a second algorithm is half
// the reason the network simplex was chosen at all - simplex.go's own comment
// has the Brandes-Kopf erratum this avoids - and it is why that file knows
// nothing about tables or ranks: this caller hands it a graph whose nodes are
// mostly not nodes of the drawing.
func auxGraph(g *graph, o order, heights []Coord) (edges []simplexEdge, n int, local []int) {
	local = make([]int, len(g.nodes))
	for v := range local {
		local[v] = -1
	}
	for _, layer := range o.layers {
		for _, v := range layer {
			local[v] = n
			n++
		}
	}

	// One aux node per route step, walked in half-rank order and then in the
	// order of the layer, so the aux graph is a function of the order alone
	// and two runs build the same edge list. o.next is already the routes
	// broken into single-half-rank steps, which is exactly what a bend is
	// measured between.
	for _, layer := range o.layers {
		for _, v := range layer {
			for _, w := range o.next[v] {
				weight := omega(g.nodes[v].kind, g.nodes[w].kind)
				aux := n
				n++
				edges = append(edges,
					simplexEdge{from: aux, to: local[v], weight: weight},
					simplexEdge{from: aux, to: local[w], weight: weight},
				)
			}
		}
	}

	for _, layer := range o.layers {
		for i := 0; i+1 < len(layer); i++ {
			a, b := layer[i], layer[i+1]
			edges = append(edges, simplexEdge{
				from: local[a],
				to:   local[b],
				// The simplex counts in ints and the drawing counts in Coord.
				// The conversion is safe by magnitude and not by luck: a
				// separation is a few hundred tenths of a pixel and their sum
				// down one half-rank is the height of the drawing, while the
				// smallest int the Go spec allows holds 2^31 tenths, which is
				// 214 million pixels - a diagram that would need something
				// like ten million rows of text to reach.
				minLen: int(separation(g, heights, a, b)),
				weight: 0,
			})
		}
	}

	return edges, n, local
}

// assignY gives every node of one component the y of its TOP edge, normalised
// so the highest node in the component sits at 0.
func assignY(g *graph, o order, heights []Coord) []Coord {
	tops := make([]Coord, len(g.nodes))

	// A component of one node needs no solver, and it is the common case
	// rather than the degenerate one - assignRanks takes the same shortcut and
	// says why. The general path answers 0 for it too; this one says so
	// without building an auxiliary graph of one node and no edges.
	if len(o.layers) == 1 && len(o.layers[0]) == 1 {
		return tops
	}

	edges, n, local := auxGraph(g, o, heights)
	solved := rank(edges, n)

	anchors := make([]Coord, len(g.nodes))
	for _, layer := range o.layers {
		for _, v := range layer {
			anchors[v] = Coord(solved[local[v]])
		}
	}
	balanceY(g, o, heights, anchors)

	var low Coord
	first := true
	for _, layer := range o.layers {
		for _, v := range layer {
			above, _ := anchorOffsets(g.nodes[v].kind, heights[v])
			tops[v] = anchors[v] - above
			if first || tops[v] < low {
				low, first = tops[v], false
			}
		}
	}
	for _, layer := range o.layers {
		for _, v := range layer {
			tops[v] -= low
		}
	}
	return tops
}

// balanceY centres every node that has room to move without making any route
// longer.
//
// This is the balance step Gansner describes for the coordinate axis, and it
// is deliberately outside the solver: the paper's two balance steps differ
// between the layering use and this one - spreading nodes across ranks means
// nothing on an auxiliary graph - so a balance inside the solver would have to
// become a mode flag the moment the second caller arrived. simplex.go's
// balanceRanks says the same thing from the other side.
//
// What it is for: the solver returns AN optimum, and where the optimum is a
// range it returns one end of it. A parent with two children can sit anywhere
// between them for the same total cost, and the solver leaves it flush against
// one of them. Sitting in the middle is what a reader expects and what makes a
// diagram look laid out rather than settled.
//
// Why it cannot make the drawing worse, which matters more here than the
// improvement does: a node is only ever moved to a point that is feasible
// against its two neighbours in its own half-rank - the only constraints it
// takes part in - and only when the cost of its own routes there is no higher
// than where it already sits. The cost of the whole drawing is a sum over
// route steps and every step touching this node is in that comparison, so a
// move that does not raise the node's own cost cannot raise the total. The
// comparison is a line of code rather than an argument on purpose: the
// convexity argument that says the midpoint is always cost-neutral is correct
// and is exactly the kind of correct that stops being true when someone
// changes the weights.
//
// One sweep, in half-rank order and then in layer order, reading positions as
// it goes. A second sweep would find a little more; it would also make the
// answer depend on the sweep count, which is the property this package spends
// everything else to avoid.
func balanceY(g *graph, o order, heights []Coord, anchors []Coord) {
	for _, layer := range o.layers {
		for i, v := range layer {
			// Boxes only. A label node and a virtual node ARE the route line,
			// and the solver has already put them where the route bends least;
			// the range they are free to move in is flat because the objective
			// counts total vertical travel and not the number of corners, so
			// moving one to the middle of that range turns a route with one
			// bend into a staircase with two at exactly the same cost. A box
			// is on no route line, so its freedom is worth spending.
			if !isReal(g.nodes[v].kind) {
				continue
			}
			neighbours := slices.Concat(o.prev[v], o.next[v])
			if len(neighbours) == 0 {
				continue
			}

			// The feasible interval, from the two nodes this one is stacked
			// between. Nothing else in the drawing constrains it: the
			// separation edges of one half-rank form a chain, so a node that
			// clears its two neighbours clears the whole layer.
			lo, hi := anchors[v], anchors[v]
			hasLo, hasHi := false, false
			if i > 0 {
				lo, hasLo = anchors[layer[i-1]]+separation(g, heights, layer[i-1], v), true
			}
			if i+1 < len(layer) {
				hi, hasHi = anchors[layer[i+1]]-separation(g, heights, v, layer[i+1]), true
			}
			clamp := func(y Coord) Coord {
				if hasLo {
					y = max(y, lo)
				}
				if hasHi {
					y = min(y, hi)
				}
				return y
			}

			// The cost is piecewise linear in this node's anchor and bends
			// only where a neighbour sits, so every position worth trying is a
			// neighbour's position pulled into the feasible interval. The
			// range of positions that tie for the lowest cost is what the node
			// is free to move in, and its middle is where it goes.
			cost := func(y Coord) Coord {
				var total Coord
				for _, w := range neighbours {
					weight := Coord(omega(g.nodes[v].kind, g.nodes[w].kind))
					total += weight * max(y-anchors[w], anchors[w]-y)
				}
				return total
			}

			var best, bestLow, bestHigh Coord
			first := true
			for _, w := range neighbours {
				y := clamp(anchors[w])
				switch c := cost(y); {
				case first || c < best:
					best, bestLow, bestHigh, first = c, y, y, false
				case c == best:
					bestLow, bestHigh = min(bestLow, y), max(bestHigh, y)
				}
			}

			if middle := (bestLow + bestHigh) / 2; cost(middle) <= cost(anchors[v]) {
				anchors[v] = middle
			}
		}
	}
}

// positionComponent gives every node of one component a rectangle, in its own
// coordinate space with its top-left corner at the origin. Placing one
// component beside another is packing, and packing is a later pass.
//
// The axis mapping, once and plainly: a half-rank is a COLUMN, so ranks run
// along x, and the ordering within a half-rank runs down y. That follows from
// ranks increasing from the child to the table it references with rank 0
// drawn leftmost (D07), which is what `jjf export dot` already draws with
// rankdir=LR - so a reader who has seen one of the two diagrams is not
// disoriented by the other.
//
// A virtual node comes out as a zero-width, zero-height rectangle at the point
// its route passes through, so the router can read a waypoint off it with no
// case analysis. A label node comes out as its whole band; the route runs
// along the bottom of that rectangle and the text is drawn in the strip at its
// top.
//
// # Y before X, with the channel plan between them
//
// The two axes are independent here and nothing said so out loud until the
// corridors needed a width: rankX reads only widths, assignY reads only
// heights. So every y in the drawing can be final while no x exists - and it
// has to be, because a corridor is as wide as the routes bending in it, and
// which routes bend, and over what y interval, is a fact about the y's alone.
// planCorridors is therefore run in the middle: after the pass that answers its
// question, before the pass that asks it.
//
// The channel plan comes back out with the rectangles because routing needs it
// too. It is one computation read by two passes rather than two computations of
// one number, for the reason Geometry's own comment gives about two
// measurements of one width.
func positionComponent(g *graph, o order, ranks []int, chains []chain, demands []demand) ([]Rect, corridors) {
	widths, heights := finalSizes(g, demands)
	tops := assignY(g, o, heights)
	plan := planCorridors(g, o, ranks, chains, widths, heights, tops)
	xs := rankX(o, widths, plan.count)

	rects := make([]Rect, len(g.nodes))
	for r, layer := range o.layers {
		for _, v := range layer {
			rects[v] = Rect{X: xs[r], Y: tops[v], W: widths[v], H: heights[v]}
		}
	}
	return rects, plan
}
