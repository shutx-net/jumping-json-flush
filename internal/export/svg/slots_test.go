package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// demandOf runs every stage up to and including this one over doc's first
// component, which is what a caller does and therefore what the counting
// assertions below are written against.
func demandOf(doc *model.Document) (laidOut, []demand) {
	l := layOut(doc)
	return l, slotDemand(l.g, l.members, l.ranks)
}

// handDemand counts the demand of doc's first component against ranks written
// out by hand.
//
// The two special categories need it: layering gives every relationship a
// minimum length of one rank, so no document can put both ends of a
// relationship on one rank, and a self-reference never reaches the ranking at
// all. Writing the ranks is the honest way to count a category the pipeline
// cannot currently produce - the same thing TestClassifyIsExhaustive does, for
// the same reason.
func handDemand(doc *model.Document, ranks []int) (*graph, []demand) {
	g := buildGraph(doc)
	return g, slotDemand(g, g.components[0], ranks)
}

func TestSlotDemandInterRank(t *testing.T) {
	// 0 parent, 1..3 its children.
	l, demands := demandOf(document(
		linked("parent"),
		linked("a", "parent"),
		linked("b", "parent"),
		linked("c", "parent"),
	))

	if got, want := demands[0], (demand{left: 3}); got != want {
		t.Errorf("the parent's demand = %+v, want %+v", got, want)
	}
	for _, child := range []int{1, 2, 3} {
		if got, want := demands[child], (demand{right: 1}); got != want {
			t.Errorf("%s's demand = %+v, want %+v", l.g.nodes[child].name, got, want)
		}
	}
}

// TestSlotDemandParallelEdges is the case the whole pass exists for: two
// relationships between one pair of tables, which without slots would leave
// the child at the same point along the same first segment and draw both
// crow's feet on top of each other.
func TestSlotDemandParallelEdges(t *testing.T) {
	_, demands := demandOf(document(
		linked("parent"),
		linked("child", "parent", "parent"),
	))

	if got, want := demands[0], (demand{left: 2}); got != want {
		t.Errorf("the parent's demand = %+v, want %+v", got, want)
	}
	if got, want := demands[1], (demand{right: 2}); got != want {
		t.Errorf("the child's demand = %+v, want %+v", got, want)
	}
}

func TestSlotDemandSameRank(t *testing.T) {
	// Both ends on rank 0, which is the state layering cannot reach.
	g, demands := handDemand(document(linked("a", "b"), linked("b")), []int{0, 0})

	for v := range g.nodes {
		if got, want := demands[v], (demand{top: 1}); got != want {
			t.Errorf("%s's demand = %+v, want %+v", g.nodes[v].name, got, want)
		}
	}
}

func TestSlotDemandSelfLoop(t *testing.T) {
	// A self-reference takes two slots because both of its ends attach to the
	// top side and both of its ends are the same box - no special case in the
	// counting, which is the point.
	_, one := demandOf(document(linked("categories", "categories")))
	if got, want := one[0], (demand{top: 2}); got != want {
		t.Errorf("one self-reference: demand = %+v, want %+v", got, want)
	}

	_, two := demandOf(document(linked("categories", "categories", "categories")))
	if got, want := two[0], (demand{top: 4}); got != want {
		t.Errorf("two self-references: demand = %+v, want %+v", got, want)
	}
}

func TestSlotDemandMixed(t *testing.T) {
	// 0 hub with a self-reference, 1 and 2 pointing at it from the rank
	// before, 3 pointing at it from its own rank. The hub therefore carries
	// two incoming inter-rank attachments on its left, one same-rank
	// attachment on its top and two more for the self-reference.
	g, demands := handDemand(document(
		linked("hub", "hub"),
		linked("a", "hub"),
		linked("b", "hub"),
		linked("c", "hub"),
	), []int{2, 0, 0, 2})

	if got, want := demands[0], (demand{left: 2, top: 3}); got != want {
		t.Errorf("the hub's demand = %+v, want %+v", got, want)
	}
	if got, want := demands[3], (demand{top: 1}); got != want {
		t.Errorf("%s's demand = %+v, want %+v", g.nodes[3].name, got, want)
	}
}

func TestSlotExtent(t *testing.T) {
	tests := []struct {
		slots int
		want  Coord
	}{
		// No attachment needs no room, which is not the same statement as one
		// attachment needing none: a single slot sits at the side's midpoint
		// and still wants a margin's clearance at each end, so the side has to
		// be two margins long.
		{0, 0},
		{1, 2 * slotMargin},
		{2, 2*slotMargin + slotSpacing},
		{3, 2*slotMargin + 2*slotSpacing},
	}

	for _, tt := range tests {
		if got := slotExtent(tt.slots); got != tt.want {
			t.Errorf("slotExtent(%d) = %d, want %d", tt.slots, got, tt.want)
		}
	}
}

// TestSlotAtCentresOnTheSide pins the arithmetic
// TestRouteStaysStraightWhenAnchorsAlign rests on. One slot lands on the
// midpoint of its side - which for a box is exactly the anchor coordinate
// assignment aligned, and therefore what makes the one-relationship case come
// out straight - and several slots are symmetric about that midpoint rather
// than crowded against one end.
//
// The rectangle is bigger than slotExtent(3) on both axes, which is what
// finalSize guarantees for a box carrying three attachments on a side, so the
// clearance assertion is checking the same fit slotExtent promises rather than
// a property of a rectangle picked to make it hold.
func TestSlotAtCentresOnTheSide(t *testing.T) {
	const slots = 3
	r := Rect{X: 1000, Y: 2000, W: slotExtent(slots) + rowHeight, H: slotExtent(slots) + rowHeight}

	single := []struct {
		s    side
		want Point
	}{
		{sideLeft, Point{X: r.X, Y: r.Y + r.H/2}},
		{sideRight, Point{X: r.Right(), Y: r.Y + r.H/2}},
		{sideTop, Point{X: r.X + r.W/2, Y: r.Y}},
	}
	for _, tt := range single {
		if got := slotAt(r, tt.s, 0, 1); got != tt.want {
			t.Errorf("slotAt(side %d, slot 0 of 1) = %+v, want the side's midpoint %+v", tt.s, got, tt.want)
		}
	}

	for _, s := range []side{sideLeft, sideRight, sideTop} {
		run := r.H
		if s == sideTop {
			run = r.W
		}

		offsets := make([]Coord, 0, slots)
		for i := range slots {
			p := slotAt(r, s, i, slots)
			if s == sideTop {
				offsets = append(offsets, p.X-r.X)
			} else {
				offsets = append(offsets, p.Y-r.Y)
			}
		}

		if want := run / 2; offsets[1] != want {
			t.Errorf("side %d: the middle of %d slots is at %d, want the side's midpoint %d",
				s, slots, offsets[1], want)
		}
		for i := 1; i < slots; i++ {
			if got := offsets[i] - offsets[i-1]; got != slotSpacing {
				t.Errorf("side %d: slots %d and %d are %d apart, want slotSpacing %d", s, i-1, i, got, slotSpacing)
			}
		}
		if offsets[0] < slotMargin || offsets[slots-1] > run-slotMargin {
			t.Errorf("side %d: %d slots at %v on a side %d long, want at least slotMargin %d clear at each end",
				s, slots, offsets, run, slotMargin)
		}
	}
}

func TestFinalSizeContentWins(t *testing.T) {
	n := node{kind: kindTable, content: measureTable(ordersTable())}
	w, h := finalSize(&n, demand{left: 1, right: 1})

	if w != n.content.width || h != n.content.height {
		t.Errorf("finalSize = %dx%d, want the content size %dx%d",
			w, h, n.content.width, n.content.height)
	}
}

// TestFinalSizeRoutingWins is the hub-overflow case the issue #32 discussion
// asked to be settled: a three-column lookup table referenced by fifteen
// others. A Go literal rather than a fixture because the assertion is
// arithmetic about one box, and a fixture would put a whole layout between the
// two.
func TestFinalSizeRoutingWins(t *testing.T) {
	const incoming = 15
	n := node{kind: kindTable, content: measureTable(&model.Table{
		Name: "currency", LogicalName: "通貨",
		Columns: []model.Column{
			{Name: "id", LogicalName: "ID", Type: "BIGINT"},
			{Name: "code", LogicalName: "コード", Type: "CHAR", Length: ptr(3)},
			{Name: "name", LogicalName: "名称", Type: "VARCHAR", Length: ptr(64)},
		},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
	})}

	// The content height is the header band plus three rows, which is what
	// makes this the small box the surplus case is about.
	if want := 2*lineHeight + 2*cellPadV + 3*rowHeight; n.content.height != want {
		t.Fatalf("the lookup's content height = %d, want %d", n.content.height, want)
	}

	w, h := finalSize(&n, demand{left: incoming})
	if want := 2*slotMargin + (incoming-1)*slotSpacing; h != want {
		t.Errorf("height = %d, want the slot extent %d", h, want)
	}
	if w != n.content.width {
		t.Errorf("width = %d, want the content width %d unchanged", w, n.content.width)
	}
	if surplus := h - n.content.height; surplus != slotExtent(incoming)-n.content.height {
		t.Errorf("surplus = %d, want %d", surplus, slotExtent(incoming)-n.content.height)
	}
}

// TestFinalSizeTopDemandWidensRatherThanHeightens pins the other axis: the two
// special categories attach to the top, so their demand is spent on width.
func TestFinalSizeTopDemandWidensRatherThanHeightens(t *testing.T) {
	// Four self-references, two slots each.
	const slots = 8
	n := node{kind: kindTable, content: measureTable(&model.Table{
		Name: "tag", LogicalName: "tag",
		Columns:    []model.Column{{Name: "id", LogicalName: "t", Type: "INT"}},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
	})}

	// The case is only about the top demand if the box is narrower than the
	// slots need; asserting that rather than assuming it keeps the test about
	// what it says it is about.
	if n.content.width >= slotExtent(slots) {
		t.Fatalf("the tag box is %d wide, which is not narrower than the slot extent %d",
			n.content.width, slotExtent(slots))
	}

	w, h := finalSize(&n, demand{top: slots})
	if want := slotExtent(slots); w != want {
		t.Errorf("width = %d, want the slot extent %d", w, want)
	}
	if h != n.content.height {
		t.Errorf("height = %d, want the content height %d unchanged", h, n.content.height)
	}
}

// TestContentIsTopAligned is the assertion behind D10: the surplus finalSize
// added is empty space at the BOTTOM of the box, so the content starts at the
// top edge and a row of boxes reads along a common top line.
func TestContentIsTopAligned(t *testing.T) {
	const incoming = 15
	n := node{kind: kindTable, content: measureStub("shipments")}

	w, h := finalSize(&n, demand{left: incoming})
	box := Rect{W: w, H: h}
	if got := contentOffsetY(box, n.content); got != 0 {
		t.Fatalf("contentOffsetY = %d, want 0", got)
	}

	surplus := h - n.content.height
	if want := slotExtent(incoming) - n.content.height; surplus != want {
		t.Fatalf("surplus = %d, want %d", surplus, want)
	}
	if bottom := contentOffsetY(box, n.content) + n.content.height; bottom != h-surplus {
		t.Errorf("the content's bottom edge is at %d, want %d - the surplus %d above the box's",
			bottom, h-surplus, surplus)
	}
}

func TestSlotDemandIsDeterministic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			_, first := demandOf(tt.doc)
			_, second := demandOf(tt.doc)
			if !reflect.DeepEqual(first, second) {
				t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
			}
		})
	}
}
