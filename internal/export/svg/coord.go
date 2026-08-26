// Package svg draws a database design document as an entity relationship
// diagram and writes it as an SVG image.
//
// jjf lays the diagram out itself. It never runs graphviz, never links it and
// never embeds it, so the binary keeps the project's no-CGO,
// no-runtime-dependency property and a reader gets a picture without
// installing anything.
//
// The exporter checks nothing and reports nothing, ever. A foreign key that
// names a table the document does not define is drawn as a dashed stub, so the
// picture shows exactly what the JSON claims; "jjf validate" is where a
// document is spoken about.
//
// The output is deterministic: the same document always produces the same
// bytes, because nothing here reads the clock, no tool version and no input
// path is written into the file, every loop that reaches the output walks a
// slice rather than a Go map, and every length is an integer - so there is no
// floating-point formatting left to differ between platforms.
//
// # Two representations, and what each one does not know
//
// A document becomes a Geometry: rectangles, routes, attachment points and the
// page they sit on, which is all the ten geometry invariants need to be stated
// against and all they may read. A Geometry becomes a Scene: a flat list of
// painted primitives with every position, baseline, colour and glyph resolved,
// and no rank, no cardinality and no row height left in it. A Scene becomes the
// file. Each step throws information away, and that is the point of having
// three of them rather than one.
//
// # Text is estimated, never measured
//
// Drawing text requires knowing how wide it will be, and there is no font file
// in this binary to ask. So jjf does not ask: runeWidth in measure.go returns
// an UPPER BOUND on the advance width of a rune in a monospace face, and every
// box is sized from that bound. Text can therefore come out narrower than the
// cell it was given, and never wider - the same trick internal/export/xlsx
// plays with its fixed column widths, which decline to measure anything at
// all.
//
// The alternative was to embed graphviz as a WASM build and let it measure.
// That builds without CGO, so it is not ruled out on those grounds; it is
// ruled out because it measures worse than this estimate does. graphviz's
// built-in metrics table covers ASCII only and iterates BYTES, charging every
// byte of a multi-byte rune the width of a space, and there is no font library
// inside the sandbox for it to fall back to. Rendering
// internal/export/svg/testdata/full.json that way overflowed 7 of 24 Japanese
// cells; an estimate like this one overflowed 0 of 19 on the same document. It
// also brought 9 module dependencies against this project's zero. All three
// numbers were measured by the author of issue #32 on a prototype that is not
// in this tree - they are not measurements of this code.
//
// The estimate has one known limit, written here rather than left to be found:
// UAX #11 Ambiguous-width characters are counted as NARROW, so a logical name
// built mostly out of them can overflow its cell in a CJK-configured face. See
// runeWidth for why counting them wide would be the worse failure.
//
// # Everything is an integer
//
// Coord below is the only unit, and there is no floating point anywhere in the
// package, tests included.
package svg

// ---------------------------------------------------------------------------
// Units
// ---------------------------------------------------------------------------

// Coord is a length or a position in the drawing, in tenths of an SVG pixel.
//
// int64 rather than int because the Go spec allows int to be 32 bits, and
// install.sh:193 sends every architecture with no published release to
// "go install" - so a 32-bit build is the documented fallback the installer
// prints, not a hypothetical. Overflow is not reachable at these magnitudes;
// the largest diagram this package can produce is a few hundred thousand
// tenths of a pixel across, which is six orders of magnitude inside a 32-bit
// int as it is. The value of writing the type out is that the geometry model
// is architecture-independent BY CONSTRUCTION rather than by an argument about
// magnitudes, which a later change to the constants could invalidate without
// anyone noticing.
type Coord int64

// ---------------------------------------------------------------------------
// The geometry constants
// ---------------------------------------------------------------------------

// Every geometry constant in this package is in the block below, and nowhere
// else. The golden files are byte-exact, so an inline 8 in a routing function
// is a number nobody can find when the diagram looks wrong, and a second copy
// of it is how two parts of one drawing come to disagree about the same
// distance.
//
// Several of them are load-bearing against each other rather than freely
// choosable. A reader changing a value has to be able to see what it is
// holding up:
//
//   - slotMargin >= crowHalf, so a glyph drawn at the first or the last slot
//     of a side cannot reach past its own box's extent, and therefore cannot
//     escape the computed bounds.
//   - slotSpacing > 2*crowHalf, so two crow's feet on adjacent slots do not
//     overlap. This is also the floor that makes compressing the spacing
//     pointless when the slots do not fit: below it the glyphs collide again
//     and the slot pass has stopped buying anything, which is why the demand
//     goes into the box's size instead.
//   - rankGap > circleOffset + circleR, so the whole optionality glyph fits in
//     the corridor between a box and the adjacent half-rank. It is also the
//     width of a corridor no route bends in: gapWidth widens a corridor only
//     for the channels it actually holds.
//   - channelPitch > strokeWidth, so two channels in one corridor are two
//     lines. Below one stroke width they merge into the single thick line the
//     channels exist to undo, which makes this a merge threshold rather than a
//     comfort margin - and the reason the pitch is never compressed to make a
//     crowded corridor fit.
//   - channelMargin > circleOffset + circleR + strokeWidth, so the outermost
//     channel on either side of a corridor clears the whole optionality glyph
//     of the boxes facing it, stroke included. Deliberately stricter than
//     rankGap's inequality above, which is written without the stroke term:
//     two strokes whose CENTRES are exactly circleOffset + circleR apart still
//     touch, and a trunk line drawn through somebody else's cardinality mark
//     trades one legibility defect for another that no invariant would notice,
//     because the glyphs are placed in scene construction and a Geometry does
//     not hold them.
//   - labelHeight = lineHeight + 2*cellPadV, so a label rectangle and a table
//     row are the same shape.
//   - lanePitch = labelHeight + 2*labelGap, so a label centred on one lane's
//     horizontal run cannot touch the lane next to it.
//
// The two derived values are written as their equations rather than as their
// results, so that the equalities above cannot be broken by editing one side
// of them.
const (
	// unit is one SVG pixel, and the reason a Coord is a TENTH of a pixel
	// rather than a whole one: sub-pixel positions are genuinely wanted - a
	// 1 px stroke centred on a boundary, a text baseline derived from a font
	// size - and a tenth of a pixel lets every one of them be an integer.
	//
	// Integers rather than fractions because determinism is a hard
	// requirement, and because it is what makes the predicates below EXACT:
	// "the segment lies along the box's boundary" and "the segment is a hair
	// inside it" are different answers here, with no tolerance to tune and no
	// way for the two to trade places. The geometry invariants are asserted
	// with those predicates, so this is also what stops them flaking.
	unit Coord = 10

	// Type. lineHeight is one line of text in a header or a cell; textAscent
	// is how far below the top of the em box the baseline sits, which a
	// monospace face puts at about 0.8 em. The exact value only shifts text
	// within its own band and cannot make it overflow, because the band's
	// height is a constant and the baseline is derived from it.
	fontSize   Coord = 120 // 12 px
	lineHeight Coord = 160 // 16 px
	textAscent Coord = 4 * fontSize / 5

	// Table boxes. dashLen is both the dash and the gap of the dashed stroke
	// the stub box is drawn with: it is a length in the drawing like every
	// other one here, and it is one value rather than two so that the dashes
	// and the gaps cannot drift apart. It is not in the Scene because Box
	// carries a dash FLAG - there is exactly one dashed thing in this drawing,
	// so a pattern there would be a presentation knob no document can reach.
	rowHeight   Coord = 200 // one column row
	cellPadH    Coord = 60  // left and right of a cell's text
	cellPadV    Coord = 40  // above and below a header's text
	strokeWidth Coord = 10  // 1 px
	dashLen     Coord = 40  // 4 px on, 4 px off

	// Separation. nodeSep is the minimum gap between two boxes in one rank,
	// rankGap the minimum gap between two adjacent DOUBLED ranks, margin the
	// blank border around the whole drawing.
	nodeSep      Coord = 160
	rankGap      Coord = 280
	componentGap Coord = 320
	margin       Coord = 160

	// The channels an inter-rank route turns on, inside the corridor between
	// two columns. channelPitch is the distance between two of them and
	// channelMargin insets the band of them from both corridor boundaries;
	// gapWidth in corridor.go turns a channel count into a corridor width, and
	// interRankPoints has the argument for why a line anywhere in that band is
	// as safe as the column boundary it replaced.
	//
	// The pitch is FIXED and the corridor widens to fit, which is the same
	// answer slotExtent gives for a crowded side and for a floor an order of
	// magnitude lower: a slot's floor is the crow's-foot glyph's own height,
	// and a channel carries no glyph, so its floor is one stroke width. A pitch
	// that varied per corridor would make two corridors of one drawing look
	// like different distances.
	channelPitch  Coord = 40
	channelMargin Coord = 220

	// Labels and the lanes the two special routing categories share.
	// loopRise is the distance from a box's top edge to the first lane.
	labelGap    Coord = 40
	labelHeight Coord = lineHeight + 2*cellPadV
	lanePitch   Coord = labelHeight + 2*labelGap
	loopRise    Coord = 160

	// Attachment slots along a box's side.
	slotSpacing Coord = 140
	slotMargin  Coord = 80

	// Crow's foot glyphs. crowLen is the depth of the foot, crowHalf half its
	// spread; barHalf is half the length of the bar and barOffset the distance
	// from the attachment point to the CARDINALITY mark. circleR is the
	// optionality circle's radius and circleOffset the distance to the
	// OPTIONALITY mark - the circle's centre when the end is optional, and the
	// bar's line when it is mandatory, so that the two forms of that mark
	// occupy the same place and every end carries its ink at the same two
	// offsets.
	crowLen      Coord = 100
	crowHalf     Coord = 50
	barHalf      Coord = 50
	barOffset    Coord = 100
	circleR      Coord = 30
	circleOffset Coord = 160
)

// ---------------------------------------------------------------------------
// Shapes
// ---------------------------------------------------------------------------

// Point is a position in the drawing.
type Point struct{ X, Y Coord }

// Rect is an axis-aligned rectangle, given by its top-left corner and its
// size. Y grows downwards, as it does in SVG.
type Rect struct{ X, Y, W, H Coord }

// Right is the x of r's right edge.
func (r Rect) Right() Coord { return r.X + r.W }

// Bottom is the y of r's bottom edge.
func (r Rect) Bottom() Coord { return r.Y + r.H }

// Segment is a straight line from A to B. Every segment this package routes is
// axis-aligned; see axisAligned.
type Segment struct{ A, B Point }

// shift is p moved by delta.
//
// A named operation rather than two lines of arithmetic at each call site,
// because a translation that moved x and forgot y is the one mistake in a
// rigid-body move that no bounds check catches: the drawing would still fit
// its page, still be axis-aligned and still have every endpoint on a boundary,
// and it would be sheared.
func (p Point) shift(delta Point) Point { return Point{X: p.X + delta.X, Y: p.Y + delta.Y} }

// shift is r moved by delta. Its SIZE is untouched, which is the whole of what
// a rigid translation does to a rectangle and the reason this is not a general
// transform.
func (r Rect) shift(delta Point) Rect {
	return Rect{X: r.X + delta.X, Y: r.Y + delta.Y, W: r.W, H: r.H}
}

// at is the zero-size rectangle at p.
//
// Rect.union takes both of its arguments as closed point sets, so folding a
// route point into a bounds accumulation through this needs no second code
// path - and a bounds that skipped the route points would clip exactly the
// documents whose routes leave the boxes' extent.
func at(p Point) Rect { return Rect{X: p.X, Y: p.Y} }

// ---------------------------------------------------------------------------
// Predicates
// ---------------------------------------------------------------------------

// intersectsInterior reports whether r and o share any INTERIOR point.
//
// Interior, not closed, is the right word for every one of these predicates,
// and it is the word the geometry invariants are written in: two boxes that
// share an edge exactly, and a lane that runs along a box's side, are legal and
// common drawings, and a closed test would report both as collisions. The
// invariants can therefore be stated as "= 0" rather than as a tolerance.
//
// A strict overlap on each axis, plus the emptiness guard: a rectangle with no
// width or no height HAS no interior, so it can overlap nothing. The guard is
// not defensive - a virtual node is a real, zero-width rectangle, and without
// it a zero-width rectangle standing inside a box would be reported as
// overlapping it.
func (r Rect) intersectsInterior(o Rect) bool {
	if r.W <= 0 || r.H <= 0 || o.W <= 0 || o.H <= 0 {
		return false
	}
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// union is the smallest rectangle covering both r and o.
//
// Both are taken as CLOSED point sets, unlike everywhere else here, because
// this is how the drawing's bounds are accumulated and a zero-size rectangle
// is still a point that has to end up inside them. The consequence for callers
// is that the accumulation must start from the first rectangle and never from
// a zero Rect, which would drag the origin into the result.
func (r Rect) union(o Rect) Rect {
	x, y := min(r.X, o.X), min(r.Y, o.Y)
	right, bottom := max(r.Right(), o.Right()), max(r.Bottom(), o.Bottom())
	return Rect{X: x, Y: y, W: right - x, H: bottom - y}
}

// contains reports whether p lies in r, boundary included.
//
// Closed, again unlike intersectsInterior, and for the same reason union is:
// the question it answers is whether a piece of drawn ink is inside the page,
// and ink exactly on the page's edge is inside it.
func (r Rect) contains(p Point) bool {
	return r.X <= p.X && p.X <= r.Right() && r.Y <= p.Y && p.Y <= r.Bottom()
}

// onBoundary reports whether p lies exactly on one of r's four sides.
//
// This is what "every relationship endpoint lies on its node's boundary"
// checks. Exactly is meant literally: an attachment point is computed from the
// box's own edges, so an endpoint a tenth of a pixel inside or outside is an
// arithmetic mistake, not a rounding artefact.
func onBoundary(p Point, r Rect) bool {
	if !r.contains(p) {
		return false
	}
	return p.X == r.X || p.X == r.Right() || p.Y == r.Y || p.Y == r.Bottom()
}

// axisAligned reports whether s runs along one of the two axes.
//
// A zero-length segment answers true. That is not a special case to guard at
// the call sites: it is degenerate, it is trivially parallel to both axes, and
// the invariant that every routed segment is axis-aligned is about the ones
// that have a direction.
func (s Segment) axisAligned() bool { return s.A.X == s.B.X || s.A.Y == s.B.Y }

// horizontal and vertical are axisAligned split into the two cases, with the
// degenerate segment belonging to neither: a zero-length segment has no
// direction, so it cannot run collinearly with anything.
func (s Segment) horizontal() bool { return s.A.Y == s.B.Y && s.A.X != s.B.X }
func (s Segment) vertical() bool   { return s.A.X == s.B.X && s.A.Y != s.B.Y }

// intersectsInterior reports whether s passes through r's interior.
//
// The two axis-aligned cases are handled separately and there is no general
// segment/rectangle clipper here, deliberately: no routed segment is ever
// diagonal, and a general routine would be a case analysis nobody can check by
// reading it. A segment that is neither horizontal nor vertical is therefore
// answered false rather than answered wrongly - the invariant that every
// segment is axis-aligned is asserted separately and is what makes that
// branch unreachable, so inventing geometry for it here would only hide the
// failure.
func (s Segment) intersectsInterior(r Rect) bool {
	if r.W <= 0 || r.H <= 0 {
		return false
	}
	switch {
	case s.horizontal():
		lo, hi := min(s.A.X, s.B.X), max(s.A.X, s.B.X)
		return r.Y < s.A.Y && s.A.Y < r.Bottom() && lo < r.Right() && r.X < hi
	case s.vertical():
		lo, hi := min(s.A.Y, s.B.Y), max(s.A.Y, s.B.Y)
		return r.X < s.A.X && s.A.X < r.Right() && lo < r.Bottom() && r.Y < hi
	default:
		return false
	}
}

// collinearOverlap reports whether a and b run along the same line and share
// more than a single point.
//
// More than a point is the whole subtlety: two segments of ONE route meet end
// to end at every corner, and a predicate that counted a shared endpoint as an
// overlap would report every corner in the diagram. What it is meant to catch
// is two different relationships drawn along the same line, which is the one
// way two edges become indistinguishable to a reader.
func collinearOverlap(a, b Segment) bool {
	switch {
	case a.horizontal() && b.horizontal():
		return a.A.Y == b.A.Y && overlapsMoreThanAPoint(a.A.X, a.B.X, b.A.X, b.B.X)
	case a.vertical() && b.vertical():
		return a.A.X == b.A.X && overlapsMoreThanAPoint(a.A.Y, a.B.Y, b.A.Y, b.B.Y)
	default:
		return false
	}
}

// overlapsMoreThanAPoint reports whether the closed intervals [a1,a2] and
// [b1,b2], given in either order, share an interval of non-zero length.
func overlapsMoreThanAPoint(a1, a2, b1, b2 Coord) bool {
	lo := max(min(a1, a2), min(b1, b2))
	hi := min(max(a1, a2), max(b1, b2))
	return lo < hi
}
