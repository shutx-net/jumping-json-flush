package svg

import "testing"

// The coordinates in this file are arbitrary. They are deliberately NOT built
// out of the constants in coord.go: these predicates are scale-free, and
// writing them in terms of rankGap or slotSpacing would mean that adjusting a
// spacing value silently changed what the predicate tests assert. Round
// numbers, chosen so that "one unit" cases are visible.
//
// The whole geometry invariant suite is written in terms of these four
// predicates, so they are tested exhaustively - every boundary case, in both
// argument orders - rather than representatively.

// testBox is the rectangle most cases below are stated against: x from 100 to
// 200, y from 100 to 200.
var testBox = Rect{X: 100, Y: 100, W: 100, H: 100}

func TestRectIntersectsInterior(t *testing.T) {
	tests := []struct {
		name string
		a, b Rect
		want bool
	}{
		{"disjoint", testBox, Rect{X: 300, Y: 300, W: 50, H: 50}, false},
		// Two boxes standing side by side share their whole edge and are a
		// perfectly legal drawing. A closed test would call this a collision.
		{"sharing the right edge exactly", testBox, Rect{X: 200, Y: 100, W: 100, H: 100}, false},
		{"sharing the bottom edge exactly", testBox, Rect{X: 100, Y: 200, W: 100, H: 100}, false},
		{"sharing a corner exactly", testBox, Rect{X: 200, Y: 200, W: 100, H: 100}, false},
		{"one inside the other", testBox, Rect{X: 120, Y: 120, W: 10, H: 10}, true},
		{"overlapping by one unit", testBox, Rect{X: 199, Y: 199, W: 100, H: 100}, true},
		{"identical", testBox, testBox, true},
		// A zero-width rectangle has no interior at all, so it can overlap
		// nothing - not even the box it is standing in the middle of. Virtual
		// nodes are exactly this shape, which is why the case is not academic.
		{"a zero-width rect inside the box", testBox, Rect{X: 150, Y: 100, W: 0, H: 100}, false},
		{"a zero-height rect inside the box", testBox, Rect{X: 100, Y: 150, W: 100, H: 0}, false},
		{"two zero-width rects on the same line", Rect{X: 150, Y: 100, W: 0, H: 100}, Rect{X: 150, Y: 150, W: 0, H: 100}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.intersectsInterior(tt.b); got != tt.want {
				t.Errorf("%+v.intersectsInterior(%+v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// Overlap is symmetric, and every caller relies on that: the
			// invariant suite compares each pair once, in whichever order the
			// two happen to come out of their slices.
			if got := tt.b.intersectsInterior(tt.a); got != tt.want {
				t.Errorf("%+v.intersectsInterior(%+v) = %v, want %v (not symmetric)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func TestRectUnion(t *testing.T) {
	tests := []struct {
		name string
		a, b Rect
		want Rect
	}{
		{"disjoint", testBox, Rect{X: 300, Y: 400, W: 100, H: 100}, Rect{X: 100, Y: 100, W: 300, H: 400}},
		{"one inside the other", testBox, Rect{X: 120, Y: 120, W: 10, H: 10}, testBox},
		{"identical", testBox, testBox, testBox},
		// A zero-size rectangle is a point, and the bounds have to reach it:
		// this is how an attachment point or a glyph vertex gets inside the
		// page.
		{"a point outside", testBox, Rect{X: 50, Y: 250}, Rect{X: 50, Y: 100, W: 150, H: 150}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.union(tt.b); got != tt.want {
				t.Errorf("%+v.union(%+v) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
			if got := tt.b.union(tt.a); got != tt.want {
				t.Errorf("%+v.union(%+v) = %+v, want %+v (not symmetric)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func TestRectContains(t *testing.T) {
	tests := []struct {
		name string
		p    Point
		want bool
	}{
		{"strictly inside", Point{X: 150, Y: 150}, true},
		// Closed, unlike intersectsInterior: ink on the page's edge is on the
		// page.
		{"on the left edge", Point{X: 100, Y: 150}, true},
		{"on the right edge", Point{X: 200, Y: 150}, true},
		{"on the top edge", Point{X: 150, Y: 100}, true},
		{"on the bottom edge", Point{X: 150, Y: 200}, true},
		{"on a corner", Point{X: 200, Y: 200}, true},
		{"one unit past the right edge", Point{X: 201, Y: 150}, false},
		{"one unit above the top edge", Point{X: 150, Y: 99}, false},
		{"far outside", Point{X: 0, Y: 0}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testBox.contains(tt.p); got != tt.want {
				t.Errorf("%+v.contains(%+v) = %v, want %v", testBox, tt.p, got, tt.want)
			}
		})
	}
}

func TestOnBoundary(t *testing.T) {
	tests := []struct {
		name string
		p    Point
		want bool
	}{
		{"on the left edge", Point{X: 100, Y: 150}, true},
		{"on the right edge", Point{X: 200, Y: 150}, true},
		{"on the top edge", Point{X: 150, Y: 100}, true},
		{"on the bottom edge", Point{X: 150, Y: 200}, true},
		{"on a corner", Point{X: 100, Y: 100}, true},
		// An endpoint one unit inside its own box is an arithmetic mistake,
		// not a rounding artefact, so the answer has to be no.
		{"one unit inside", Point{X: 101, Y: 150}, false},
		{"one unit outside", Point{X: 99, Y: 150}, false},
		{"far outside", Point{X: 0, Y: 0}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onBoundary(tt.p, testBox); got != tt.want {
				t.Errorf("onBoundary(%+v, %+v) = %v, want %v", tt.p, testBox, got, tt.want)
			}
		})
	}
}

func TestSegmentAxisAligned(t *testing.T) {
	tests := []struct {
		name string
		s    Segment
		want bool
	}{
		{"horizontal", Segment{A: Point{X: 0, Y: 50}, B: Point{X: 100, Y: 50}}, true},
		{"vertical", Segment{A: Point{X: 50, Y: 0}, B: Point{X: 50, Y: 100}}, true},
		{"diagonal", Segment{A: Point{X: 0, Y: 0}, B: Point{X: 100, Y: 100}}, false},
		{"one unit off horizontal", Segment{A: Point{X: 0, Y: 50}, B: Point{X: 100, Y: 51}}, false},
		// Degenerate, and trivially parallel to both axes. It answers true so
		// that no call site has to special-case it.
		{"zero length", Segment{A: Point{X: 50, Y: 50}, B: Point{X: 50, Y: 50}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.axisAligned(); got != tt.want {
				t.Errorf("%+v.axisAligned() = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestSegmentIntersectsInterior(t *testing.T) {
	tests := []struct {
		name string
		s    Segment
		want bool
	}{
		// A lane running along a box's side is a legal and common drawing, and
		// it is the case that makes "interior" the right word.
		{"along the top edge", Segment{A: Point{X: 50, Y: 100}, B: Point{X: 250, Y: 100}}, false},
		{"along the left edge", Segment{A: Point{X: 100, Y: 50}, B: Point{X: 100, Y: 250}}, false},
		{"one unit inside the top edge", Segment{A: Point{X: 50, Y: 101}, B: Point{X: 250, Y: 101}}, true},
		{"crossing the box", Segment{A: Point{X: 50, Y: 150}, B: Point{X: 250, Y: 150}}, true},
		{"crossing the box, endpoints reversed", Segment{A: Point{X: 250, Y: 150}, B: Point{X: 50, Y: 150}}, true},
		// Stopping exactly on the boundary is what every attachment point
		// does, so this one is load-bearing: an edge must not be reported as
		// running through the box it ends on.
		{"ending exactly on the left edge", Segment{A: Point{X: 50, Y: 150}, B: Point{X: 100, Y: 150}}, false},
		{"starting exactly on the right edge", Segment{A: Point{X: 200, Y: 150}, B: Point{X: 250, Y: 150}}, false},
		{"one unit past the left edge", Segment{A: Point{X: 50, Y: 150}, B: Point{X: 101, Y: 150}}, true},
		{"entirely outside", Segment{A: Point{X: 0, Y: 0}, B: Point{X: 50, Y: 0}}, false},
		{"a vertical segment through the box", Segment{A: Point{X: 150, Y: 50}, B: Point{X: 150, Y: 250}}, true},
		{"a vertical segment beside the box", Segment{A: Point{X: 250, Y: 50}, B: Point{X: 250, Y: 250}}, false},
		{"a zero-length segment inside the box", Segment{A: Point{X: 150, Y: 150}, B: Point{X: 150, Y: 150}}, false},
		// No routed segment is ever diagonal, and there is no general clipper
		// here to answer for one. The axis-aligned invariant is asserted
		// separately and is what makes this unreachable.
		{"a diagonal through the box is not answered", Segment{A: Point{X: 50, Y: 50}, B: Point{X: 250, Y: 250}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.intersectsInterior(testBox); got != tt.want {
				t.Errorf("%+v.intersectsInterior(%+v) = %v, want %v", tt.s, testBox, got, tt.want)
			}
		})
	}
}

func TestCollinearOverlap(t *testing.T) {
	horizontal := Segment{A: Point{X: 0, Y: 10}, B: Point{X: 100, Y: 10}}

	tests := []struct {
		name string
		a, b Segment
		want bool
	}{
		{"two horizontal at the same y, overlapping", horizontal, Segment{A: Point{X: 50, Y: 10}, B: Point{X: 150, Y: 10}}, true},
		{"the same pair with one written backwards", horizontal, Segment{A: Point{X: 150, Y: 10}, B: Point{X: 50, Y: 10}}, true},
		// Two segments of ONE route meet end to end at every corner. A
		// predicate that counted a shared endpoint would report every corner
		// in the diagram.
		{"touching at one point", horizontal, Segment{A: Point{X: 100, Y: 10}, B: Point{X: 200, Y: 10}}, false},
		{"at different y", horizontal, Segment{A: Point{X: 0, Y: 20}, B: Point{X: 100, Y: 20}}, false},
		{"one unit apart in y", horizontal, Segment{A: Point{X: 0, Y: 11}, B: Point{X: 100, Y: 11}}, false},
		{"one horizontal, one vertical", horizontal, Segment{A: Point{X: 50, Y: 0}, B: Point{X: 50, Y: 100}}, false},
		{"identical", horizontal, horizontal, true},
		{"collinear but disjoint", horizontal, Segment{A: Point{X: 150, Y: 10}, B: Point{X: 250, Y: 10}}, false},
		{"one contained in the other", horizontal, Segment{A: Point{X: 20, Y: 10}, B: Point{X: 30, Y: 10}}, true},
		{
			"two vertical at the same x, overlapping",
			Segment{A: Point{X: 10, Y: 0}, B: Point{X: 10, Y: 100}},
			Segment{A: Point{X: 10, Y: 50}, B: Point{X: 10, Y: 150}},
			true,
		},
		{
			"two vertical one unit apart in x",
			Segment{A: Point{X: 10, Y: 0}, B: Point{X: 10, Y: 100}},
			Segment{A: Point{X: 11, Y: 50}, B: Point{X: 11, Y: 150}},
			false,
		},
		// A zero-length segment has no direction, so it lies along no line and
		// can share no interval with one.
		{"a degenerate segment on the line", horizontal, Segment{A: Point{X: 50, Y: 10}, B: Point{X: 50, Y: 10}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collinearOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("collinearOverlap(%+v, %+v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			if got := collinearOverlap(tt.b, tt.a); got != tt.want {
				t.Errorf("collinearOverlap(%+v, %+v) = %v, want %v (not symmetric)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// TestGeometryConstantsHoldTheirInequalities asserts the relations coord.go's
// block states in prose. The values themselves are adjustable - the first
// person to look at a rendered diagram may well want a wider rankGap - but
// these five are not, because a drawing property depends on each of them, and
// nothing else in the package would notice if one were broken: the diagram
// would still be produced, and it would be wrong in a way only an eye catches.
func TestGeometryConstantsHoldTheirInequalities(t *testing.T) {
	if slotMargin < crowHalf {
		t.Errorf("slotMargin = %d, want at least crowHalf = %d: a glyph at the first or last slot would reach past its own box and escape the computed bounds",
			slotMargin, crowHalf)
	}
	if slotSpacing <= 2*crowHalf {
		t.Errorf("slotSpacing = %d, want more than 2*crowHalf = %d: two crow's feet on adjacent slots would overlap",
			slotSpacing, 2*crowHalf)
	}
	if rankGap <= circleOffset+circleR {
		t.Errorf("rankGap = %d, want more than circleOffset+circleR = %d: the optionality glyph would not fit in the corridor",
			rankGap, circleOffset+circleR)
	}
	// The last two are equations in the block, so they hold by construction.
	// They are asserted anyway, because the way they get broken is somebody
	// replacing an equation with the number it evaluates to and then editing
	// one side of it.
	if labelHeight != lineHeight+2*cellPadV {
		t.Errorf("labelHeight = %d, want lineHeight+2*cellPadV = %d: a label rectangle and a table row must be the same shape",
			labelHeight, lineHeight+2*cellPadV)
	}
	if lanePitch != labelHeight+2*labelGap {
		t.Errorf("lanePitch = %d, want labelHeight+2*labelGap = %d: a label centred on one lane could touch the next lane",
			lanePitch, labelHeight+2*labelGap)
	}
}

// TestEveryGeometryConstantIsPositive names every constant in coord.go's block
// and asserts the one thing true of all of them: zero or less is a mistake in
// every case - a separation of nothing lets two boxes touch, a glyph of
// nothing is invisible, a negative margin puts ink outside the page.
//
// Naming them all has a second purpose, stated so that nobody deletes the list
// wondering what it is for. The block is written once, in full, because the
// values are load-bearing against each other and splitting them across the
// stages that happen to reach them first would put one decision in several
// places. staticcheck reports an unexported constant nothing refers to, so
// this list is what lets a constant exist before the drawing code that will
// use it.
func TestEveryGeometryConstantIsPositive(t *testing.T) {
	constants := []struct {
		name  string
		value Coord
	}{
		{"unit", unit},
		{"fontSize", fontSize},
		{"lineHeight", lineHeight},
		{"textAscent", textAscent},
		{"rowHeight", rowHeight},
		{"cellPadH", cellPadH},
		{"cellPadV", cellPadV},
		{"strokeWidth", strokeWidth},
		{"nodeSep", nodeSep},
		{"rankGap", rankGap},
		{"componentGap", componentGap},
		{"margin", margin},
		{"labelGap", labelGap},
		{"labelHeight", labelHeight},
		{"lanePitch", lanePitch},
		{"loopRise", loopRise},
		{"slotSpacing", slotSpacing},
		{"slotMargin", slotMargin},
		{"crowLen", crowLen},
		{"crowHalf", crowHalf},
		{"barHalf", barHalf},
		{"barOffset", barOffset},
		{"circleR", circleR},
		{"circleOffset", circleOffset},
	}

	for _, c := range constants {
		if c.value <= 0 {
			t.Errorf("%s = %d, want a positive length", c.name, c.value)
		}
	}
}
