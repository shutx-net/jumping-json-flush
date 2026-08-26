package svg

import (
	"slices"

	"github.com/shutx-net/jumping-json-flush/internal/export/erd"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// The face, and how wide it is
// ---------------------------------------------------------------------------

// fontFamily is the face every piece of text in the diagram asks for.
//
// The stack and the advance widths below are ONE decision and live next to
// each other on purpose: the estimate is an upper bound for monospace faces
// and for nothing else, so a stack that could resolve to a proportional face
// would silently invalidate every box width in the drawing. The generic
// monospace keyword at the end is what makes the guarantee hold on a machine
// that has none of the named faces, including for the CJK glyphs no Latin
// monospace face carries.
//
// Single quotes because the value is written into a double-quoted XML
// attribute; a family name containing a space has to be quoted in CSS, and
// quoting it this way needs no escaping on the way out.
const fontFamily = "'DejaVu Sans Mono', 'Menlo', 'Consolas', monospace"

// Advance widths, in thousandths of an em. These are not geometry - they are
// the metric itself - which is why they are here rather than in coord.go's
// block; the one geometry value the measurement reads is fontSize.
const (
	advanceNarrow = 620
	advanceWide   = 1200
	emUnits       = 1000
)

// wideRanges are the East Asian Wide and Fullwidth code point ranges of
// UAX #11, sorted and disjoint, each entry inclusive at both ends.
//
// A hand-written table rather than unicode.Is(unicode.Han, r) or any other
// standard library range table, because script is not width: a fullwidth digit
// is Latin script and takes two cells, and a Cyrillic letter is not Han and
// takes one. golang.org/x/text has the real property, and taking it would
// break the project's zero module dependencies for a table of seventeen
// ranges.
//
// The table is data, so a typo in it is invisible to the compiler and to every
// test that only asks about a rune somebody thought of. The sortedness test
// walks the whole table instead and asserts the shape the binary search
// requires, which is the only check that sees an entry nobody named.
var wideRanges = [][2]rune{
	{0x1100, 0x115F},   // Hangul Jamo initial consonants
	{0x2E80, 0x303E},   // CJK radicals, Kangxi radicals, CJK symbols and punctuation
	{0x3041, 0x33FF},   // Hiragana, Katakana, Bopomofo, Hangul compatibility Jamo, CJK compatibility
	{0x3400, 0x4DBF},   // CJK unified ideographs extension A
	{0x4E00, 0x9FFF},   // CJK unified ideographs
	{0xA000, 0xA4CF},   // Yi syllables and radicals
	{0xA960, 0xA97F},   // Hangul Jamo extended-A
	{0xAC00, 0xD7A3},   // Hangul syllables
	{0xF900, 0xFAFF},   // CJK compatibility ideographs
	{0xFE10, 0xFE19},   // vertical forms
	{0xFE30, 0xFE6F},   // CJK compatibility forms, small form variants
	{0xFF00, 0xFF60},   // fullwidth ASCII variants
	{0xFFE0, 0xFFE6},   // fullwidth signs
	{0x1F300, 0x1F64F}, // miscellaneous symbols and pictographs, emoticons
	{0x1F900, 0x1F9FF}, // supplemental symbols and pictographs
	{0x20000, 0x2FFFD}, // CJK unified ideographs extensions B and later
	{0x30000, 0x3FFFD}, // CJK unified ideographs extension G and later
}

// runeWidth estimates r's advance width, in thousandths of an em.
//
// Both figures are UPPER BOUNDS, and that is the whole trick. Text sized from
// an upper bound can come out narrower than the cell it was given and never
// wider, so a box is never overflowed by the text it was measured for - the
// same trick internal/export/xlsx plays with its fixed column widths, which
// decline to measure anything at all. An estimate that were merely accurate on
// average would overflow half the time and there would be no way to tell which
// half.
//
// 620 rather than 600: 0.6 em is the figure usually quoted for a monospace
// face, but it is not a strict bound, because DejaVu Sans Mono and Menlo are
// both reported at about 0.602 em. Sitting exactly on a boundary that two
// common faces already cross is not a bound at all, so the number carries
// headroom instead. 1200 against a CJK monospace ideograph, whose advance is
// at most 1000, has room to spare for the same reason.
//
// The estimate was compared against the alternative on
// internal/export/svg/testdata/full.json: an embedded graphviz overflowed 7 of
// 24 Japanese cells and an estimate like this one overflowed 0 of 19. Both
// numbers were measured by the author of issue #32 on a prototype that is not
// in this tree, not on this code.
//
// The limit, stated rather than left to be found: UAX #11 AMBIGUOUS-width
// characters count as narrow here, so a logical name built mostly out of them
// can overflow its cell when the reader's face renders them wide. Counting
// them wide is the worse failure, because the Ambiguous set contains the whole
// of Greek and the whole of Cyrillic: every Russian logical name in every
// diagram would then reserve double the width it needs, to protect a name made
// mostly of Ambiguous punctuation in a CJK-configured face. This is a known
// bounded limit, not a bug waiting to be fixed.
func runeWidth(r rune) int {
	// A binary search rather than a walk, over a table whose sortedness a test
	// asserts: the shape of the data is what makes the search correct, so the
	// two belong together.
	_, wide := slices.BinarySearchFunc(wideRanges, r, func(rng [2]rune, target rune) int {
		switch {
		case rng[1] < target:
			return -1
		case target < rng[0]:
			return 1
		default:
			return 0
		}
	})
	if wide {
		return advanceWide
	}
	return advanceNarrow
}

// measureText estimates how wide s is when drawn at fontSize.
//
// The sum is accumulated in thousandths of an em and converted ONCE, rounding
// half up. The alternative is rounding each rune to a Coord and adding those,
// which over-reserves by up to 0.2 px per character - about 6 px on a
// thirty-character string, a figure measured in the issue #32 discussion and
// not on this code. That is most of a cell's padding wasted on every cell in
// the diagram, and it makes the width of a string depend on where it was cut
// rather than on what is in it. One division also keeps the whole package free
// of fractions, which is what lets the geometry predicates be exact.
//
// Rounding up rather than down at the halfway point keeps the result an upper
// bound of the estimate, which is the property runeWidth exists to provide.
func measureText(s string) Coord {
	thousandths := 0
	for _, r := range s {
		thousandths += runeWidth(r)
	}
	return (Coord(thousandths)*fontSize + emUnits/2) / emUnits
}

// ---------------------------------------------------------------------------
// The inside of a box
// ---------------------------------------------------------------------------

// cellColumns is the number of cells in one column row of a table box: the key
// marker, the physical name, the logical name and the rendered type. The
// header spans exactly this many, so the two have to move together: a fifth
// cell means editing measureTable and the header span in the same change or
// the box comes out ragged.
const cellColumns = 4

// stubNote is the second line of a stub box. It says, in the picture, the one
// thing the document does say about that table: that it does not define it.
// The stub invents nothing else - no columns, no keys, no types - because the
// document says nothing else about it.
const stubNote = "(not defined in this document)"

// content is one box's measured interior: the strings that go in it and where
// they go, relative to the box's own top-left corner.
//
// It is measured once, here, and carried through the layout untouched. Layout
// reads width and height - it has to, since that is what a box is sized from -
// and reads nothing else; the invariant tests read none of it; scene
// construction reads all of it. Measuring again at drawing time is the
// alternative this rejects: two measurements are two places that can disagree
// about one width, and the box was SIZED from the first, so the second is the
// one that overflows.
type content struct {
	// headerLines is the box's header: the logical name, which is drawn bold,
	// then the physical name. A stub box carries its table name and stubNote
	// here, which is what gives both kinds of box one shape and scene
	// construction one code path.
	headerLines [2]string

	// headerBaselines and rowBaselines are text baselines measured down from
	// the box's top edge; headerHeight is where the header band ends and the
	// first column row begins.
	headerBaselines [2]Coord
	headerHeight    Coord

	// cells[i] holds row i's strings, one per cell column, and rowHeights[i]
	// its height. A stub box has no rows.
	cells      [][]string
	rowHeights []Coord

	rowBaselines []Coord

	// colX and colW are each cell column's left edge and width, measured from
	// the box's left edge. The last column always ends at width, so a row's
	// cells span the whole box.
	colX []Coord
	colW []Coord

	// width and height are the box's intrinsic size: the smallest it can be
	// and still hold its content. Layout may make a box taller or wider than
	// this - a side with more attachment slots than it has room for is what
	// does it - and never smaller.
	width, height Coord
}

// measureTable measures t as a box: a header carrying the logical and physical
// names, then one row per column with its key marker, physical name, logical
// name and rendered type.
//
// The four cell strings come from erd, which answers what the document says
// about a column. Measuring is this package's job; deciding what a column's
// type reads as, or which keys it takes part in, is not, and asking erd rather
// than reimplementing it is what keeps the picture and the workbook talking
// about the same document.
func measureTable(t *model.Table) *content {
	cells := make([][]string, len(t.Columns))
	for i := range t.Columns {
		c := &t.Columns[i]
		cells[i] = []string{erd.Marker(t, c.Name), c.Name, c.LogicalName, erd.RenderType(c)}
	}
	return newContent([2]string{t.LogicalName, t.Name}, cells, cellColumns)
}

// measureStub measures the box for a table a foreign key names and the
// document does not define.
//
// Two lines and one cell column, and otherwise the shape a table box has, so
// that everything downstream - sizing, layout, drawing - has one kind of box
// to handle. What makes it read as a stub is how it is drawn, not how it is
// measured.
func measureStub(name string) *content {
	return newContent([2]string{name, stubNote}, nil, 1)
}

// newContent lays out a box's interior from its header lines and its rows.
func newContent(header [2]string, cells [][]string, cols int) *content {
	c := &content{
		headerLines:  header,
		cells:        cells,
		rowHeights:   make([]Coord, len(cells)),
		rowBaselines: make([]Coord, len(cells)),
		colX:         make([]Coord, cols),
		colW:         make([]Coord, cols),
	}

	// A cell column is as wide as its widest cell plus its padding, so every
	// string in it fits by construction. An empty column - a stub's, or a
	// table's marker column when no column carries a key - collapses to its
	// padding rather than to nothing, which keeps the borders evenly spaced.
	var total Coord
	for j := range c.colW {
		var w Coord
		for _, row := range cells {
			w = max(w, measureText(row[j]))
		}
		c.colW[j] = w + 2*cellPadH
		total += c.colW[j]
	}

	// The header spans every cell column, so its own text has to be able to
	// widen the box: a table with one short column and a long logical name is
	// ordinary, and without this the header would be the one string in the
	// drawing that was not measured into the box holding it.
	//
	// The surplus goes to the LAST column rather than being shared out.
	// Sharing needs a tie-break for the remainder, and the last column's right
	// edge is the box's right edge, so widening it moves no border a reader
	// can see against another box's.
	if needed := max(measureText(header[0]), measureText(header[1])) + 2*cellPadH; needed > total {
		c.colW[cols-1] += needed - total
		total = needed
	}

	var x Coord
	for j, w := range c.colW {
		c.colX[j] = x
		x += w
	}
	c.width = total

	// Two lines of text with one padding above and one below, which is the
	// same arithmetic labelHeight uses for one line, so a label rectangle and
	// a table row come out the same shape.
	c.headerHeight = 2*lineHeight + 2*cellPadV
	c.headerBaselines[0] = cellPadV + baselineIn(lineHeight)
	c.headerBaselines[1] = cellPadV + lineHeight + baselineIn(lineHeight)

	y := c.headerHeight
	for i := range cells {
		c.rowHeights[i] = rowHeight
		c.rowBaselines[i] = y + baselineIn(rowHeight)
		y += rowHeight
	}
	c.height = y

	return c
}

// baselineIn is the baseline of one line of text centred in a band of the
// given height, measured from the band's top edge: the em box is centred, and
// the baseline sits textAscent below its top.
func baselineIn(band Coord) Coord { return (band-fontSize)/2 + textAscent }
