package svg

// The frozen geometry invariants, and the crossing ceilings that sit beside
// them.
//
// # Why this file exists
//
// Golden files in the shape internal/export/dot's take are necessary here and
// they are a weak net for a layout engine. A golden says the bytes changed; and
// after any change to the layout EVERY byte of all four goldens changes at once,
// so the diff a reviewer is handed is unreadable and none of it says the drawing
// is right. The answer is not a better diff. It is to assert the properties a
// correct drawing has, against the Geometry the layout produced - not against
// the SVG, which would mean re-parsing what was just written and re-deriving
// what the value already holds.
//
// Ten statements are frozen, and they are the ten in the plan's invariant set:
// nothing overlaps that should not, every routed segment is axis-aligned, every
// endpoint is exactly on its node's boundary, everything is inside the page, and
// the number of drawn relationships equals the number of foreign keys. They run
// over one shared table of documents, so a new document is one line and gets all
// ten checks.
//
// All ten hold as stated, and one of them did not until the commit that added
// corridor.go. "No two relationships drawn along the same line" held for
// horizontal segments and failed for vertical ones, because every route
// approaching a column turned on that column's boundary and so shared one line
// with every other route approaching it - 56 overlapping pairs on a
// fifteen-table hub, the longest sharing 268 px of line, measured at commit
// bd917e1 before this changed. Closing it
// cost the corridor between two columns its constant width: it is now a
// function of the routes that bend in it, which is why a change that looks like
// a routing detail reaches coordinate assignment. TestNoCollinearEdgeOverlap
// carries the argument, both halves of it, including the part that is measured
// rather than proved.
//
// Beside the set, and deliberately not inside it, sits a per-fixture ceiling on
// the ORDERING crossing count. TestCrossingCeiling says why it is a neighbour
// rather than a member.

import (
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// The documents every invariant runs over
// ---------------------------------------------------------------------------

// invariantDoc is one document in the shared table, with a name a failure can
// quote.
type invariantDoc struct {
	name string
	doc  *model.Document
}

// invariantDocuments is the table. It is the four fixtures plus the literal
// documents that cover the shapes no fixture has.
//
// The fixtures come through loadDoc, so they are schema-checked on the way in:
// a fixture that no longer conforms is not a document the CLI could reach this
// exporter with, and an invariant asserted over one proves nothing. The
// literals are here because a fixture cannot be justified for every shape - the
// hub is a box whose height comes from its attachment slots rather than from its
// text, and writing fifteen tables into JSON to say that would bury the point.
//
// TestInvariantDocumentTableCoversEveryCategory is the guard that keeps this
// list honest: it checks that the table really does contain each category the
// suite claims to cover, so that adding an invariant to a set that never sees a
// self-reference cannot look like coverage.
func invariantDocuments(t *testing.T) []invariantDoc {
	t.Helper()

	docs := make([]invariantDoc, 0, len(fixtures)+5)
	for _, name := range fixtures {
		docs = append(docs, invariantDoc{name: name, doc: loadDoc(t, name)})
	}

	return append(docs,
		invariantDoc{
			// The hub-overflow case, as a whole document rather than the one
			// box TestFinalSizeRoutingWins measures: fifteen tables pointing at
			// a three-column lookup, so the lookup's left side needs more room
			// than its own text does and the box is made taller. That is the one
			// way a box's size is not its content's size, and it is exactly the
			// case where a slot could be placed outside the box it belongs to.
			name: "a hub with more incoming relationships than its content is tall",
			doc:  hubDocument(15),
		},
		invariantDoc{
			// A cycle between two DISTINCT tables, which is what makes the
			// cycle breaker reverse an edge. cycle.json holds one too; this one
			// holds nothing else, so a failure here names the cycle rather than
			// one of four other things.
			name: "a two-table cycle and nothing else",
			doc:  document(linked("a", "b"), linked("b", "a")),
		},
		invariantDoc{
			// Two self-references on one table: four slots on one top side, two
			// staples in two different lanes. One self-reference cannot show
			// that the lanes are allocated rather than assumed.
			name: "a table with two self-references",
			doc:  document(selfReferencing("categories", "parent_id", "root_id")),
		},
		invariantDoc{
			// Two relationships between one pair of tables, told apart by
			// nothing but their labels. Both need their own slot at both ends
			// and their own label band.
			name: "two parallel relationships between one pair of tables",
			doc:  document(linked("parent"), linked("child", "parent", "parent")),
		},
		invariantDoc{
			// Several components, so that packing is inside the invariants
			// rather than beside them: every statement below is about one
			// drawing, and a rigid translation is what is supposed to preserve
			// them.
			name: "several components of different sizes",
			doc:  componentsDocument(3, 1, 1, 5, 2),
		},
		invariantDoc{
			// A layered mesh, which is the shape that puts verticals running
			// BOTH ways through one corridor - some descending, some ascending -
			// together with ordering crossings. That is the configuration
			// TestNoCollinearEdgeOverlap's horizontal residual needs, and the
			// hub cannot produce it: every route into a hub bends the same way
			// on the same side.
			name: "a layered mesh of five ranks",
			doc:  meshDocument(5, 4),
		},
	)
}

// meshDocument is layers half-ranks of width tables each, where every table
// references two of the layer after it - the one below it and the one after
// that, wrapping - so no two routes through a corridor bend alike.
func meshDocument(layers, width int) *model.Document {
	name := func(i, j int) string { return string(rune('a'+i)) + string(rune('a'+j)) }
	var tables []model.Table
	for i := range layers {
		for j := range width {
			if i+1 == layers {
				tables = append(tables, linked(name(i, j)))
				continue
			}
			tables = append(tables, linked(name(i, j), name(i+1, j), name(i+1, (j+1)%width)))
		}
	}
	return document(tables...)
}

// hubDocument is a three-column lookup table referenced by n others.
func hubDocument(n int) *model.Document {
	tables := []model.Table{{
		Name: "currency", LogicalName: "通貨",
		Columns: []model.Column{
			{Name: "id", LogicalName: "通貨ID", Type: "BIGINT"},
			{Name: "code", LogicalName: "通貨コード", Type: "CHAR", Length: ptr(3)},
			{Name: "name", LogicalName: "通貨名", Type: "VARCHAR", Length: ptr(64)},
		},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
	}}
	for i := range n {
		tables = append(tables, linked(string(rune('a'+i%26))+string(rune('a'+i/26)), "currency"))
	}
	return document(tables...)
}

// selfReferencing builds one table with one self-referencing foreign key per
// column named, which is how the table gets more than one self-reference
// without two foreign keys sharing a column.
func selfReferencing(name string, columns ...string) model.Table {
	t := model.Table{
		Name:        name,
		LogicalName: name,
		Columns:     []model.Column{bigint("id")},
		PrimaryKey:  &model.PrimaryKey{Columns: []string{"id"}},
	}
	for _, column := range columns {
		t.Columns = append(t.Columns, nullableBigint(column))
		t.ForeignKeys = append(t.ForeignKeys, fk("fk_"+name+"_"+column, column, name))
	}
	return t
}

// ---------------------------------------------------------------------------
// The obstacle sets
// ---------------------------------------------------------------------------

// INTERIOR, not closed, is the word every statement below is written in, and it
// is not a softening. Two boxes that share an edge exactly, and a route that
// runs along a box's side or along the bottom edge of the band its own label
// reserved, are legal and common drawings; a closed test would report all of
// them as collisions and the invariants would have to be weakened to tolerances
// to survive. Interior lets them be stated as "= 0".
//
// And because every coordinate here is an integer (D06), each one is EXACT.
// There is no epsilon to tune, no way for "touching the boundary" and "a hair
// inside it" to trade places, and therefore no way for one of these tests to
// flake.

// nodeRects is every node's rectangle, indexed by node id.
//
// All four kinds are in it. A virtual node's rectangle is zero-sized, and it is
// left in rather than filtered out: intersectsInterior answers false for an
// empty rectangle because an empty rectangle HAS no interior, so a virtual node
// falls out of every overlap statement on its own. Filtering here would say the
// same thing in a place where a reader could not check it.
func nodeRects(geo Geometry) []Rect {
	out := make([]Rect, len(geo.Nodes))
	for i, n := range geo.Nodes {
		out[i] = n.Rect
	}
	return out
}

// labelRects is every relationship label's rectangle, indexed by label id.
func labelRects(geo Geometry) []Rect {
	out := make([]Rect, len(geo.Labels))
	for i, l := range geo.Labels {
		out[i] = l.Rect
	}
	return out
}

// segmentsOf is one relationship's route as segments, in drawing order.
func segmentsOf(e GeoEdge) []Segment {
	out := make([]Segment, 0, max(0, len(e.Points)-1))
	for i := 0; i+1 < len(e.Points); i++ {
		out = append(out, Segment{A: e.Points[i], B: e.Points[i+1]})
	}
	return out
}

// incidentNode reports whether node v is one of relationship i's own ends.
//
// The plan's incidence list has four entries - the child node, the parent node,
// the relationship's own label and the virtual nodes of its own chain - and only
// the first two need to be written down. The other two need no exemption, which
// is worth stating because a reader will otherwise assume one is missing:
//
//   - A virtual node is zero-sized, so it cannot appear in an interior overlap
//     at all, whether it belongs to this relationship's chain or another's.
//   - A label node's rectangle is the BAND, and the route runs along the band's
//     bottom edge and turns at its side edges. Those are boundary points, not
//     interior ones. The label's own rectangle - the strip of text inside the
//     band - is a separate question, and incidentLabel is where it is answered.
//
// Getting this list too WIDE is the failure mode to watch: each entry excused
// here is a case the invariant stops checking, and an incidence set of
// "everything" makes the statement vacuous rather than false. Narrower than the
// plan's is the safe direction, and it is the direction this is.
func incidentNode(e GeoEdge, v int) bool { return v == e.Child || v == e.Parent }

// incidentLabel reports whether label j belongs to relationship i - which is
// the qualifier the label statement cannot be written without. Every
// relationship's route runs into the band its own label sits in, so an
// unqualified "no route crosses a label" would fail on every relationship in
// every diagram.
func incidentLabel(geo Geometry, i, j int) bool { return geo.Labels[j].Edge == i }

// isBox reports whether a node draws ink.
//
// The two placeholder kinds are the layout's own bookkeeping: a label node
// reserves the band its text sits in, a virtual node holds a corridor open, and
// neither puts anything on the page. Where a statement is about one drawn thing
// covering another - a label over a box - this is the set that means "drawn".
func isBox(kind nodeKind) bool { return kind == kindTable || kind == kindStub }

// ---------------------------------------------------------------------------
// The ten
// ---------------------------------------------------------------------------

// TestNoNodeOverlap: node x node interior overlap = 0.
//
// Every node, both kinds of box and both kinds of placeholder. The placeholders
// are in because that is what they are FOR: a label node's band is a node to the
// separation constraints, so a box overlapping one would mean a label drawn on
// top of a box, and the way to say "nothing else can land in the band" is to say
// nothing overlaps a node.
func TestNoNodeOverlap(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			rects := nodeRects(geo)

			for i := range rects {
				for j := i + 1; j < len(rects); j++ {
					if rects[i].intersectsInterior(rects[j]) {
						t.Errorf("node %d (%s, kind %d) %+v overlaps node %d (%s, kind %d) %+v",
							i, geo.Nodes[i].Name, geo.Nodes[i].Kind, rects[i],
							j, geo.Nodes[j].Name, geo.Nodes[j].Kind, rects[j])
					}
				}
			}
		})
	}
}

// TestNoEdgeThroughNonIncidentNode: edge x non-incident node interior = 0.
//
// A line through a box it has nothing to do with is the failure a reader notices
// first and the one an orthogonal router with no obstacle avoidance would make.
// It does not happen here because the corridors are RESERVED - a long
// relationship gets one virtual node per half-rank it crosses, and nothing else
// can be placed where a node is - which is the whole argument for spending the
// virtual nodes rather than writing a path planner.
//
// The other half of the argument is the strip BETWEEN two columns. rankX puts
// the next column exactly gapWidth past the widest node of this one, and every
// node in a half-rank is left-aligned at that half-rank's x, so the strip holds
// no node at all - which is what lets a route turn anywhere inside it, on any
// of the channels planCorridors handed out, with still no obstacle test in the
// router. interRankPoints has it in full.
func TestNoEdgeThroughNonIncidentNode(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			rects := nodeRects(geo)

			for i, e := range geo.Edges {
				for k, s := range segmentsOf(e) {
					for v := range rects {
						if incidentNode(e, v) {
							continue
						}
						if s.intersectsInterior(rects[v]) {
							t.Errorf("relationship %d (%s) segment %d %+v passes through node %d (%s, kind %d) %+v",
								i, geo.Labels[e.Label].Text, k, s, v, geo.Nodes[v].Name, geo.Nodes[v].Kind, rects[v])
						}
					}
				}
			}
		})
	}
}

// TestNoEdgeThroughNonIncidentLabel: edge x non-incident label interior = 0.
//
// A line drawn through somebody else's name is as unreadable as a line through a
// box. It holds for the same reason: a label node reserves a band of
// labelHeight + labelGap across the rank axis, the text occupies the strip at
// the band's top and the route runs along the band's bottom edge, so the strip
// is separated from every route by the gap and from every other route by the
// band being a node.
func TestNoEdgeThroughNonIncidentLabel(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			labels := labelRects(geo)

			for i, e := range geo.Edges {
				for k, s := range segmentsOf(e) {
					for j := range labels {
						if incidentLabel(geo, i, j) {
							continue
						}
						if s.intersectsInterior(labels[j]) {
							t.Errorf("relationship %d (%s) segment %d %+v passes through the label %q %+v",
								i, geo.Labels[e.Label].Text, k, s, geo.Labels[j].Text, labels[j])
						}
					}
				}
			}
		})
	}
}

// TestNoLabelOverNode: label x node interior overlap = 0.
//
// Against the BOXES, and that qualifier is the design rather than a weakening: a
// label rectangle lies inside the band its own label node reserved, so comparing
// labels against label nodes would report every label in every diagram. The band
// draws nothing. What the statement is about is one piece of ink covering
// another, and the ink is the boxes.
func TestNoLabelOverNode(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			for j, l := range geo.Labels {
				for v, n := range geo.Nodes {
					if !isBox(n.Kind) {
						continue
					}
					if l.Rect.intersectsInterior(n.Rect) {
						t.Errorf("the label %q (%d) %+v is drawn over the box %q (%d) %+v",
							l.Text, j, l.Rect, n.Name, v, n.Rect)
					}
				}
			}
		})
	}
}

// TestNoLabelOverLabel: label x label interior overlap = 0.
//
// Two names on top of each other is the failure mode of putting labels anywhere
// other than in the layout. It holds here because each one is a node with its
// own band, so the separation constraints keep them apart, and there is no
// placement pass that could put two in one place.
func TestNoLabelOverLabel(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			labels := labelRects(geo)

			for i := range labels {
				for j := i + 1; j < len(labels); j++ {
					if labels[i].intersectsInterior(labels[j]) {
						t.Errorf("the labels %q %+v and %q %+v overlap",
							geo.Labels[i].Text, labels[i], geo.Labels[j].Text, labels[j])
					}
				}
			}
		})
	}
}

// piece is one segment of one relationship, named so that a failure can say
// which relationship and which segment of it.
type piece struct {
	edge int
	at   int
	s    Segment
}

// pieces is every segment of every relationship in one drawing.
func pieces(geo Geometry) []piece {
	var out []piece
	for i, e := range geo.Edges {
		for k, s := range segmentsOf(e) {
			out = append(out, piece{edge: i, at: k, s: s})
		}
	}
	return out
}

// TestNoCollinearEdgeOverlap: collinear edge-segment overlap = 0.
//
// Two relationships drawn along the same line for any distance are one line to a
// reader, and no other invariant catches it: both are axis-aligned, neither is
// inside a box, and the drawing is otherwise perfectly legal. Every pair of
// distinct segments is compared, on both axes. Sharing a single point is not an
// overlap - collinearOverlap requires an interval of non-zero length - which is
// what lets the comparison be total: consecutive segments of a route meet at
// every corner.
//
// # Vertical: by construction
//
// Every vertical segment of an inter-rank route runs on a CHANNEL inside the
// corridor between two columns, and planCorridors gives two verticals the same
// channel only when overlapsMoreThanAPoint says their y intervals may share a
// line. That is this test's own predicate, not a copy of it: collinearOverlap
// asks exactly the same question of two vertical segments, so the allocator's
// rule and this assertion are one function and cannot drift apart. Two
// relationships approaching one column are therefore two lines. Verticals that
// are not inter-rank are a staple's two legs, which allocateLanes keeps apart
// by giving every staple over one half-rank its own lane.
//
// # Horizontal: it holds, and the proof it used to have is gone
//
// The old argument was a tiling one. Each horizontal run was confined to one
// cell [xs[r], xs[r+1]) and the cells tile the axis, so two runs at one y were
// either in one cell - impossible, since that needs two waypoints of two routes
// at one y in one half-rank, which the separation constraints forbid - or in
// different cells, where they can share at most the boundary point. Channels end
// a run INSIDE a corridor, so the tiling is gone and the narrower argument below
// is what is left. It is written out because a proof with a quiet hole in it is
// worth less than a named residual, which is the same choice this file already
// makes for TestCrossingCeiling.
//
// The configuration needed. Two horizontal runs at one y meeting inside the
// corridor after half-rank g needs one route's half-rank-g waypoint and another
// route's half-rank-(g+1) waypoint at EXACTLY the same y. Both routes must also
// BEND there: a route that runs straight through the corridor occupies that y in
// BOTH half-ranks, and then the other route's waypoint would be a second
// waypoint at one y in one half-rank, which cannot happen. Call the route whose
// half-rank-g waypoint is at y the one arriving from the LEFT; it turns at
// channel cL and its run covers the corridor from the left up to cL. The other
// arrives from the right and its run covers from cR to the right edge. They
// overlap exactly when cL > cR.
//
// Four sub-cases, by which end of each vertical's y interval the shared y is:
//
//   - The shared y is the LO of both intervals. Safe BY CONSTRUCTION. Both
//     verticals sort at the same lo, the descending-before-ascending key puts
//     the left-arriving one first, and it takes the lowest free channel; every
//     channel below that was occupied at that moment by an interval reaching
//     past y, so it is still occupied a moment later, and the channel just taken
//     is occupied too. The second one cannot land below the first.
//   - The shared y is the HI of both. Not excluded. The two intervals then
//     overlap, so they are on different channels, and which is lower is not
//     constrained; this configuration also requires the two routes to CROSS in
//     that corridor.
//   - The shared y is the LO of one and the HI of the other, either way round.
//     Not excluded. The two intervals touch at y and nothing else, so they may
//     share a channel - and when they do the two runs stop at the same x and
//     share one point, which is legal - but a third vertical placed in between
//     can push them apart in the wrong order.
//
// And one case no assignment can fix: two routes that EXCHANGE y across one
// corridor, A from y1 to y2 and B from y2 to y1. Then cA <= cB is required at
// y1 and cB <= cA at y2, so they must share a channel, and sharing it makes the
// two verticals themselves collinear over the whole interval.
//
// Measured: 0 overlapping pairs on both axes, over every document in the table
// below and over two larger hubs - 30 and 50 tables into one lookup. The
// configuration above occurred at all exactly twice, both on the fifty-table
// hub, both in the third sub-case, and both came out with the two verticals
// sharing one channel and therefore sharing one point. The answer if a real
// document ever produces the overlap is a recorded follow-up: channel routing
// with vertical constraints is NP-hard, and it is the path planner issue #32
// rejected wearing a different hat.
func TestNoCollinearEdgeOverlap(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			all := pieces(layout(tt.doc))

			for a := range all {
				for b := a + 1; b < len(all); b++ {
					if collinearOverlap(all[a].s, all[b].s) {
						t.Errorf("relationship %d segment %d %+v runs along relationship %d segment %d %+v",
							all[a].edge, all[a].at, all[a].s, all[b].edge, all[b].at, all[b].s)
					}
				}
			}
		})
	}
}

// TestEverySegmentAxisAligned: every segment of every routed edge is
// axis-aligned.
//
// The statement carries no "ordinary" qualifier, and that is the one total claim
// in the set. A self-reference and a same-rank relationship are drawn as
// orthogonal staples rather than as arcs (D19), so there is no curve anywhere in
// a Geometry - the optionality circle is the only curve in the drawing and it is
// not a routed segment.
//
// The consequence, so that nobody weakens this line casually: introducing an arc
// would mean qualifying it, and it would mean an arc-versus-rectangle
// intersection test, which is not exact in integers. Two of the invariants above
// would then need a tolerance, and a tolerance is a thing that can be tuned
// until a test passes.
func TestEverySegmentAxisAligned(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			for i, e := range geo.Edges {
				for k, s := range segmentsOf(e) {
					if !s.axisAligned() {
						t.Errorf("relationship %d (%s) segment %d %+v is diagonal",
							i, geo.Labels[e.Label].Text, k, s)
					}
				}
			}
		})
	}
}

// TestEndpointsOnBoundary: every relationship endpoint lies on its node's
// boundary.
//
// Exactly on it. An attachment point is computed from the box's own edges, so an
// endpoint a tenth of a pixel inside or outside is an arithmetic mistake and not
// a rounding artefact - there is nothing here to round. A point inside the box
// would draw the crow's foot under the border it is supposed to touch, and a
// point outside it would leave a visible gap between the line and the box.
func TestEndpointsOnBoundary(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			for i, e := range geo.Edges {
				if !onBoundary(e.ChildAt, geo.Nodes[e.Child].Rect) {
					t.Errorf("relationship %d's child end %+v is not on the boundary of node %d %+v",
						i, e.ChildAt, e.Child, geo.Nodes[e.Child].Rect)
				}
				if !onBoundary(e.ParentAt, geo.Nodes[e.Parent].Rect) {
					t.Errorf("relationship %d's parent end %+v is not on the boundary of node %d %+v",
						i, e.ParentAt, e.Parent, geo.Nodes[e.Parent].Rect)
				}
			}
		})
	}
}

// TestEverythingInsideBounds: all drawn geometry lies inside the final bounds.
//
// Inside is closed here, unlike everywhere else in this file, and for the reason
// Rect.contains gives: the question is whether a piece of ink is on the page,
// and ink exactly on the page's edge is on it.
//
// What is checked is what a Geometry holds: every node rectangle, every label
// rectangle and every point of every route. The crow's feet and the optionality
// circles are placed by scene construction, from these attachment points, and
// reach at most circleOffset + circleR outward - which is less than the
// narrowest corridor the drawing can contain, which is rankGap, so a glyph
// cannot be the thing that escapes a page these points are inside of.
func TestEverythingInsideBounds(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			inside := func(what string, r Rect) {
				if !geo.Bounds.contains(Point{X: r.X, Y: r.Y}) || !geo.Bounds.contains(Point{X: r.Right(), Y: r.Bottom()}) {
					t.Errorf("%s %+v is not inside the page %+v", what, r, geo.Bounds)
				}
			}

			for _, n := range geo.Nodes {
				inside("node "+n.Name, n.Rect)
			}
			for _, l := range geo.Labels {
				inside("the label "+l.Text, l.Rect)
			}
			for i, e := range geo.Edges {
				for k, p := range e.Points {
					if !geo.Bounds.contains(p) {
						t.Errorf("relationship %d point %d %+v is outside the page %+v", i, k, p, geo.Bounds)
					}
				}
			}
		})
	}
}

// TestRelationshipCountMatchesForeignKeys: the number of drawn relationships
// equals the number of foreign keys in the document.
//
// Every foreign key is drawn, with no exceptions to remember. A self-reference
// is drawn as a staple rather than as a line between two boxes, and it counts. A
// foreign key naming a table the document does not define is drawn to a stub,
// and it counts too - the stub is a real node precisely so that this stays true.
// Three foreign keys naming one undefined table share one stub and are still
// three relationships.
//
// This is the invariant that would catch an exporter that silently dropped one,
// which is the failure with no visible symptom: a diagram missing one line still
// looks like a diagram.
func TestRelationshipCountMatchesForeignKeys(t *testing.T) {
	for _, tt := range invariantDocuments(t) {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)

			want := 0
			for i := range tt.doc.Tables {
				want += len(tt.doc.Tables[i].ForeignKeys)
			}
			if got := len(geo.Edges); got != want {
				t.Errorf("%d drawn relationship(s) for %d foreign key(s)", got, want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Beside the set: the crossing ceiling
// ---------------------------------------------------------------------------

// orderingCrossings is the ordering crossing count for a whole document: the
// number of pairs of edge steps that cross between adjacent half-ranks, summed
// over every component.
//
// It is the prefix of layOutComponents up to ordering, and nothing after it,
// because the count this holds a ceiling on is the ORDERING one - the quantity
// median plus transpose minimises - and not a geometric count of segments that
// intersect in the finished drawing. A geometric count would move when lane
// assignment or slot assignment changed and would report that as an ordering
// regression.
func orderingCrossings(doc *model.Document) int {
	g := buildGraph(doc)

	total := 0
	for c := range g.components {
		members := g.components[c]
		breakCycles(g, members)
		ranks := assignRanks(g, members)
		members, ranks, chains := insertVirtualNodes(g, members, ranks)
		total += crossings(orderComponent(g, members, ranks, chains))
	}
	return total
}

// TestCrossingCeiling holds each fixture's ordering crossing count at or below a
// recorded number.
//
// # Why it is not one of the ten
//
// Because it is a quality ceiling and not a statement about correctness. With
// all ten invariants above passing you can delete median and transpose
// altogether and every one of them still passes: the virtual nodes reserve the
// corridors, so the edges stay in their lanes, stay axis-aligned, stay out of
// every box interior and stay on the page - and the diagram is spaghetti. The
// crossing count is the only thing that notices, and it is the ordering pass's
// only regression test. So it sits next to the frozen set rather than inside it.
//
// # Where the numbers come from
//
// MEASURED, by running orderingCrossings over each fixture at commit a0bf0f8
// (phase 12, the commit that added the fixtures and the goldens) and reading the
// value the Logf below printed. They are not predictions and not budgets, and
// none of them was adjusted to make anything pass - the first three guesses
// written here before the run were 0, 0 and 1, and the measurement replaced all
// of them:
//
//	full.json    1
//	edge.json    2
//	nofk.json    0
//	cycle.json   0
//
// nofk.json is the one number that is known in advance rather than measured: it
// has no foreign key at all, so there is no pair of steps to cross and any other
// answer would be a bug in the counter rather than in the layout.
//
// The other three are explained rather than merely recorded, because a number
// nobody can explain is a number nobody can defend. Each was located by printing
// the final order layer by layer and the crossing count at each boundary:
//
//   - full.json's 1 is at the last boundary, between the three label nodes
//     [fk_orders_customer, fk_orders_coupon, fk_customer_profiles_customer] and
//     the two boxes [customers, coupons]: fk_customer_profiles_customer reaches
//     back up to customers across fk_orders_coupon's descent to coupons. It is
//     NOT a lower bound - putting fk_orders_coupon first and coupons before
//     customers crosses nothing - and median plus transpose does not find that,
//     because getting there means swapping in two layers at once and transpose
//     accepts only a swap that strictly decreases the count on its own. A local
//     minimum, which is what a heuristic returns; D22 records that this heuristic
//     beat an exact per-layer minimiser on real schemas anyway.
//   - edge.json's 2 are both at the last boundary and both belong to one
//     relationship: fk_node_stats_unknown_column, which reaches from node_stats
//     across to the graph table, crossing the two relationships that run from
//     edge into the shared nodes stub.
//   - cycle.json's 0 is the interesting one: it holds a cycle and two
//     relationships spanning two ranks and still comes out crossing-free, which
//     is the ordering pass doing exactly what it is for.
//
// The assertion is <=, deliberately. An improvement to the ordering must not be
// a test failure - though it should come with the number here lowered, or the
// ceiling stops meaning anything.
//
// # The honest limit (K04)
//
// Four small fixtures. A regression that only shows up on a 71-table schema will
// not be caught here, because pagila is not jjf's to ship and cannot be added to
// this repository. cycle.json is deliberately the fixture with the most graph
// structure per node, which is the most this can be narrowed without inventing a
// schema nobody would write.
func TestCrossingCeiling(t *testing.T) {
	ceilings := map[string]int{
		"full.json":  1,
		"edge.json":  2,
		"nofk.json":  0,
		"cycle.json": 0,
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			ceiling, ok := ceilings[name]
			if !ok {
				t.Fatalf("no measured ceiling recorded for %s: measure it, do not guess it", name)
			}

			got := orderingCrossings(loadDoc(t, name))
			t.Logf("%s: %d crossing(s), ceiling %d", name, got, ceiling)
			if got > ceiling {
				t.Errorf("%s has %d ordering crossing(s), above the measured ceiling of %d", name, got, ceiling)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The guard on the table itself
// ---------------------------------------------------------------------------

// TestInvariantDocumentTableCoversEveryCategory checks the table, not the
// layout: an invariant suite that never sees a category proves nothing about it,
// and this is the cheapest way to keep that visible rather than a thing somebody
// has to remember.
//
// Six categories, and the plan's seventh is deliberately absent. It asked for a
// document with a SAME-RANK relationship, and no document can produce one:
// layer.go gives every layering edge a minimum length of one rank and the
// simplex enforces it, so the two ends of a relationship never land on one rank
// and the same-rank arm of the routing taxonomy is unreachable through the
// pipeline. A guard demanding one here would be unsatisfiable, and a fixture
// contorted into looking like it satisfied it would be a fiction. That category
// is covered where it can be: the hand-built-ranks tests in route_test.go and
// slots_test.go set the ranks directly, which is the only way to reach it.
func TestInvariantDocumentTableCoversEveryCategory(t *testing.T) {
	docs := invariantDocuments(t)

	categories := []struct {
		name string
		has  func(*model.Document) bool
	}{
		{"a self-reference", hasSelfReference},
		{"a relationship spanning more than one rank", hasLongRelationship},
		{"a cycle between distinct tables", hasCycle},
		{"two parallel relationships", hasParallelRelationships},
		{"a foreign key with no target, and so a stub", hasStub},
		{"no relationships at all", hasNoRelationships},
	}

	for _, category := range categories {
		t.Run(category.name, func(t *testing.T) {
			for _, tt := range docs {
				if category.has(tt.doc) {
					t.Logf("covered by %s", tt.name)
					return
				}
			}
			t.Errorf("no document in the table has %s, so nothing above says anything about it", category.name)
		})
	}
}

func hasSelfReference(doc *model.Document) bool {
	for i := range doc.Tables {
		for j := range doc.Tables[i].ForeignKeys {
			if doc.Tables[i].ForeignKeys[j].References.Table == doc.Tables[i].Name {
				return true
			}
		}
	}
	return false
}

func hasParallelRelationships(doc *model.Document) bool {
	for i := range doc.Tables {
		seen := make(map[string]bool, len(doc.Tables[i].ForeignKeys))
		for j := range doc.Tables[i].ForeignKeys {
			target := doc.Tables[i].ForeignKeys[j].References.Table
			if seen[target] {
				return true
			}
			seen[target] = true
		}
	}
	return false
}

func hasStub(doc *model.Document) bool { return len(erd.UndefinedTargets(doc)) > 0 }

func hasNoRelationships(doc *model.Document) bool {
	for i := range doc.Tables {
		if len(doc.Tables[i].ForeignKeys) > 0 {
			return false
		}
	}
	return true
}

// hasCycle asks the cycle breaker: a document has a cycle exactly when the
// depth-first walk has to reverse something. Self-loops are extracted before it
// runs, so they do not answer this question - which is what makes this category
// different from the first one.
func hasCycle(doc *model.Document) bool {
	g := buildGraph(doc)
	for _, members := range g.components {
		if len(breakCycles(g, members)) > 0 {
			return true
		}
	}
	return false
}

// hasLongRelationship reports whether some relationship still spans more than
// one rank after the ranking is tightened - which is the same question as
// whether its chain holds more than the one label node, since a chain is one
// node per half-rank crossed.
func hasLongRelationship(doc *model.Document) bool {
	g := buildGraph(doc)
	for _, members := range g.components {
		breakCycles(g, members)
		ranks := assignRanks(g, members)
		_, _, chains := insertVirtualNodes(g, members, ranks)
		for _, c := range chains {
			if len(c.nodes) > 1 {
				return true
			}
		}
	}
	return false
}
