package sml

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
)

// Sheet limits imposed by the file format.
const (
	colLimit = 16384   // the last column is XFD
	rowLimit = 1048576 // the last row
)

// Sheet is one worksheet of a Workbook. Cells are held sparsely and emitted in
// ascending row and column order.
//
// All coordinates are 1-based. Passing a column or row outside the sheet limits
// is a programming error and panics rather than silently dropping data.
type Sheet struct {
	wb   *Workbook
	name string

	rows   map[int]*sheetRow
	cols   []colWidth
	merges []cellRange

	freezeCols       int
	freezeRows       int
	tabSelected      bool
	defaultRowHeight float64

	minCol, minRow int
	maxCol, maxRow int
}

type sheetRow struct {
	height float64
	cells  map[int]cellData
}

type colWidth struct {
	min, max int
	width    float64
}

type cellRange struct {
	col1, row1, col2, row2 int
}

type cellKind uint8

const (
	cellBlank cellKind = iota
	cellNumber
	cellString
	cellError
)

type cellData struct {
	kind  cellKind
	style StyleID
	// text holds the inline string for cellString and the literal written to
	// <v> for cellNumber and cellError.
	text string
}

func newSheet(wb *Workbook, name string) *Sheet {
	return &Sheet{wb: wb, name: name, rows: make(map[int]*sheetRow)}
}

// Name returns the sheet name that was actually allocated, which may differ
// from the name passed to Workbook.AddSheet after sanitising and deduplication.
func (s *Sheet) Name() string { return s.name }

// SetString writes a text cell. The value is stripped of characters XML cannot
// represent and its line endings are normalised to LF, so callers never have to
// pre-process text themselves.
func (s *Sheet) SetString(col, row int, v string, style StyleID) {
	text := sanitizeXMLText(normalizeNewlines(v))
	s.set(col, row, cellData{kind: cellString, style: style, text: text})
}

// SetInt writes a numeric cell.
func (s *Sheet) SetInt(col, row int, v int, style StyleID) {
	s.set(col, row, cellData{kind: cellNumber, style: style, text: strconv.Itoa(v)})
}

// SetFloat writes a numeric cell using the shortest representation that round
// trips. NaN and infinity have no numeric spelling in SpreadsheetML and become
// a #NUM! error cell.
func (s *Sheet) SetFloat(col, row int, v float64, style StyleID) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		s.set(col, row, cellData{kind: cellError, style: style, text: "#NUM!"})
		return
	}
	s.set(col, row, cellData{
		kind:  cellNumber,
		style: style,
		text:  strconv.FormatFloat(v, 'f', -1, 64),
	})
}

// SetBlank writes an empty cell that carries only a style. Merged ranges need
// this: a border set on the top left cell alone is not drawn around the whole
// range, so every cell of the range must carry the same border style.
func (s *Sheet) SetBlank(col, row int, style StyleID) {
	s.set(col, row, cellData{kind: cellBlank, style: style})
}

func (s *Sheet) set(col, row int, c cellData) {
	checkCell(col, row)
	r := s.rows[row]
	if r == nil {
		r = &sheetRow{cells: make(map[int]cellData)}
		s.rows[row] = r
	}
	r.cells[col] = c
	s.grow(col, row)
}

func (s *Sheet) grow(col, row int) {
	if s.minCol == 0 || col < s.minCol {
		s.minCol = col
	}
	if s.minRow == 0 || row < s.minRow {
		s.minRow = row
	}
	if col > s.maxCol {
		s.maxCol = col
	}
	if row > s.maxRow {
		s.maxRow = row
	}
}

// SetColWidth sets the width of the columns from first to last, inclusive. The
// width is measured in characters of the default font's digit zero, so a column
// showing n half-width characters needs roughly n+0.71.
func (s *Sheet) SetColWidth(first, last int, width float64) {
	if first < 1 || last < first || last > colLimit {
		panic(fmt.Sprintf("sml: column range %d..%d out of range", first, last))
	}
	s.cols = append(s.cols, colWidth{min: first, max: last, width: width})
}

// SetRowHeight sets the height of a row in points.
func (s *Sheet) SetRowHeight(row int, height float64) {
	if row < 1 || row > rowLimit {
		panic(fmt.Sprintf("sml: row %d out of range", row))
	}
	r := s.rows[row]
	if r == nil {
		r = &sheetRow{cells: make(map[int]cellData)}
		s.rows[row] = r
	}
	r.height = height
}

// SetDefaultRowHeight sets the height in points used by rows that have no
// explicit height.
func (s *Sheet) SetDefaultRowHeight(height float64) {
	s.defaultRowHeight = height
}

// ErrMergeOverlap is returned when a merge range intersects an existing one.
var ErrMergeOverlap = errors.New("sml: merge range overlaps an existing merge")

// Merge merges a rectangular range of cells. The value and the number format
// are taken from the top left cell, but borders are not: use SetBlank to give
// every cell of the range the same bordered style.
func (s *Sheet) Merge(col1, row1, col2, row2 int) error {
	checkCell(col1, row1)
	checkCell(col2, row2)
	r := cellRange{col1: col1, row1: row1, col2: col2, row2: row2}
	if r.col1 > r.col2 {
		r.col1, r.col2 = r.col2, r.col1
	}
	if r.row1 > r.row2 {
		r.row1, r.row2 = r.row2, r.row1
	}
	for _, other := range s.merges {
		if r.overlaps(other) {
			return fmt.Errorf("%w: %s and %s", ErrMergeOverlap, r.ref(), other.ref())
		}
	}
	s.merges = append(s.merges, r)
	s.grow(r.col2, r.row2)
	s.grow(r.col1, r.row1)
	return nil
}

// Freeze freezes the leftmost cols columns and the topmost rows rows. Freeze(0,
// 0) removes the split.
func (s *Sheet) Freeze(cols, rows int) {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	s.freezeCols, s.freezeRows = cols, rows
}

// SetTabSelected marks this sheet as the one selected when the workbook opens.
// Only one sheet may be selected, so selecting one deselects the others.
func (s *Sheet) SetTabSelected(v bool) {
	if v {
		for _, other := range s.wb.sheets {
			other.tabSelected = false
		}
	}
	s.tabSelected = v
}

func (r cellRange) overlaps(o cellRange) bool {
	return r.col1 <= o.col2 && o.col1 <= r.col2 && r.row1 <= o.row2 && o.row1 <= r.row2
}

func (r cellRange) ref() string {
	return rangeRef(r.col1, r.row1, r.col2, r.row2)
}

func checkCell(col, row int) {
	if col < 1 || col > colLimit {
		panic(fmt.Sprintf("sml: column %d out of range 1..%d", col, colLimit))
	}
	if row < 1 || row > rowLimit {
		panic(fmt.Sprintf("sml: row %d out of range 1..%d", row, rowLimit))
	}
}

// xml renders the sheet. Sparse rows and cells live in maps, so their keys are
// collected and sorted; the map is never ranged over to produce output order.
func (s *Sheet) xml() *xlWorksheet {
	ws := &xlWorksheet{
		Xmlns:      nsSpreadsheetML,
		Dimension:  &xlDimension{Ref: s.dimension()},
		SheetViews: s.sheetViews(),
	}
	if s.defaultRowHeight > 0 {
		ws.SheetFormatPr = &xlSheetFormat{
			DefaultRowHeight: s.defaultRowHeight,
			CustomHeight:     1,
		}
	}
	if len(s.cols) > 0 {
		cols := &xlCols{Col: make([]xlCol, 0, len(s.cols))}
		for _, c := range s.cols {
			cols.Col = append(cols.Col, xlCol{
				Min:         c.min,
				Max:         c.max,
				Width:       c.width,
				CustomWidth: 1,
			})
		}
		ws.Cols = cols
	}

	nums := make([]int, 0, len(s.rows))
	for n := range s.rows {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	ws.SheetData.Row = make([]xlRow, 0, len(nums))
	for _, n := range nums {
		ws.SheetData.Row = append(ws.SheetData.Row, s.rows[n].xml(n))
	}

	if len(s.merges) > 0 {
		mc := &xlMergeCells{Count: len(s.merges), MergeCell: make([]xlMergeCell, 0, len(s.merges))}
		for _, m := range s.merges {
			mc.MergeCell = append(mc.MergeCell, xlMergeCell{Ref: m.ref()})
		}
		ws.MergeCells = mc
	}
	return ws
}

func (r *sheetRow) xml(num int) xlRow {
	out := xlRow{R: num}
	if r.height > 0 {
		out.Ht = r.height
		out.CustomHeight = 1
	}
	cols := make([]int, 0, len(r.cells))
	for c := range r.cells {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	out.C = make([]xlCell, 0, len(cols))
	for _, c := range cols {
		out.C = append(out.C, r.cells[c].xml(c, num))
	}
	return out
}

func (c cellData) xml(col, row int) xlCell {
	out := xlCell{R: cellRef(col, row), S: int(c.style)}
	switch c.kind {
	case cellString:
		out.T = "inlineStr"
		t := xlText{Value: c.text}
		if needsSpacePreserve(c.text) {
			t.Space = "preserve"
		}
		out.Is = &xlInlineStr{T: t}
	case cellNumber:
		// Numeric cells carry no t attribute: "n" is the default.
		out.V = c.text
	case cellError:
		out.T = "e"
		out.V = c.text
	case cellBlank:
	}
	return out
}

// dimension returns the used range of the sheet, or A1 when nothing is written.
func (s *Sheet) dimension() string {
	if s.minCol == 0 {
		return "A1"
	}
	return rangeRef(s.minCol, s.minRow, s.maxCol, s.maxRow)
}

// sheetViews derives the frozen pane from Freeze. The activePane and
// topLeftCell values depend on which axes are split:
//
//	rows only    ySplit=rows            topLeftCell=A{rows+1}          bottomLeft
//	cols only    xSplit=cols            topLeftCell={col(cols+1)}1     topRight
//	both         xSplit=cols ySplit=rows topLeftCell={col+1}{rows+1}   bottomRight
func (s *Sheet) sheetViews() *xlSheetViews {
	view := xlSheetView{WorkbookViewID: 0}
	if s.tabSelected {
		view.TabSelected = 1
	}
	if s.freezeCols > 0 || s.freezeRows > 0 {
		topLeft := cellRef(s.freezeCols+1, s.freezeRows+1)
		pane := &xlPane{TopLeftCell: topLeft, State: "frozen"}
		switch {
		case s.freezeCols > 0 && s.freezeRows > 0:
			pane.XSplit = float64(s.freezeCols)
			pane.YSplit = float64(s.freezeRows)
			pane.ActivePane = "bottomRight"
		case s.freezeCols > 0:
			pane.XSplit = float64(s.freezeCols)
			pane.ActivePane = "topRight"
		default:
			pane.YSplit = float64(s.freezeRows)
			pane.ActivePane = "bottomLeft"
		}
		view.Pane = pane
		view.Selection = &xlSelection{
			Pane:       pane.ActivePane,
			ActiveCell: topLeft,
			Sqref:      topLeft,
		}
	}
	return &xlSheetViews{SheetView: []xlSheetView{view}}
}
