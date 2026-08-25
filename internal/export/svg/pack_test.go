package svg

import (
	"reflect"
	"slices"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// chainedTables is n tables in a line - the first referencing the second and so
// on - which is the smallest way to build a component of a stated size. The
// names carry the prefix and the position, so document order and the intended
// component are both visible in a failure message.
func chainedTables(prefix string, n int) []model.Table {
	tables := make([]model.Table, 0, n)
	for i := range n {
		name := prefix + string(rune('a'+i))
		if i+1 == n {
			tables = append(tables, linked(name))
			continue
		}
		tables = append(tables, linked(name, prefix+string(rune('a'+i+1))))
	}
	return tables
}

// componentsDocument is a document whose components have exactly the given node
// counts, in the order the counts are given - which is what lets the packing
// order be asserted against a document rather than against a hand-built graph.
func componentsDocument(sizes ...int) *model.Document {
	var tables []model.Table
	for i, n := range sizes {
		tables = append(tables, chainedTables(string(rune('a'+i)), n)...)
	}
	return document(tables...)
}

// pairwiseOffsets is the offset between every pair of points block b draws
// something at: its node rectangles' corners, its route points and the corners
// of the label rectangles it owns.
//
// Offsets rather than distances, because a distance needs a square root and
// there is no floating point in this package (D06). Preserving every pairwise
// offset is the stronger statement in any case, and it is exactly what "moved as
// a rigid body" means - a check written against the definition rather than
// against the fields translate happens to touch.
func pairwiseOffsets(b *block) []Point {
	var points []Point
	for _, v := range b.members {
		points = append(points, Point{X: b.rects[v].X, Y: b.rects[v].Y})
	}
	for k := range b.routes {
		points = append(points, b.routes[k].points...)
		if b.routes[k].hasOwnLabel() {
			points = append(points, Point{X: b.routes[k].labelRect.X, Y: b.routes[k].labelRect.Y})
		}
	}

	offsets := make([]Point, 0, len(points)*(len(points)-1)/2)
	for i, p := range points {
		for _, q := range points[i+1:] {
			offsets = append(offsets, Point{X: q.X - p.X, Y: q.Y - p.Y})
		}
	}
	return offsets
}

func TestPackOrderBySizeThenDocument(t *testing.T) {
	// Components of 3, 1, 1 and 5 nodes in document order, so the answer has to
	// put the five-node one first, the three-node one next, and the two
	// singletons after them in the order the document gives.
	g := buildGraph(componentsDocument(3, 1, 1, 5))
	if len(g.components) != 4 {
		t.Fatalf("the document has %d component(s), want 4", len(g.components))
	}

	want := []int{3, 0, 1, 2}
	if got := packOrder(g); !slices.Equal(got, want) {
		t.Errorf("packOrder = %v, want %v", got, want)
	}
}

// TestPackOrderIsTotal is the case the second key decides. Two components of the
// same size is the normal case rather than the corner - on pagila 50 of the 71
// tables are single-node components, so the tie is 50-way - and the only order
// the document itself supplies is its own.
func TestPackOrderIsTotal(t *testing.T) {
	g := buildGraph(componentsDocument(1, 2, 1))

	want := []int{1, 0, 2}
	if got := packOrder(g); !slices.Equal(got, want) {
		t.Errorf("packOrder = %v, want %v: the two-node component first, then the singletons in document order", got, want)
	}
}

// TestPackShelvesWrap is the pagila shape in miniature: one wide component sets
// the target width and the singletons shelve across it until one does not fit.
//
// It calls pack directly on rectangles rather than laying a document out,
// because what is being asserted is the shelving arithmetic, and a document
// would put a whole layout between the boxes and the assertion. The sizes are
// arbitrary round numbers for the same reason coord_test.go's are - the
// shelving is scale-free, and stating them in terms of a spacing constant would
// mean that adjusting the spacing silently changed what the test asserts. Only
// componentGap, which IS the thing under test, comes from the block.
//
// The first box carries a non-zero origin on purpose: a component's coordinates
// are its own, so the translation has to be measured from where its bounding box
// actually is rather than assumed to start at zero.
func TestPackShelvesWrap(t *testing.T) {
	boxes := []Rect{
		{X: 50, Y: 70, W: 1000, H: 400},
		{W: 200, H: 100},
		{W: 200, H: 100},
		{W: 200, H: 300},
	}

	want := []Point{
		{X: -50, Y: -70},
		{Y: 400 + componentGap},
		{X: 200 + componentGap, Y: 400 + componentGap},
		{Y: 400 + componentGap + 100 + componentGap},
	}
	got := pack(boxes, []int{0, 1, 2, 3})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pack = %v, want %v", got, want)
	}

	// The two statements the arithmetic is there for: a new shelf starts one
	// componentGap below the tallest thing on the shelf above it, and two
	// components that fit within the target width share a shelf.
	if second, first := got[1].Y+boxes[1].Y, boxes[0].H; second != first+componentGap {
		t.Errorf("the second shelf is at y %d, want the first shelf's height %d plus componentGap %d",
			second, first, componentGap)
	}
	if got[1].Y != got[2].Y {
		t.Errorf("components 1 and 2 are on shelves at %d and %d, want them sharing one", got[1].Y, got[2].Y)
	}
}

func TestPackNoOverlap(t *testing.T) {
	g := buildGraph(componentsDocument(3, 1, 1, 5, 2))
	blocks := layOutComponents(g)
	packComponents(g, blocks)

	for i := range blocks {
		for j := i + 1; j < len(blocks); j++ {
			a, b := componentBounds(&blocks[i]), componentBounds(&blocks[j])
			if a.intersectsInterior(b) {
				t.Errorf("components %d %+v and %d %+v overlap", i, a, j, b)
			}
		}
	}
}

// TestPackIsRigid is what "packed as a rigid body" means, checked rather than
// asserted in prose: packing may move a component and may not reshape it. It is
// also why the per-component geometry tests are worth anything after this pass -
// every one of them is a statement about distances inside one component.
func TestPackIsRigid(t *testing.T) {
	g := buildGraph(componentsDocument(4, 1, 1))
	blocks := layOutComponents(g)

	before := make([][]Point, len(blocks))
	for i := range blocks {
		before[i] = pairwiseOffsets(&blocks[i])
	}

	packComponents(g, blocks)

	for i := range blocks {
		if after := pairwiseOffsets(&blocks[i]); !reflect.DeepEqual(before[i], after) {
			t.Errorf("component %d was reshaped by packing", i)
		}
	}
}

// TestComponentBoundsIncludeRoutePointsAndLabels is the bug componentBounds
// exists to avoid, at the level of one component: a staple runs in a lane above
// its half-rank and carries its label above the lane, so both sit outside every
// node rectangle in the component.
func TestComponentBoundsIncludeRoutePointsAndLabels(t *testing.T) {
	g := buildGraph(selfLoopDocument())
	blocks := layOutComponents(g)

	b := &blocks[0]
	var rectsOnly Rect
	for i, v := range b.members {
		if i == 0 {
			rectsOnly = b.rects[v]
			continue
		}
		rectsOnly = rectsOnly.union(b.rects[v])
	}

	full := componentBounds(b)
	if full.Y >= rectsOnly.Y {
		t.Fatalf("the component's bounds start at y %d and its rectangles at y %d; this document no longer draws anything above its boxes",
			full.Y, rectsOnly.Y)
	}

	label := b.routes[0].labelRect
	if !b.routes[0].hasOwnLabel() {
		t.Fatalf("the self-reference carries no label rectangle of its own")
	}
	if full.Y != label.Y {
		t.Errorf("the component's bounds start at y %d, want the staple's label rectangle at %d", full.Y, label.Y)
	}
	for _, p := range b.routes[0].points {
		if !full.contains(p) {
			t.Errorf("route point %+v is outside the component's bounds %+v", p, full)
		}
	}
}
