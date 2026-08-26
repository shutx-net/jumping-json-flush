package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
)

// drawnRects is everything the finished drawing puts on the page, as
// rectangles: every node rectangle, every label rectangle, and every route
// point as a zero-size rectangle.
//
// It is the same set componentBounds accumulates, which is what makes "the least
// drawn coordinate is exactly the margin" a statement that the bounds are TIGHT
// rather than merely large enough - a bounds that were too big would pass an
// inside-the-page check and fail this one.
func drawnRects(geo Geometry) []Rect {
	out := make([]Rect, 0, len(geo.Nodes)+len(geo.Labels))
	for _, n := range geo.Nodes {
		out = append(out, n.Rect)
	}
	for _, l := range geo.Labels {
		out = append(out, l.Rect)
	}
	for _, e := range geo.Edges {
		for _, p := range e.Points {
			out = append(out, at(p))
		}
	}
	return out
}

// leastDrawn is the top-left corner of everything drawn.
func leastDrawn(geo Geometry) Point {
	rects := drawnRects(geo)
	least := Point{X: rects[0].X, Y: rects[0].Y}
	for _, r := range rects {
		least = Point{X: min(least.X, r.X), Y: min(least.Y, r.Y)}
	}
	return least
}

func TestBoundsStartAtOrigin(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			if geo.Bounds.X != 0 || geo.Bounds.Y != 0 {
				t.Errorf("Bounds = %+v, want an origin of 0 0: the viewBox starts there and no coordinate in the file may be negative", geo.Bounds)
			}
			for _, r := range drawnRects(geo) {
				if r.X < margin || r.Y < margin {
					t.Errorf("%+v starts before the margin %d", r, margin)
				}
				if r.Right() > geo.Bounds.W-margin || r.Bottom() > geo.Bounds.H-margin {
					t.Errorf("%+v ends past the page %+v less its margin %d", r, geo.Bounds, margin)
				}
			}
			if least := leastDrawn(geo); least.X != margin || least.Y != margin {
				t.Errorf("the drawing starts at %+v, want exactly the margin %d on both axes", least, margin)
			}
		})
	}
}

// TestBoundsIncludeRoutePoints is the whole-drawing form of the componentBounds
// case: a table in the TOP rank with a self-reference puts its staple, and the
// staple's label, above every box in the drawing. Bounds taken from the node
// rectangles alone would have moved the topmost BOX to the margin and left the
// lane off the page.
func TestBoundsIncludeRoutePoints(t *testing.T) {
	// a carries a self-reference and a relationship to b, so a is ranked first
	// and drawn leftmost, and the self-reference is the document's first
	// foreign key.
	geo := layout(document(linked("a", "a", "b"), linked("b")))

	if geo.Bounds.Y != 0 {
		t.Fatalf("Bounds = %+v, want an origin of 0 0", geo.Bounds)
	}

	lane := geo.Edges[0].Points[1].Y
	var boxes Rect
	for i, n := range geo.Nodes {
		if i == 0 {
			boxes = n.Rect
			continue
		}
		boxes = boxes.union(n.Rect)
	}
	if lane >= boxes.Y {
		t.Fatalf("the staple runs at y %d and the topmost node rectangle is at %d; this document no longer draws anything above its boxes",
			lane, boxes.Y)
	}

	if lane <= 0 || lane >= geo.Bounds.H {
		t.Errorf("the staple's lane is at y %d, outside the page %+v", lane, geo.Bounds)
	}
	if least := leastDrawn(geo); least.Y != margin {
		t.Errorf("the drawing starts at y %d, want the margin %d", least.Y, margin)
	}
}

// TestLayoutIsDeterministic is the gate everything downstream of here depends
// on: from phase 12 the golden files are the bytes this value serialises to, so
// a layout that answered differently on the second run would fail as a golden
// mismatch on a document nobody had touched.
func TestLayoutIsDeterministic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			if first, second := layout(tt.doc), layout(tt.doc); !reflect.DeepEqual(first, second) {
				t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
			}
		})
	}

	// And one multi-component document, because packing is the one pass the
	// documents above barely exercise and the one whose order is decided by a
	// comparator rather than by the graph.
	t.Run("several components", func(t *testing.T) {
		doc := componentsDocument(3, 1, 1, 5, 2)
		if first, second := layout(doc), layout(doc); !reflect.DeepEqual(first, second) {
			t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
		}
	})
}

func TestLayoutNodeCount(t *testing.T) {
	// customers is referenced, coupons is not defined and gets a stub, and
	// categories references itself - so the document reaches three of the four
	// node kinds by itself and the fourth through the two-rank relationship.
	doc := document(
		linked("customers"),
		linked("orders", "customers", "coupons"),
		linked("order_lines", "orders"),
		linked("categories", "categories"),
	)
	geo := layout(doc)

	var byKind [kindVirtual + 1]int
	for _, n := range geo.Nodes {
		byKind[n.Kind]++
	}

	if got, want := byKind[kindTable], len(doc.Tables); got != want {
		t.Errorf("%d table node(s), want %d", got, want)
	}
	if got, want := byKind[kindStub], len(erd.UndefinedTargets(doc)); got != want {
		t.Errorf("%d stub node(s), want %d", got, want)
	}

	// One label node per ordinary relationship, and every relationship that is
	// not a self-reference is one: a same-rank relationship would be the other
	// exception, and no document can produce one, because every layering
	// relationship carries a minimum length of one rank.
	//
	// The virtual node count is deliberately not pinned here. It depends on how
	// many half-ranks each relationship crosses, which is the ranking's answer
	// and not this pass's; the chains are asserted where they are built.
	g := buildGraph(doc)
	if got, want := byKind[kindLabel], len(g.edges)-len(g.loops); got != want {
		t.Errorf("%d label node(s), want %d - one per relationship that is not a self-reference", got, want)
	}

	// The table nodes come first, in document order, which is what lets an edge
	// name its two ends as indices into Nodes.
	for i := range doc.Tables {
		if geo.Nodes[i].Kind != kindTable || geo.Nodes[i].Name != doc.Tables[i].Name {
			t.Errorf("Nodes[%d] is %q of kind %d, want the table %q", i, geo.Nodes[i].Name, geo.Nodes[i].Kind, doc.Tables[i].Name)
		}
	}
}

func TestLayoutEdgeCount(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			foreignKeys := 0
			for i := range tt.doc.Tables {
				foreignKeys += len(tt.doc.Tables[i].ForeignKeys)
			}
			if len(geo.Edges) != foreignKeys {
				t.Fatalf("%d drawn relationship(s) for %d foreign key(s)", len(geo.Edges), foreignKeys)
			}

			for i, e := range geo.Edges {
				if len(e.Points) < 2 {
					t.Errorf("relationship %d is drawn as %d point(s)", i, len(e.Points))
					continue
				}
				if e.Points[0] != e.ChildAt || e.Points[len(e.Points)-1] != e.ParentAt {
					t.Errorf("relationship %d runs %+v..%+v, want %+v..%+v",
						i, e.Points[0], e.Points[len(e.Points)-1], e.ChildAt, e.ParentAt)
				}
				// Packing is a translation, so an endpoint that was on its
				// box's boundary before it still is. Asserting it here is what
				// tells a later failure apart: if this passes and the invariant
				// suite fails, the fault is downstream of the translation.
				if !onBoundary(e.ChildAt, geo.Nodes[e.Child].Rect) {
					t.Errorf("relationship %d's child end %+v is not on %+v", i, e.ChildAt, geo.Nodes[e.Child].Rect)
				}
				if !onBoundary(e.ParentAt, geo.Nodes[e.Parent].Rect) {
					t.Errorf("relationship %d's parent end %+v is not on %+v", i, e.ParentAt, geo.Nodes[e.Parent].Rect)
				}
			}
		})
	}
}

// TestLayoutEveryRelationshipHasOneLabel pins the fact newGeometry's two arms
// rest on: every relationship has a name and is drawn with it, from one of the
// two places it can be held. Labels is therefore parallel to Edges, and the two
// indices are what make that checkable rather than assumed.
func TestLayoutEveryRelationshipHasOneLabel(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			if len(geo.Labels) != len(geo.Edges) {
				t.Fatalf("%d label(s) for %d relationship(s)", len(geo.Labels), len(geo.Edges))
			}
			for i, e := range geo.Edges {
				if e.Label < 0 || e.Label >= len(geo.Labels) {
					t.Errorf("relationship %d names label %d, which is not a label", i, e.Label)
					continue
				}
				label := geo.Labels[e.Label]
				if label.Edge != i {
					t.Errorf("relationship %d names label %d, which belongs to relationship %d", i, e.Label, label.Edge)
				}
				if label.Text == "" {
					t.Errorf("relationship %d's label is empty", i)
				}
				if label.Rect.W <= 0 || label.Rect.H <= 0 {
					t.Errorf("relationship %d's label rectangle is %+v", i, label.Rect)
				}
			}
		})
	}
}

// TestLayoutNoFKDocument is the nofk fixture's shape: two tables with no
// relationship between them, so the drawing is two components and nothing else.
//
// They come out STACKED rather than side by side, and that is the packer's rule
// rather than an accident. The target width is the widest single component's
// width, so two components of similar width can never share a shelf:
// w + componentGap + w exceeds max(w, w) for any positive gap. pack's comment
// has why no constant is introduced to change that.
func TestLayoutNoFKDocument(t *testing.T) {
	doc := document(linked("products"), linked("warehouses"))

	if g := buildGraph(doc); len(g.components) != 2 {
		t.Fatalf("%d component(s), want 2", len(g.components))
	}

	geo := layout(doc)
	if len(geo.Nodes) != 2 {
		t.Fatalf("%d node(s), want 2: two boxes and no placeholders", len(geo.Nodes))
	}
	if len(geo.Edges) != 0 || len(geo.Labels) != 0 {
		t.Errorf("%d relationship(s) and %d label(s), want none of either", len(geo.Edges), len(geo.Labels))
	}

	first, second := geo.Nodes[0].Rect, geo.Nodes[1].Rect
	if first.intersectsInterior(second) {
		t.Errorf("the two boxes %+v and %+v overlap", first, second)
	}
	if second.Y != first.Bottom()+componentGap || second.X != first.X {
		t.Errorf("the second box is at %+v, want (%d, %d) - one componentGap below the first",
			second, first.X, first.Bottom()+componentGap)
	}
	for _, n := range geo.Nodes {
		if !geo.Bounds.contains(Point{X: n.Rect.X, Y: n.Rect.Y}) || !geo.Bounds.contains(Point{X: n.Rect.Right(), Y: n.Rect.Bottom()}) {
			t.Errorf("%+v does not fit inside the page %+v", n.Rect, geo.Bounds)
		}
	}
}

func TestLayoutContentIsCarriedThrough(t *testing.T) {
	customers := linked("customers")
	geo := layout(document(customers, linked("orders", "customers")))

	for i, n := range geo.Nodes {
		switch n.Kind {
		case kindTable, kindStub:
			if n.Content == nil {
				t.Errorf("node %d %q is a box with no measured content", i, n.Name)
			}
		default:
			// A label node and a virtual node have no interior to draw, so
			// there is nothing for them to carry.
			if n.Content != nil {
				t.Errorf("node %d is of kind %d and carries content", i, n.Kind)
			}
		}
	}

	// The content is the measurement the box was SIZED from, unchanged - which
	// is the whole reason it travels through Geometry rather than being taken
	// again where it is drawn.
	if got, want := geo.Nodes[0].Content, measureTable(&customers); !reflect.DeepEqual(got, want) {
		t.Errorf("the customers box carries %+v, want the measurement it was sized from %+v", got, want)
	}
}
