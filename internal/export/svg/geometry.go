package svg

import "github.com/shutx-net/jumping-json-flush/internal/export/erd"

// ---------------------------------------------------------------------------
// One component, as a rigid body
// ---------------------------------------------------------------------------

// block is one connected component with every per-component pass behind it: the
// nodes it owns, the chains its relationships were given, the rectangles they
// were all placed in and the routes drawn between them.
//
// It is the unit packing moves, and it moves as a RIGID BODY. That is why
// packing is the LAST pass rather than the first: every geometric property the
// per-component tests establish - no two boxes overlapping, no segment through
// a box it is not incident to, every endpoint exactly on its own boundary - is
// a statement about distances inside one component, and a rigid translation
// preserves all of them without re-checking anything. A packer that ran first
// would have to be told how big each component was going to be before its
// boxes had been sized, and a packer that nudged a route afterwards would have
// to re-establish everything routing had just proved.
//
// rects is indexed by node id and is meaningful only at this block's members.
// It is also SHORTER than the finished graph for every component but the last:
// insertVirtualNodes appends to g.nodes, so a component laid out early holds a
// slice sized for a graph that has grown since. Nothing indexes it outside its
// own members, which is what makes that safe rather than merely true today.
type block struct {
	members []int
	chains  []chain
	rects   []Rect
	routes  []route
}

// translate moves everything block b draws by delta: every node rectangle,
// every route point, both attachment points of every route, and the label
// rectangle of every route that carries its own.
//
// Rigid means exactly that list and nothing else - no re-routing, no nudging,
// no recomputing a lane or a slot. TestPackIsRigid checks it the way the word
// is defined, by comparing every pairwise distance inside one component before
// and after, rather than by checking the fields this function happens to touch.
func translate(b *block, delta Point) {
	for _, v := range b.members {
		b.rects[v] = b.rects[v].shift(delta)
	}
	for k := range b.routes {
		r := &b.routes[k]
		for i := range r.points {
			r.points[i] = r.points[i].shift(delta)
		}
		r.childAt = r.childAt.shift(delta)
		r.parentAt = r.parentAt.shift(delta)
		if r.hasOwnLabel() {
			r.labelRect = r.labelRect.shift(delta)
		}
	}
}

// bounds is the smallest rectangle covering every block's drawn geometry.
//
// The empty case answers the zero rectangle rather than accumulating from one:
// Rect.union treats both arguments as closed point sets, so a fold that started
// from a zero Rect would drag the origin into the answer, and there is no first
// rectangle to start from when there are no blocks. The schema requires at
// least one table, so a document with no components cannot be validated; the
// guard is here because a panic on it would be a worse answer than an empty
// page.
func bounds(blocks []block) Rect {
	if len(blocks) == 0 {
		return Rect{}
	}
	r := componentBounds(&blocks[0])
	for i := range blocks {
		r = r.union(componentBounds(&blocks[i]))
	}
	return r
}

// ---------------------------------------------------------------------------
// Geometry: the layer the invariants read
// ---------------------------------------------------------------------------

// Geometry is the finished layout in final page coordinates: rectangles,
// routes, attachment points and the page they sit on, with nothing left to
// decide.
//
// This is the seam, and it sits at ONE depth deliberately (D37). The invariant
// suite reads what is here and only what is here - rectangles, segments,
// endpoints, bounds - because all ten of its statements are about those, and it
// can make every one of them without knowing what a rank or a row height is.
// Scene construction needs more: it has to draw the inside of a box. So each
// node carries its measured Content as a payload Geometry never interprets and
// no invariant may read, and scene construction is its only reader.
//
// Re-measuring the text at drawing time instead was the alternative, and the
// reason it is the worse one is worth stating rather than assuming: two
// measurements are two places that can disagree about one width, and the box
// was SIZED from the first, so the second is the one that overflows its own
// cell. Carrying the first through means there is only ever one answer.
//
// Nodes is indexed by node id, so the tables come first in document order, then
// the stubs in first-reference order, then the label and virtual nodes each
// component inserted. Edges is indexed by relationship id, which is document
// order over the foreign keys - so "one drawn relationship per foreign key" is
// a statement about len(Edges) rather than a sum over the components. Labels is
// parallel to Edges today, because every relationship has exactly one name and
// is drawn with it; the Edge and Label indices are what make that a fact of the
// data a reader can check rather than a coincidence they have to rediscover.
type Geometry struct {
	Nodes  []GeoNode
	Labels []GeoLabel
	Edges  []GeoEdge
	Bounds Rect
}

// GeoNode is one node of the layout, in final coordinates.
//
// Every node is here, including the two kinds that draw no ink at all: a label
// node is the band its text sits in and a virtual node is a zero-size point in
// a corridor a route was given. They are here because the bounds have to cover
// them, and because an invariant about "a node" has to be able to say which
// nodes it means - Kind is how it says so.
//
// Content is the measured interior, and it is nil for a label and a virtual
// node, which have no interior to draw. Geometry never looks inside it; see the
// type comment for why it travels through here rather than being measured again
// where it is drawn.
type GeoNode struct {
	Rect    Rect
	Kind    nodeKind
	Name    string
	Content *content
}

// GeoLabel is one relationship's name and the rectangle it is drawn in.
//
// The two routing categories put it in two different places, and both arrive
// here as one flat list. An ordinary inter-rank relationship's label is the
// strip at the top of the band its label NODE reserved back in rank doubling; a
// staple's was placed by routeStaple, because rank doubling leaves a span-0
// relationship no intermediate half-rank to put a node in. One list rather than
// two is what makes "no label overlaps a box" and "no label overlaps another
// label" one check each instead of two checks that could come to disagree.
type GeoLabel struct {
	Rect Rect
	Text string
	Edge int
}

// GeoEdge is one relationship as drawn.
//
// Points is the polyline, child end first. ChildAt and ParentAt are those same
// two ends named, because the drawing has to know which end is which: the
// crow's foot goes on the child end whichever direction the route runs, and a
// relationship the cycle breaker reversed runs right to left. ChildSide and
// ParentSide are the faces the two ends touch, so the glyph's direction is read
// off the side policy rather than guessed from a coordinate comparison.
//
// ChildEnd and ParentEnd are erd's answers about the document - many or one,
// optional or mandatory - carried through untouched. Nothing here derives them
// and nothing may swap them: a swap moves the crow's foot to the other end and
// describes a different database, and not one geometry invariant would notice,
// because the drawing stays perfectly legal.
type GeoEdge struct {
	Points []Point

	ChildAt, ParentAt     Point
	ChildSide, ParentSide side
	ChildEnd, ParentEnd   erd.End

	Child, Parent int
	Label         int
}

// newGeometry gathers the packed blocks into the one value the drawing and the
// invariants read. page is the finished Bounds, which moveToMargin computed.
//
// The two walks below fill every entry of both slices, and neither needs a
// found-or-not check to say so: the components are a partition of the nodes, so
// every node is a member of exactly one block, and routeComponent draws every
// relationship of the component it is handed, so every relationship has exactly
// one route. Both of those are asserted - TestLayoutNodeCount and
// TestEveryEdgeHasExactlyOneRoute - rather than defended against here.
func newGeometry(g *graph, blocks []block, page Rect) Geometry {
	geo := Geometry{
		Nodes:  make([]GeoNode, len(g.nodes)),
		Labels: make([]GeoLabel, 0, len(g.edges)),
		Edges:  make([]GeoEdge, len(g.edges)),
		Bounds: page,
	}

	// labelNode is the node carrying relationship i's name, or -1 when the name
	// is not on a node. Having a chain and being an ordinary inter-rank
	// relationship are the same thing - insertVirtualNodes builds a chain for
	// exactly those - so this is the routing category, read off the data rather
	// than classified again from ranks the packed blocks no longer carry.
	labelNode := make([]int, len(g.edges))
	for i := range labelNode {
		labelNode[i] = -1
	}
	drawn := make([]*route, len(g.edges))

	for b := range blocks {
		for _, v := range blocks[b].members {
			n := &g.nodes[v]
			geo.Nodes[v] = GeoNode{
				Rect:    blocks[b].rects[v],
				Kind:    n.kind,
				Name:    n.name,
				Content: n.content,
			}
		}
		for _, c := range blocks[b].chains {
			labelNode[c.edge] = c.nodes[0]
		}
		for k := range blocks[b].routes {
			r := &blocks[b].routes[k]
			drawn[r.edge] = r
		}
	}

	for i := range g.edges {
		e := &g.edges[i]
		r := drawn[i]

		geo.Edges[i] = GeoEdge{
			Points:     r.points,
			ChildAt:    r.childAt,
			ParentAt:   r.parentAt,
			ChildSide:  r.childSide,
			ParentSide: r.parentSide,
			ChildEnd:   e.childEnd,
			ParentEnd:  e.parentEnd,
			Child:      e.child,
			Parent:     e.parent,
			Label:      len(geo.Labels),
		}

		// One label per relationship, from whichever of the two places holds
		// it. A label node's rectangle is stored relative to its own top-left
		// corner - the band is that rectangle plus labelGap below it, and the
		// route runs along the band's bottom edge - so it is read back through
		// the node's finished position rather than recomputed from the band.
		if v := labelNode[i]; v >= 0 {
			n := &g.nodes[v]
			geo.Labels = append(geo.Labels, GeoLabel{
				Rect: n.labelRect.shift(Point{X: geo.Nodes[v].Rect.X, Y: geo.Nodes[v].Rect.Y}),
				Text: n.label,
				Edge: i,
			})
			continue
		}
		geo.Labels = append(geo.Labels, GeoLabel{Rect: r.labelRect, Text: e.label, Edge: i})
	}

	return geo
}
