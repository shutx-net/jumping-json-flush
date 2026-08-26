package svg

import (
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ptr returns a pointer to n, so that a table row can declare an optional
// numeric column attribute inline.
func ptr(n int) *int { return &n }

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want int
	}{
		{"an ASCII letter", 'A', advanceNarrow},
		{"a space", ' ', advanceNarrow},
		{"a Kanji", '漢', advanceWide},
		{"a Hiragana", 'あ', advanceWide},
		{"a Katakana", 'ア', advanceWide},
		{"a Hangul syllable", '가', advanceWide},
		{"a fullwidth digit", '１', advanceWide},
		// The Ambiguous rule, and the reason for it: the Ambiguous set holds
		// the whole of Cyrillic and the whole of Greek, so counting it wide
		// would double the width of every Russian logical name.
		{"a Cyrillic letter", 'Ж', advanceNarrow},
		{"a Greek letter", 'Ω', advanceNarrow},
		// What an invalid byte decodes to, and what xml.EscapeText will
		// substitute for an XML-invalid rune. It must not be wide.
		{"the replacement character", '�', advanceNarrow},
		{"a supplementary-plane CJK ideograph", '𠀋', advanceWide},

		// The four rows around one range's edges: a binary search that is off
		// by one is visible here and nowhere else.
		{"one below a range's first rune", 0x10FF, advanceNarrow},
		{"a range's first rune", 0x1100, advanceWide},
		{"a range's last rune", 0x115F, advanceWide},
		{"one above a range's last rune", 0x1160, advanceNarrow},
		// Between two ranges, and above the last one.
		{"between two ranges", 0xFE20, advanceNarrow},
		{"above the last range", 0x40000, advanceNarrow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runeWidth(tt.r); got != tt.want {
				t.Errorf("runeWidth(%U) = %d, want %d", tt.r, got, tt.want)
			}
		})
	}
}

// TestWideRangesAreSortedAndDisjoint walks the whole table rather than asking
// about runes somebody thought of. The table is data: a typo in an entry
// nobody named is invisible to every other test, and it would also break the
// binary search's precondition, which is what this asserts.
func TestWideRangesAreSortedAndDisjoint(t *testing.T) {
	for i, rng := range wideRanges {
		if rng[0] > rng[1] {
			t.Errorf("wideRanges[%d] = %U..%U is empty: the start is above the end", i, rng[0], rng[1])
		}
		if i > 0 && rng[0] <= wideRanges[i-1][1] {
			t.Errorf("wideRanges[%d] starts at %U, which is not above wideRanges[%d]'s end %U",
				i, rng[0], i-1, wideRanges[i-1][1])
		}
	}
}

func TestMeasureTextEmpty(t *testing.T) {
	if got := measureText(""); got != 0 {
		t.Errorf("measureText(%q) = %d, want 0", "", got)
	}
}

// TestMeasureTextIsAnUpperBound pins the property the whole approach rests on.
// The naive comparison rounds each rune DOWN, which is the widest thing a cell
// could be sized from and still be too small, so measuring at least that much
// is what "never wider than its cell" means arithmetically.
func TestMeasureTextIsAnUpperBound(t *testing.T) {
	strs := []string{"", "id", "orders", "受注明細", "混在mixed１２３", "Привет", "Ωμέγα", "(not defined in this document)"}

	for _, s := range strs {
		var floored Coord
		for _, r := range s {
			floored += Coord(runeWidth(r)) * fontSize / emUnits
		}
		if got := measureText(s); got < floored {
			t.Errorf("measureText(%q) = %d, want at least %d", s, got, floored)
		}
	}

	// Rounding once cannot cost more than a single unit against rounding
	// twice, which is the whole gain of accumulating before converting.
	for _, a := range strs {
		for _, b := range strs {
			joined, apart := measureText(a+b), measureText(a)+measureText(b)
			if joined > apart+1 {
				t.Errorf("measureText(%q+%q) = %d, want at most %d", a, b, joined, apart+1)
			}
		}
	}
}

// TestMeasureTextIsAdditiveInRunes is the assertion per-character rounding
// would break: three times a ten-character string is a thirty-character
// string, to within the one unit the single final rounding can cost.
func TestMeasureTextIsAdditiveInRunes(t *testing.T) {
	ten := measureText(strings.Repeat("a", 10))
	thirty := measureText(strings.Repeat("a", 30))

	if diff := thirty - 3*ten; diff > 1 || diff < -1 {
		t.Errorf("measureText(30 runes) = %d, want %d (3 x %d) to within one unit", thirty, 3*ten, ten)
	}
}

// ordersTable is the fixture the box measurements below are stated against:
// two columns, one of them in the primary key, one carrying a length.
func ordersTable() *model.Table {
	return &model.Table{
		Name:        "orders",
		LogicalName: "受注",
		Columns: []model.Column{
			{Name: "id", LogicalName: "受注ID", Type: "BIGINT"},
			{Name: "note", LogicalName: "備考", Type: "VARCHAR", Length: ptr(255), Nullable: true},
		},
		PrimaryKey: &model.PrimaryKey{Columns: []string{"id"}},
	}
}

func TestMeasureTable(t *testing.T) {
	c := measureTable(ordersTable())

	// The four cells come from erd: this asserts the wiring, not the
	// derivation, which internal/export/erd's own tests pin.
	wantCells := [][]string{
		{"PK", "id", "受注ID", "BIGINT"},
		{"", "note", "備考", "VARCHAR(255)"},
	}
	if len(c.cells) != len(wantCells) {
		t.Fatalf("measured %d row(s), want %d", len(c.cells), len(wantCells))
	}
	for i, want := range wantCells {
		if !slices.Equal(c.cells[i], want) {
			t.Errorf("cells[%d] = %q, want %q", i, c.cells[i], want)
		}
	}

	if len(c.colW) != cellColumns {
		t.Fatalf("measured %d cell column(s), want %d", len(c.colW), cellColumns)
	}
	for j := range c.colW {
		var widest Coord
		for _, row := range c.cells {
			widest = max(widest, measureText(row[j]))
		}
		if want := widest + 2*cellPadH; c.colW[j] != want {
			t.Errorf("colW[%d] = %d, want %d (widest cell plus padding)", j, c.colW[j], want)
		}
	}

	// The x offsets are cumulative and the last cell ends at the box's right
	// edge, so a row's cells span the whole box.
	var x Coord
	for j := range c.colX {
		if c.colX[j] != x {
			t.Errorf("colX[%d] = %d, want %d", j, c.colX[j], x)
		}
		x += c.colW[j]
	}
	if last := len(c.colW) - 1; c.colX[last]+c.colW[last] != c.width {
		t.Errorf("the last cell ends at %d, want the box width %d", c.colX[last]+c.colW[last], c.width)
	}

	if want := 2*lineHeight + 2*cellPadV; c.headerHeight != want {
		t.Errorf("headerHeight = %d, want %d", c.headerHeight, want)
	}
	if want := c.headerHeight + 2*rowHeight; c.height != want {
		t.Errorf("height = %d, want %d (header plus two rows)", c.height, want)
	}
	if !slices.Equal(c.rowHeights, []Coord{rowHeight, rowHeight}) {
		t.Errorf("rowHeights = %v, want two rows of %d", c.rowHeights, rowHeight)
	}

	// Every baseline sits inside its own band, in order. Text that sits
	// outside its band is text drawn over the row above it.
	if !(0 < c.headerBaselines[0] && c.headerBaselines[0] < c.headerBaselines[1] && c.headerBaselines[1] < c.headerHeight) {
		t.Errorf("header baselines %v are not both inside the header band of %d", c.headerBaselines, c.headerHeight)
	}
	for i, b := range c.rowBaselines {
		top := c.headerHeight + Coord(i)*rowHeight
		if b <= top || b >= top+rowHeight {
			t.Errorf("rowBaselines[%d] = %d, want it inside the band %d..%d", i, b, top, top+rowHeight)
		}
	}
}

// TestMeasureTableJapanese pins the one observable consequence of the wide
// range table: the same table with the same number of runes in its logical
// names is wider when those runes are Japanese.
func TestMeasureTableJapanese(t *testing.T) {
	ascii := measureTable(&model.Table{
		Name:        "t",
		LogicalName: "t",
		Columns:     []model.Column{{Name: "id", LogicalName: "abcd", Type: "BIGINT"}},
	})
	japanese := measureTable(&model.Table{
		Name:        "t",
		LogicalName: "t",
		Columns:     []model.Column{{Name: "id", LogicalName: "受注明細", Type: "BIGINT"}},
	})

	if japanese.width <= ascii.width {
		t.Errorf("a Japanese logical name measured %d wide, want more than the %d of an ASCII name of the same rune count",
			japanese.width, ascii.width)
	}
	// Only the width differs: the two boxes have the same rows, so they have
	// the same height.
	if japanese.height != ascii.height {
		t.Errorf("height = %d for Japanese and %d for ASCII, want them equal", japanese.height, ascii.height)
	}
}

// TestMeasureTableWidensForALongHeader covers the case the cell columns cannot
// answer on their own. The header spans them all, so a logical name longer
// than every cell under it has to widen the box - otherwise it would be the
// one string in the drawing whose box was not measured from it.
func TestMeasureTableWidensForALongHeader(t *testing.T) {
	const longName = "受注明細テーブルの拡張属性"

	c := measureTable(&model.Table{
		Name:        "t",
		LogicalName: longName,
		Columns:     []model.Column{{Name: "a", LogicalName: "あ", Type: "INT"}},
	})

	if want := measureText(longName) + 2*cellPadH; c.width != want {
		t.Errorf("width = %d, want %d (the header's own text plus padding)", c.width, want)
	}
	if last := len(c.colW) - 1; c.colX[last]+c.colW[last] != c.width {
		t.Errorf("the last cell ends at %d, want the box width %d: the surplus was not given to a column",
			c.colX[last]+c.colW[last], c.width)
	}
}

func TestMeasureStub(t *testing.T) {
	const name = "nodes"

	c := measureStub(name)

	if c.headerLines != [2]string{name, stubNote} {
		t.Errorf("headerLines = %q, want %q", c.headerLines, [2]string{name, stubNote})
	}
	if len(c.cells) != 0 {
		t.Errorf("a stub measured %d row(s), want none: it invents no columns", len(c.cells))
	}
	if len(c.colW) != 1 {
		t.Errorf("a stub measured %d cell column(s), want one", len(c.colW))
	}
	if want := 2*lineHeight + 2*cellPadV; c.height != want {
		t.Errorf("height = %d, want %d (two lines of text)", c.height, want)
	}
	if want := max(measureText(name), measureText(stubNote)) + 2*cellPadH; c.width != want {
		t.Errorf("width = %d, want %d (the wider of its two lines plus padding)", c.width, want)
	}
}

// TestFontStackEndsInGenericMonospace pins the half of the measurement
// argument that lives in the font stack rather than in the numbers. The
// advances are an upper bound for monospace faces and for nothing else, so the
// last resort has to be the generic monospace family: a stack that fell
// through to the reader's default sans face would make every box width in the
// diagram a guess, and nothing in the output would show it.
func TestFontStackEndsInGenericMonospace(t *testing.T) {
	if !strings.HasSuffix(fontFamily, "monospace") {
		t.Errorf("fontFamily = %s, want it to end in the generic monospace family", fontFamily)
	}
}
