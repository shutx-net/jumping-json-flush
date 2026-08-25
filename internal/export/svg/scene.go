package svg

import "github.com/shutx-net/jumping-json-flush/internal/export/erd"

// ---------------------------------------------------------------------------
// The Scene: four primitives, and nothing that has to be interpreted
// ---------------------------------------------------------------------------

// Scene is the drawing as a flat, ordered list of painted primitives, with
// every position, baseline, colour, font and glyph already resolved.
//
// Its contract is best stated as a prohibition list, because what it does NOT
// hold is the whole reason it exists. A Scene holds no rank, no half-rank, no
// virtual node, no component, no cardinality, no row height, no font role and
// no cell. The writer that follows it can therefore be pure serialization: it
// turns numbers and strings into XML and has no drawing decision left to make.
//
// The next phase's TestWriteSceneBuiltByHand is what enforces that rather than
// this comment - a Scene assembled from Go literals, with no Document and no
// Geometry anywhere near it, that the writer can still render byte for byte. If
// the writer ever starts needing a rank or a row height, that test stops
// compiling.
type Scene struct {
	Width, Height Coord
	Items         []Item
}

// Item is one primitive of a Scene. The set is CLOSED to four: Box, Polyline,
// Circle and TextRun.
//
// Closed by an unexported method rather than by convention. Nothing outside this
// package can implement it, and a fifth primitive is a visible edit to this file
// rather than something that turns up in a caller - which matters because every
// plausible fifth is a piece of drawing knowledge being moved into the writer.
// There is deliberately no Path item for "whatever else the writer can draw",
// and no Glyph or Marker item either; see appendEnd for the case that makes the
// difference.
type Item interface{ item() }

// Box is a painted rectangle: the page's background, a table's outline, a header
// band.
//
// It is not called Rect because coord.go's Rect is the GEOMETRY - a rectangle
// with no paint on it, which is what the layout computes and the invariants read
// - and this is one <rect> element in the finished file. The two are different
// things and a reader has to be able to tell which one a name means.
//
// Dash is a flag rather than a dash pattern string because there is exactly one
// dashed thing in this drawing, the stub box, and a pattern here would be a
// presentation knob no document can reach.
type Box struct {
	Rect        Rect
	Fill        string
	Stroke      string
	StrokeWidth Coord
	Dash        bool
}

// Polyline is a painted open path: a route, a row separator, a crow's foot, a
// cardinality bar.
//
// Fill is a field, always "none", rather than a default the writer supplies.
// SVG fills a <polyline> BLACK when nothing says otherwise, so "none" has to be
// written into the file - and if the writer supplied it, the writer would be
// making a presentation decision, which is the one thing it may not do.
type Polyline struct {
	Points      []Point
	Stroke      string
	StrokeWidth Coord
	Fill        string
}

// Circle is the optionality mark, and the only curve in the drawing. Every
// routed segment is axis-aligned (D14); this is not a routed segment.
type Circle struct {
	Centre      Point
	R           Coord
	Fill        string
	Stroke      string
	StrokeWidth Coord
}

// TextRun is one line of text, positioned on its BASELINE.
//
// Family is a resolved font-family string and not a role enum, and that is a
// decision rather than a convenience (D12). The advance-width estimate in
// measure.go is an upper bound for MONOSPACE faces and for nothing else, so the
// family stack and the metric are one decision and have to live in one place; if
// the writer chose the stack it could choose one the estimate does not bound,
// and nothing would catch it but a person noticing an overflowing box. Rejected:
// a role enum the writer maps through a table, which puts a presentation table
// in the file whose whole point is having none. Rejected: putting the family
// once on a wrapping <g> and letting it inherit, which would save real bytes but
// adds a second place a value can come from and one more question about GitHub's
// SVG sanitiser to be wrong about.
//
// At is a baseline position and never a box's top edge, and there is no
// dominant-baseline anywhere in this package (D26). GitHub's SVG sanitiser
// strips dominant-baseline from <text> nodes (github/markup#1160), so text
// centred that way is correct in a browser and misaligned in the README the
// diagram was made for. text-anchor survives sanitisation and is what Anchor is.
type TextRun struct {
	At     Point
	Text   string
	Family string
	Size   Coord
	Weight string
	Anchor string
	Fill   string
}

func (Box) item()      {}
func (Polyline) item() {}
func (Circle) item()   {}
func (TextRun) item()  {}

// ---------------------------------------------------------------------------
// Paint
// ---------------------------------------------------------------------------

// The colours, and the two text attribute values that are not geometry.
//
// They are here rather than in coord.go's block for the same reason measure.go's
// advance widths and position.go's omega weights are where they are: that block
// is the geometry, and these are neither lengths nor positions. What makes them
// one group is that they are all presentation, and presentation is resolved HERE
// so that the writer has none.
//
// There is no theme awareness and no @media rule (D24). A theme-aware SVG needs
// a <style> block, which is exactly what GitHub's sanitiser strips, so the file
// would be right in a browser and wrong in the README it was made for. And an
// SVG with no background at all is transparent, which makes a diagram of dark
// text unreadable on a dark-mode README - the most likely place this file is
// ever looked at. An opaque light background is the answer, and it is the same
// position internal/export/xlsx takes about its workbook.
const (
	colourPage     = "#ffffff"
	colourLine     = "#333333"
	colourHeader   = "#eef1f5"
	colourText     = "#1a1a1a"
	colourStubFill = "#f4f4f4"
	colourStubLine = "#9a9a9a"
	colourStubText = "#666666"
	colourNone     = "none"

	weightNormal = "normal"
	weightBold   = "bold"
	anchorStart  = "start"
	anchorMiddle = "middle"
)

// ---------------------------------------------------------------------------
// Building the scene
// ---------------------------------------------------------------------------

// buildScene turns the finished Geometry into the Scene the writer serialises.
//
// # The order is the paint order
//
// It is fixed here, in this function, and nothing downstream sorts it: the
// background, then the boxes in Geometry.Nodes order, then the relationships in
// Geometry.Edges order, then the labels in Geometry.Labels order. Two of those
// placements matter and the rest is only determinism.
//
// The relationships come AFTER the boxes so that an endpoint sitting exactly on
// a box's boundary is drawn over the border rather than under it - an attachment
// point is on the boundary by construction, so half of every crow's foot would
// otherwise be hidden by the box's own stroke. The labels come LAST so that no
// route is drawn across its own name.
//
// The order is fixed rather than sorted because the golden files are the bytes
// this list produces, and a sort would put a comparator between a layout change
// and the diff that is supposed to show it.
func buildScene(geo Geometry) Scene {
	s := Scene{Width: geo.Bounds.W, Height: geo.Bounds.H}

	// The background covers the whole viewBox, which moveToMargin made start at
	// the origin.
	s.Items = append(s.Items, Box{Rect: geo.Bounds, Fill: colourPage})

	for i := range geo.Nodes {
		s.Items = appendNode(s.Items, &geo.Nodes[i])
	}
	for i := range geo.Edges {
		s.Items = appendEdge(s.Items, &geo.Edges[i])
	}
	for _, l := range geo.Labels {
		s.Items = append(s.Items, labelText(l))
	}
	return s
}

// appendNode draws one box: its outline, its header band, the header's two lines
// and then each column row with its separator and its cells.
//
// A label node and a virtual node draw nothing at all. They are nodes because
// they take part in the layout - one reserves the band its text sits in, the
// other holds a corridor open - and they are in Geometry because the bounds have
// to cover them, but there is no ink in either.
//
// Everything inside the box comes from Content and nothing here measures
// anything: the cells' strings, their x offsets, the row heights and every
// baseline were all measured once, at graph construction, and the box was SIZED
// from that measurement. Measuring again here would be a second answer about the
// same width, and since the box already fits the first one, the second is the
// one that overflows. TestBuildSceneMeasuresNothing is that property asserted
// rather than promised.
func appendNode(items []Item, n *GeoNode) []Item {
	if !isReal(n.Kind) {
		return items
	}

	c := n.Content
	stub := n.Kind == kindStub

	// A stub is the same builder with different paint: dashed, grey, and with
	// its header band the same colour as its body, since it has no columns for
	// a band to separate it from.
	fill, line, ink := colourPage, colourLine, colourText
	header := colourHeader
	if stub {
		fill, line, ink = colourStubFill, colourStubLine, colourStubText
		header = colourStubFill
	}

	items = append(items, Box{
		Rect:        n.Rect,
		Fill:        fill,
		Stroke:      line,
		StrokeWidth: strokeWidth,
		Dash:        stub,
	})

	top := n.Rect.Y + contentOffsetY(n.Rect, c)
	items = append(items, Box{
		Rect:        Rect{X: n.Rect.X, Y: top, W: n.Rect.W, H: c.headerHeight},
		Fill:        header,
		Stroke:      line,
		StrokeWidth: strokeWidth,
		Dash:        stub,
	})

	// The header is centred and its first line - the logical name - is bold,
	// which is the same emphasis internal/export/dot gives it.
	centre := n.Rect.X + n.Rect.W/2
	for k, text := range c.headerLines {
		weight := weightNormal
		if k == 0 {
			weight = weightBold
		}
		items = append(items, TextRun{
			At:     Point{X: centre, Y: top + c.headerBaselines[k]},
			Text:   text,
			Family: fontFamily,
			Size:   fontSize,
			Weight: weight,
			Anchor: anchorMiddle,
			Fill:   ink,
		})
	}

	y := top + c.headerHeight
	for i, row := range c.cells {
		// A separator BETWEEN rows, so the first row has none: the header
		// band's own bottom edge already draws the line above it, and a second
		// line on top of it is two strokes a reader can see as one thick one.
		if i > 0 {
			items = append(items, Polyline{
				Points:      []Point{{X: n.Rect.X, Y: y}, {X: n.Rect.Right(), Y: y}},
				Stroke:      line,
				StrokeWidth: strokeWidth,
				Fill:        colourNone,
			})
		}

		for j, cell := range row {
			// An empty cell draws nothing. The marker column of a table whose
			// columns are in no key is empty in every row, and an empty <text>
			// element is bytes in every golden file for no ink.
			if cell == "" {
				continue
			}
			items = append(items, TextRun{
				At:     Point{X: n.Rect.X + c.colX[j] + cellPadH, Y: top + c.rowBaselines[i]},
				Text:   cell,
				Family: fontFamily,
				Size:   fontSize,
				Weight: weightNormal,
				Anchor: anchorStart,
				Fill:   ink,
			})
		}
		y += c.rowHeights[i]
	}

	return items
}

// appendEdge draws one relationship: its route, then the glyph at its child end,
// then the glyph at its parent end.
func appendEdge(items []Item, e *GeoEdge) []Item {
	items = append(items, Polyline{
		Points:      e.Points,
		Stroke:      colourLine,
		StrokeWidth: strokeWidth,
		Fill:        colourNone,
	})
	items = appendEnd(items, e.ChildAt, e.ChildSide, e.ChildEnd)
	return appendEnd(items, e.ParentAt, e.ParentSide, e.ParentEnd)
}

// appendEnd expands one end of one relationship into the two primitives that
// draw it: the cardinality mark nearest the box, then the optionality mark
// inboard of it.
//
// # Why the expansion happens here
//
// Because a Scene carrying CrowFoot{at, direction, form} would leave the writer
// to turn it into path data, and the writer would then know cardinality
// geometry. "The writer knows nothing about cardinality" is the one thing Scene
// exists to guarantee, so a type like that would deny the whole purpose of the
// layer. What is left in the Scene is polylines and circles, which is what an
// end IS once somebody has decided where the lines go.
//
// # Why both marks, always
//
// The nearest mark is the cardinality - a crow's foot for many, a bar for
// exactly one - and the optionality mark sits inboard of it - a circle for
// optional, a bar for mandatory. That is the order crow's-foot notation requires
// and the same order graphviz composes an arrow type in, which is where
// internal/export/dot gets "crowodot" and "teetee" from. All four forms are two
// primitives, so both ends of every relationship carry the same amount of ink
// however they were derived, and the diagram reads as uniform rather than as one
// end being decorated and the other bare.
//
// # Why there is no trigonometry
//
// A glyph faces one of four axis directions, and only three of them occur: the
// side policy attaches to a left, a right or a top face and never to a bottom
// one (D08), and every first segment of every route is axis-aligned. So the
// glyph is ONE template, written once in terms of "how far out" and "how far
// across", placed by along and across - an integer swap and a sign change. There
// is no angle anywhere, and therefore nothing to round.
func appendEnd(items []Item, at Point, s side, end erd.End) []Item {
	dir := outward(s)

	if end.Many {
		// The fork opens onto the box: its vertex is crowLen out along the
		// route and its two outer prongs land on the boundary either side of
		// the attachment point. The third prong is the route's own last
		// segment, which runs along this axis through the vertex and into the
		// attachment - the same composition graphviz relies on, and the reason
		// this is two segments rather than three.
		items = append(items, Polyline{
			Points: []Point{
				across(at, dir, crowHalf),
				along(at, dir, crowLen),
				across(at, dir, -crowHalf),
			},
			Stroke:      colourLine,
			StrokeWidth: strokeWidth,
			Fill:        colourNone,
		})
	} else {
		items = append(items, bar(at, dir, barOffset))
	}

	if end.Optional {
		return append(items, Circle{
			Centre:      along(at, dir, circleOffset),
			R:           circleR,
			Fill:        colourPage,
			Stroke:      colourLine,
			StrokeWidth: strokeWidth,
		})
	}
	return append(items, bar(at, dir, circleOffset))
}

// bar is the stroke across the route that both "exactly one" and "mandatory"
// are drawn as, centred d out from the attachment point.
//
// One shape for the two marks is not a shortcut: crow's-foot notation draws them
// alike, and the two never coincide because they sit at different distances -
// barOffset for the cardinality mark and circleOffset for the optionality one,
// which is also where the circle's centre goes, so the two forms of the
// optionality mark occupy the same place.
func bar(at, dir Point, d Coord) Polyline {
	centre := along(at, dir, d)
	return Polyline{
		Points:      []Point{across(centre, dir, barHalf), across(centre, dir, -barHalf)},
		Stroke:      colourLine,
		StrokeWidth: strokeWidth,
		Fill:        colourNone,
	}
}

// labelText is one relationship's name, centred on its rectangle.
//
// The rectangle is where the layout reserved room for the text - the strip at
// the top of a label node's band, or the plate routeStaple placed above a
// staple's horizontal run - so the text is centred in it rather than measured
// again. The baseline is computed with baselineIn, which is the same arithmetic
// that put the text in a table row, so a label and a row look alike (D26 for why
// it is computed at all).
func labelText(l GeoLabel) TextRun {
	return TextRun{
		At:     Point{X: l.Rect.X + l.Rect.W/2, Y: l.Rect.Y + baselineIn(l.Rect.H)},
		Text:   l.Text,
		Family: fontFamily,
		Size:   fontSize,
		Weight: weightNormal,
		Anchor: anchorMiddle,
		Fill:   colourText,
	}
}

// outward is the direction a glyph is drawn in from its attachment point: away
// from the box, along the first segment of the route.
//
// The switch is total over the three sides rather than defaulting, for the same
// reason demand.add's is: nothing in this package attaches to a box's bottom
// edge (D08), so the fourth axis direction never occurs, and an invented fourth
// side should go somewhere visible rather than quietly reading as "up".
func outward(s side) Point {
	switch s {
	case sideLeft:
		return Point{X: -1}
	case sideRight:
		return Point{X: 1}
	}
	return Point{Y: -1}
}

// along is p moved d units in direction dir, and across is p moved d units along
// the perpendicular to dir - the perpendicular being dir with its components
// swapped and one of them negated, which is a quarter turn without an angle.
//
// Between them they place every point of every glyph, so the glyph template is
// written once and the four directions cost nothing but these two functions.
func along(p, dir Point, d Coord) Point {
	return Point{X: p.X + d*dir.X, Y: p.Y + d*dir.Y}
}

func across(p, dir Point, d Coord) Point {
	return Point{X: p.X - d*dir.Y, Y: p.Y + d*dir.X}
}
