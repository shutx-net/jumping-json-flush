package svg

import (
	"reflect"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
)

// kindsOf names the primitive each item is, so an ordering assertion reads as
// the paint order rather than as a list of struct literals.
func kindsOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch item.(type) {
		case Box:
			out = append(out, "box")
		case Polyline:
			out = append(out, "polyline")
		case Circle:
			out = append(out, "circle")
		case TextRun:
			out = append(out, "text")
		default:
			out = append(out, "unknown")
		}
	}
	return out
}

// textRuns is every TextRun in items, in order.
func textRuns(items []Item) []TextRun {
	var out []TextRun
	for _, item := range items {
		if run, ok := item.(TextRun); ok {
			out = append(out, run)
		}
	}
	return out
}

// outDistance is how far p lies from at in direction dir, which is what "the
// cardinality mark is nearest the box" is measured in.
func outDistance(p, at, dir Point) Coord {
	return (p.X-at.X)*dir.X + (p.Y-at.Y)*dir.Y
}

// TestSceneItemOrder is the paint order, stated as the sequence of primitives
// for the smallest document that has one of everything: two boxes, one
// relationship and its label.
//
// Every row of both tables is in a key, so every marker cell carries text and no
// run is skipped - which is what makes the sequence readable as a list rather
// than as arithmetic about which cells happened to be empty. The two
// placeholders the layout inserted, a label node and no virtual node here, draw
// nothing at all and so appear nowhere in it.
func TestSceneItemOrder(t *testing.T) {
	scene := buildScene(layout(document(linked("parent"), linked("child", "parent"))))

	want := []string{
		"box", // the page background

		// parent: outline, header band, two header lines, one row of four cells
		"box", "box", "text", "text", "text", "text", "text", "text",

		// child: the same, then a separator and its second row
		"box", "box", "text", "text", "text", "text", "text", "text",
		"polyline", "text", "text", "text", "text",

		// the relationship: its route, then two primitives per end
		"polyline", "polyline", "circle", "polyline", "circle",

		// and its name, last
		"text",
	}
	if got := kindsOf(scene.Items); !reflect.DeepEqual(got, want) {
		t.Errorf("the paint order is\n%v\nwant\n%v", got, want)
	}
}

func TestSceneBackgroundCoversBounds(t *testing.T) {
	geo := layout(document(linked("parent"), linked("child", "parent")))
	scene := buildScene(geo)

	first, ok := scene.Items[0].(Box)
	if !ok {
		t.Fatalf("the first item is a %T, want the background box", scene.Items[0])
	}
	if first.Rect != geo.Bounds {
		t.Errorf("the background is %+v, want the bounds %+v", first.Rect, geo.Bounds)
	}
	if first.Fill != colourPage {
		t.Errorf("the background is filled %q, want the opaque page colour %q", first.Fill, colourPage)
	}
	if scene.Width != geo.Bounds.W || scene.Height != geo.Bounds.H {
		t.Errorf("the scene is %dx%d, want the bounds %dx%d", scene.Width, scene.Height, geo.Bounds.W, geo.Bounds.H)
	}
}

func TestSceneBoxInternals(t *testing.T) {
	table := *ordersTable()
	geo := layout(document(table))
	node := geo.Nodes[0]
	c := node.Content

	// Row 1 of this table is in no key, so its marker cell is empty and draws
	// no run at all. That is the one thing that makes the run count depend on
	// what is in the cells rather than on the shape of the box, so it is stated
	// rather than absorbed into a number.
	if c.cells[1][0] != "" {
		t.Fatalf("row 1's marker cell is %q; this table no longer exercises the empty cell", c.cells[1][0])
	}
	want := len(c.headerLines)
	for _, row := range c.cells {
		for _, cell := range row {
			if cell != "" {
				want++
			}
		}
	}

	runs := textRuns(buildScene(geo).Items)
	if len(runs) != want {
		t.Fatalf("%d text run(s), want %d: two header lines and every cell that carries text", len(runs), want)
	}

	// The header: two lines, two different baselines, the logical name bold.
	if runs[0].At.Y == runs[1].At.Y {
		t.Errorf("the two header lines share the baseline %d", runs[0].At.Y)
	}
	if runs[0].Weight != weightBold || runs[1].Weight != weightNormal {
		t.Errorf("the header lines are %q and %q, want the logical name bold and the physical name not",
			runs[0].Weight, runs[1].Weight)
	}
	for k := range c.headerLines {
		if want := node.Rect.Y + c.headerBaselines[k]; runs[k].At.Y != want {
			t.Errorf("header line %d sits at y %d, want the measured baseline %d", k, runs[k].At.Y, want)
		}
	}

	// The first row's four cells: one shared baseline, and each at its own
	// measured column offset plus the cell padding.
	row := runs[2 : 2+cellColumns]
	for j, run := range row {
		if run.At.Y != row[0].At.Y {
			t.Errorf("cell %d sits at y %d and cell 0 at %d; a row has one baseline", j, run.At.Y, row[0].At.Y)
		}
		if want := node.Rect.X + c.colX[j] + cellPadH; run.At.X != want {
			t.Errorf("cell %d sits at x %d, want the measured offset %d", j, run.At.X, want)
		}
		if run.Anchor != anchorStart {
			t.Errorf("cell %d is anchored %q, want %q", j, run.Anchor, anchorStart)
		}
	}
	if want := node.Rect.Y + c.rowBaselines[0]; row[0].At.Y != want {
		t.Errorf("the first row sits at y %d, want the measured baseline %d", row[0].At.Y, want)
	}
}

func TestSceneStubIsDashed(t *testing.T) {
	geo := layout(document(linked("orders", "coupons")))

	stub := -1
	for i, n := range geo.Nodes {
		if n.Kind == kindStub {
			stub = i
		}
	}
	if stub < 0 {
		t.Fatalf("the document has no stub node")
	}

	items := appendNode(nil, &geo.Nodes[stub])
	box, ok := items[0].(Box)
	if !ok {
		t.Fatalf("the stub's first item is a %T, want its outline", items[0])
	}
	if !box.Dash {
		t.Errorf("the stub's outline is not dashed")
	}
	if box.Stroke != colourStubLine || box.Fill != colourStubFill {
		t.Errorf("the stub is stroked %q and filled %q, want the grey %q and %q",
			box.Stroke, box.Fill, colourStubLine, colourStubFill)
	}

	runs := textRuns(items)
	if len(runs) != 2 {
		t.Fatalf("the stub has %d text run(s), want two: its name and the note", len(runs))
	}
	if runs[0].Text != geo.Nodes[stub].Name {
		t.Errorf("the stub's first line is %q, want its name %q", runs[0].Text, geo.Nodes[stub].Name)
	}
	if runs[1].Text != stubNote {
		t.Errorf("the stub's second line is %q, want %q", runs[1].Text, stubNote)
	}
	for _, run := range runs {
		if run.Fill != colourStubText {
			t.Errorf("the stub's text is %q, want the grey %q", run.Fill, colourStubText)
		}
	}
}

// TestSceneCrowFootFourForms pins all four ends against the constants, on one
// side, so the four are comparable line for line: the cardinality mark first and
// the optionality mark inboard of it, two primitives either way.
func TestSceneCrowFootFourForms(t *testing.T) {
	at := Point{X: 1000, Y: 2000}
	dir := outward(sideRight)

	crow := Polyline{
		Points: []Point{
			{X: 1000, Y: 2000 + crowHalf},
			{X: 1000 + crowLen, Y: 2000},
			{X: 1000, Y: 2000 - crowHalf},
		},
		Stroke: colourLine, StrokeWidth: strokeWidth, Fill: colourNone,
	}
	cardinalityBar := Polyline{
		Points: []Point{
			{X: 1000 + barOffset, Y: 2000 + barHalf},
			{X: 1000 + barOffset, Y: 2000 - barHalf},
		},
		Stroke: colourLine, StrokeWidth: strokeWidth, Fill: colourNone,
	}
	optionalityBar := Polyline{
		Points: []Point{
			{X: 1000 + circleOffset, Y: 2000 + barHalf},
			{X: 1000 + circleOffset, Y: 2000 - barHalf},
		},
		Stroke: colourLine, StrokeWidth: strokeWidth, Fill: colourNone,
	}
	circle := Circle{
		Centre: Point{X: 1000 + circleOffset, Y: 2000},
		R:      circleR,
		Fill:   colourPage, Stroke: colourLine, StrokeWidth: strokeWidth,
	}

	tests := []struct {
		name string
		end  erd.End
		want []Item
	}{
		{"many and optional", erd.End{Many: true, Optional: true}, []Item{crow, circle}},
		{"many and mandatory", erd.End{Many: true}, []Item{crow, optionalityBar}},
		{"one and optional", erd.End{Optional: true}, []Item{cardinalityBar, circle}},
		{"one and mandatory", erd.End{}, []Item{cardinalityBar, optionalityBar}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendEnd(nil, at, sideRight, tt.end)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("the end is drawn as\n%+v\nwant\n%+v", got, tt.want)
			}

			// The mark nearest the box is the cardinality one, which is what
			// crow's-foot notation requires and what the two offsets encode.
			near := outDistance(furthestOut(got[0], at, dir), at, dir)
			far := outDistance(nearestOut(got[1], at, dir), at, dir)
			if near > far {
				t.Errorf("the cardinality mark reaches %d out and the optionality mark starts at %d", near, far)
			}
		})
	}
}

// furthestOut and nearestOut are the extremes of one primitive along dir, which
// is all the ordering assertion above needs. A circle's extremes are its centre
// plus and minus its radius.
func furthestOut(item Item, at, dir Point) Point {
	switch v := item.(type) {
	case Polyline:
		best := v.Points[0]
		for _, p := range v.Points {
			if outDistance(p, at, dir) > outDistance(best, at, dir) {
				best = p
			}
		}
		return best
	case Circle:
		return along(v.Centre, dir, v.R)
	}
	return at
}

func nearestOut(item Item, at, dir Point) Point {
	switch v := item.(type) {
	case Polyline:
		best := v.Points[0]
		for _, p := range v.Points {
			if outDistance(p, at, dir) < outDistance(best, at, dir) {
				best = p
			}
		}
		return best
	case Circle:
		return along(v.Centre, dir, -v.R)
	}
	return at
}

// TestSceneCrowFootFourDirections is the same end on each of the sides anything
// attaches to. The shape is one template turned by an integer swap and a sign
// change, so the coordinates are asserted rather than the shape being described.
func TestSceneCrowFootFourDirections(t *testing.T) {
	at := Point{X: 1000, Y: 2000}
	end := erd.End{Many: true, Optional: true}

	// Stated in terms of the constants rather than as finished numbers: the
	// values in coord.go's block are adjustable and the ROTATION is what this
	// test is about, so a wider crow's foot must not read as a broken glyph.
	tests := []struct {
		name       string
		s          side
		wantPoints []Point
		wantCentre Point
	}{
		{
			"out of the right face", sideRight,
			[]Point{
				{X: at.X, Y: at.Y + crowHalf},
				{X: at.X + crowLen, Y: at.Y},
				{X: at.X, Y: at.Y - crowHalf},
			},
			Point{X: at.X + circleOffset, Y: at.Y},
		},
		{
			"into the left face", sideLeft,
			[]Point{
				{X: at.X, Y: at.Y - crowHalf},
				{X: at.X - crowLen, Y: at.Y},
				{X: at.X, Y: at.Y + crowHalf},
			},
			Point{X: at.X - circleOffset, Y: at.Y},
		},
		{
			"out of the top face", sideTop,
			[]Point{
				{X: at.X + crowHalf, Y: at.Y},
				{X: at.X, Y: at.Y - crowLen},
				{X: at.X - crowHalf, Y: at.Y},
			},
			Point{X: at.X, Y: at.Y - circleOffset},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := appendEnd(nil, at, tt.s, end)
			if len(items) != 2 {
				t.Fatalf("%d primitive(s), want two: the cardinality mark and the optionality mark", len(items))
			}

			crow, ok := items[0].(Polyline)
			if !ok {
				t.Fatalf("the cardinality mark is a %T, want the crow's foot", items[0])
			}
			if !reflect.DeepEqual(crow.Points, tt.wantPoints) {
				t.Errorf("the crow's foot is %+v, want %+v", crow.Points, tt.wantPoints)
			}

			circle, ok := items[1].(Circle)
			if !ok {
				t.Fatalf("the optionality mark is a %T, want the circle", items[1])
			}
			if circle.Centre != tt.wantCentre {
				t.Errorf("the circle is centred %+v, want %+v", circle.Centre, tt.wantCentre)
			}

			// The two prongs land on the box's own boundary, either side of the
			// attachment point, and the vertex is out in the corridor.
			dir := outward(tt.s)
			if outDistance(crow.Points[0], at, dir) != 0 || outDistance(crow.Points[2], at, dir) != 0 {
				t.Errorf("the crow's prongs are not on the boundary the attachment is on: %+v", crow.Points)
			}
			if got := outDistance(crow.Points[1], at, dir); got != crowLen {
				t.Errorf("the crow's vertex is %d out, want crowLen %d", got, crowLen)
			}
		})
	}
}

// TestSceneNoCardinalityTypeLeaks is the closed set, checked. If a fifth
// primitive is ever added, this switch stops covering the Scene and says so -
// which is the only automatic warning available for "the writer now has to know
// something new".
func TestSceneNoCardinalityTypeLeaks(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			for i, item := range buildScene(layout(tt.doc)).Items {
				switch item.(type) {
				case Box, Polyline, Circle, TextRun:
				default:
					t.Errorf("item %d is a %T, which is not one of the four primitives", i, item)
				}
			}
		})
	}
}

func TestSceneTextRunsCarryAResolvedFamily(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			runs := textRuns(buildScene(layout(tt.doc)).Items)
			if len(runs) == 0 {
				t.Fatalf("the scene has no text at all")
			}
			for i, run := range runs {
				if run.Family != fontFamily {
					t.Errorf("run %d asks for %q, want the resolved stack %q", i, run.Family, fontFamily)
				}
				if run.Size != fontSize {
					t.Errorf("run %d is %d, want fontSize %d", i, run.Size, fontSize)
				}
				if run.Text == "" {
					t.Errorf("run %d draws no text", i)
				}
			}
		})
	}
}

func TestBuildSceneIsDeterministic(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			geo := layout(tt.doc)
			if first, second := buildScene(geo), buildScene(geo); !reflect.DeepEqual(first, second) {
				t.Errorf("two runs disagree:\n%+v\n%+v", first, second)
			}
		})
	}
}

// TestBuildSceneMeasuresNothing is D37 asserted by construction rather than by
// reading the code: the Content below claims a cell column is 999 units wide
// when the text in it would measure a fraction of that, and the scene has to use
// 999. Scene construction that re-measured would put the cell somewhere else,
// and the box - which was sized from the first measurement - is the thing that
// would then overflow.
func TestBuildSceneMeasuresNothing(t *testing.T) {
	const wide = 999

	c := &content{
		headerLines:     [2]string{"h", "p"},
		headerBaselines: [2]Coord{cellPadV + baselineIn(lineHeight), cellPadV + lineHeight + baselineIn(lineHeight)},
		headerHeight:    2*lineHeight + 2*cellPadV,
		cells:           [][]string{{"a", "b"}},
		rowHeights:      []Coord{rowHeight},
		rowBaselines:    []Coord{2*lineHeight + 2*cellPadV + baselineIn(rowHeight)},
		colX:            []Coord{0, wide},
		colW:            []Coord{wide, wide},
		width:           2 * wide,
		height:          2*lineHeight + 2*cellPadV + rowHeight,
	}
	geo := Geometry{
		Nodes:  []GeoNode{{Rect: Rect{W: c.width, H: c.height}, Kind: kindTable, Name: "t", Content: c}},
		Bounds: Rect{W: c.width, H: c.height},
	}

	runs := textRuns(buildScene(geo).Items)
	if len(runs) != 4 {
		t.Fatalf("%d text run(s), want two header lines and two cells", len(runs))
	}
	if got, want := runs[3].At.X, Coord(wide)+cellPadH; got != want {
		t.Errorf("the second cell is at x %d, want the offset the content claims, %d", got, want)
	}
	if measureText("b") >= wide {
		t.Fatalf("the cell text measures %d, which is not narrower than the claimed %d; the test proves nothing",
			measureText("b"), wide)
	}
}

// TestSceneLabelIsCentredOnItsRectangle pins where a relationship's name goes:
// centred in the rectangle the layout reserved for it, on a computed baseline,
// with no baseline attribute anywhere.
func TestSceneLabelIsCentredOnItsRectangle(t *testing.T) {
	geo := layout(document(linked("parent"), linked("child", "parent")))
	if len(geo.Labels) != 1 {
		t.Fatalf("%d label(s), want 1", len(geo.Labels))
	}

	label := geo.Labels[0]
	items := buildScene(geo).Items
	run, ok := items[len(items)-1].(TextRun)
	if !ok {
		t.Fatalf("the last item is a %T, want the label's text", items[len(items)-1])
	}

	if run.Text != label.Text {
		t.Errorf("the label draws %q, want %q", run.Text, label.Text)
	}
	if want := label.Rect.X + label.Rect.W/2; run.At.X != want {
		t.Errorf("the label is at x %d, want the centre of %+v, %d", run.At.X, label.Rect, want)
	}
	if want := label.Rect.Y + baselineIn(label.Rect.H); run.At.Y != want {
		t.Errorf("the label is at y %d, want the baseline %d inside %+v", run.At.Y, want, label.Rect)
	}
	if run.Anchor != anchorMiddle {
		t.Errorf("the label is anchored %q, want %q", run.Anchor, anchorMiddle)
	}
}

// TestSceneEveryPolylineDeclaresNoFill is the SVG trap: an unfilled <polyline>
// is filled BLACK, so every one of them has to say so, and saying so is the
// scene's job rather than the writer's.
func TestSceneEveryPolylineDeclaresNoFill(t *testing.T) {
	for _, tt := range spanDocuments() {
		t.Run(tt.name, func(t *testing.T) {
			for i, item := range buildScene(layout(tt.doc)).Items {
				line, ok := item.(Polyline)
				if !ok {
					continue
				}
				if line.Fill != colourNone {
					t.Errorf("polyline %d is filled %q, want %q", i, line.Fill, colourNone)
				}
				if line.StrokeWidth != strokeWidth {
					t.Errorf("polyline %d is %d wide, want strokeWidth %d", i, line.StrokeWidth, strokeWidth)
				}
			}
		})
	}
}

// TestSceneCoversEveryNodeKind makes sure the two placeholders really do draw
// nothing, on a document that has all four kinds - otherwise the item order
// above would be pinning a sequence that happened not to contain any.
func TestSceneCoversEveryNodeKind(t *testing.T) {
	doc := document(
		linked("a", "b", "c"),
		linked("b", "c"),
		linked("c", "missing"),
	)
	geo := layout(doc)

	var byKind [kindVirtual + 1]int
	for _, n := range geo.Nodes {
		byKind[n.Kind]++
	}
	for _, kind := range []nodeKind{kindTable, kindStub, kindLabel, kindVirtual} {
		if byKind[kind] == 0 {
			t.Fatalf("the document has no node of kind %d; this test no longer covers it", kind)
		}
	}

	for i := range geo.Nodes {
		items := appendNode(nil, &geo.Nodes[i])
		if isReal(geo.Nodes[i].Kind) {
			if len(items) == 0 {
				t.Errorf("node %d is a box and drew nothing", i)
			}
			continue
		}
		if len(items) != 0 {
			t.Errorf("node %d is of kind %d and drew %d item(s); a placeholder draws no ink",
				i, geo.Nodes[i].Kind, len(items))
		}
	}
}
