package sml

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "update golden files")

func TestSanitizeXMLText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"control characters dropped", "bad\x00\x01\x0bchars", "badchars"},
		{"tab and newline kept", "ok\ttext\nline", "ok\ttext\nline"},
		{"carriage return kept", "a\rb", "a\rb"},
		{"japanese and emoji kept", "日本語①㈱髙🙂", "日本語①㈱髙🙂"},
		{"noncharacters dropped", "a￾b￿c", "abc"},
		{"markup left to the encoder", `a<b>&"'`, `a<b>&"'`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeXMLText(tt.in); got != tt.want {
				t.Errorf("sanitizeXMLText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeNormalizeNewlines(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\nb", "a\nb"},
		{"a\r\n\r\nb", "a\n\nb"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeNewlines(tt.in); got != tt.want {
			t.Errorf("normalizeNewlines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNeedsSpacePreserve(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"plain", false},
		{" leading", true},
		{"trailing ", true},
		{"\tleading tab", true},
		{"trailing newline\n", true},
		{"inner space kept", false},
		{"日本語", false},
	}
	for _, tt := range tests {
		if got := needsSpacePreserve(tt.in); got != tt.want {
			t.Errorf("needsSpacePreserve(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestColName(t *testing.T) {
	tests := []struct {
		col  int
		want string
	}{
		{0, ""},
		{-1, ""},
		{1, "A"},
		{2, "B"},
		{26, "Z"},
		{27, "AA"},
		{28, "AB"},
		{52, "AZ"},
		{53, "BA"},
		{702, "ZZ"},
		{703, "AAA"},
		{16384, "XFD"},
	}
	for _, tt := range tests {
		if got := colName(tt.col); got != tt.want {
			t.Errorf("colName(%d) = %q, want %q", tt.col, got, tt.want)
		}
	}
}

func TestCellAndRangeRef(t *testing.T) {
	if got := cellRef(3, 4); got != "C4" {
		t.Errorf("cellRef(3, 4) = %q, want C4", got)
	}
	if got := rangeRef(1, 1, 3, 2); got != "A1:C2" {
		t.Errorf("rangeRef(1, 1, 3, 2) = %q, want A1:C2", got)
	}
	if got := rangeRef(2, 5, 2, 5); got != "B5" {
		t.Errorf("rangeRef(2, 5, 2, 5) = %q, want B5", got)
	}
}

func TestSanitizeSheetName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"users", "users"},
		{"order/items", "order_items"},
		{`a[b]c:d*e?f/g\h`, "a_b_c_d_e_f_g_h"},
		{"'quoted'", "quoted"},
		{"", "Sheet"},
		{"public.users", "public.users"},
		{"tab\there", "tab_here"},
		{strings.Repeat("x", 40), strings.Repeat("x", 31)},
		{strings.Repeat("あ", 40), strings.Repeat("あ", 31)},
	}
	for _, tt := range tests {
		if got := sanitizeSheetName(tt.in); got != tt.want {
			t.Errorf("sanitizeSheetName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNameAllocator(t *testing.T) {
	// The sequence and the expectations come from the behaviour verified against
	// Excel's naming rules: sanitising, case-insensitive collisions, truncation.
	var a nameAllocator
	tests := []struct{ in, want string }{
		{"users", "users"},
		{"USERS", "USERS(2)"},
		{"users", "users(3)"},
		{"order/items", "order_items"},
		{"'quoted'", "quoted"},
		{"", "Sheet"},
		{"public.users", "public.users"},
		{strings.Repeat("x", 31), strings.Repeat("x", 31)},
		{strings.Repeat("x", 31), strings.Repeat("x", 28) + "(2)"},
	}
	for _, tt := range tests {
		if got := a.Allocate(tt.in); got != tt.want {
			t.Errorf("Allocate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSheetCellOrder(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	// Write out of order and across gaps; the output must be sorted.
	sh.SetString(3, 5, "c", 0)
	sh.SetString(1, 5, "a", 0)
	sh.SetString(2, 1, "b", 0)
	sh.SetInt(27, 2, 7, 0)

	ws := writeAndOpen(t, wb).sheet(t, 1)
	want := []string{"B1", "AA2", "A5", "C5"}
	if got := ws.refs(); !slices.Equal(got, want) {
		t.Errorf("cell order = %v, want %v", got, want)
	}
	rows := []int{}
	for _, r := range ws.SheetData.Row {
		rows = append(rows, r.R)
	}
	if want := []int{1, 2, 5}; !slices.Equal(rows, want) {
		t.Errorf("row numbers = %v, want %v", rows, want)
	}
}

func TestSheetDimension(t *testing.T) {
	tests := []struct {
		name  string
		write func(*Sheet)
		want  string
	}{
		{"empty sheet", func(*Sheet) {}, "A1"},
		{"single cell", func(s *Sheet) { s.SetString(2, 3, "x", 0) }, "B3"},
		{"range", func(s *Sheet) {
			s.SetString(2, 3, "x", 0)
			s.SetInt(5, 9, 1, 0)
		}, "B3:E9"},
		{"merge extends the range", func(s *Sheet) {
			s.SetString(1, 1, "x", 0)
			if err := s.Merge(1, 1, 4, 2); err != nil {
				t.Fatal(err)
			}
		}, "A1:D2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wb := New()
			tt.write(wb.AddSheet("s"))
			ws := writeAndOpen(t, wb).sheet(t, 1)
			if ws.Dimension.Ref != tt.want {
				t.Errorf("dimension = %q, want %q", ws.Dimension.Ref, tt.want)
			}
		})
	}
}

func TestSheetStringSanitizing(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	sh.SetString(1, 1, "bad\x00\x01\x0bchars", 0)
	sh.SetString(1, 2, "crlf\r\nlf", 0)

	cells := writeAndOpen(t, wb).sheet(t, 1).cells()
	if got := cells["A1"].text(); got != "badchars" {
		t.Errorf("A1 = %q, want %q", got, "badchars")
	}
	if got := cells["A2"].text(); got != "crlf\nlf" {
		t.Errorf("A2 = %q, want %q", got, "crlf\nlf")
	}
}

func TestSheetTabSelectedIsExclusive(t *testing.T) {
	wb := New()
	first := wb.AddSheet("a")
	wb.AddSheet("b")
	third := wb.AddSheet("c")
	if !first.tabSelected {
		t.Fatal("the first sheet should start out selected")
	}
	third.SetTabSelected(true)

	p := writeAndOpen(t, wb)
	for i, want := range []int{0, 0, 1} {
		got := p.sheet(t, i+1).SheetViews.SheetView[0].TabSelected
		if got != want {
			t.Errorf("sheet%d tabSelected = %d, want %d", i+1, got, want)
		}
	}
}

func TestFreezePane(t *testing.T) {
	tests := []struct {
		name                   string
		cols, rows             int
		xSplit, ySplit         float64
		topLeftCell, activePan string
	}{
		{"rows only", 0, 1, 0, 1, "A2", "bottomLeft"},
		{"columns only", 2, 0, 2, 0, "C1", "topRight"},
		{"both axes", 2, 3, 2, 3, "C4", "bottomRight"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wb := New()
			sh := wb.AddSheet("s")
			sh.Freeze(tt.cols, tt.rows)
			view := writeAndOpen(t, wb).sheet(t, 1).SheetViews.SheetView[0]
			if view.Pane == nil {
				t.Fatal("no pane emitted")
			}
			if view.Pane.XSplit != tt.xSplit || view.Pane.YSplit != tt.ySplit {
				t.Errorf("split = %v/%v, want %v/%v",
					view.Pane.XSplit, view.Pane.YSplit, tt.xSplit, tt.ySplit)
			}
			if view.Pane.TopLeftCell != tt.topLeftCell {
				t.Errorf("topLeftCell = %q, want %q", view.Pane.TopLeftCell, tt.topLeftCell)
			}
			if view.Pane.ActivePane != tt.activePan {
				t.Errorf("activePane = %q, want %q", view.Pane.ActivePane, tt.activePan)
			}
			if view.Pane.State != "frozen" {
				t.Errorf("state = %q, want frozen", view.Pane.State)
			}
			if view.Selection == nil || view.Selection.Pane != tt.activePan ||
				view.Selection.ActiveCell != tt.topLeftCell {
				t.Errorf("selection = %+v, want pane %q at %q",
					view.Selection, tt.activePan, tt.topLeftCell)
			}
		})
	}
}

func TestFreezePaneOmittedWithoutSplit(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	sh.Freeze(0, 0)
	if view := writeAndOpen(t, wb).sheet(t, 1).SheetViews.SheetView[0]; view.Pane != nil {
		t.Errorf("pane emitted for an unsplit sheet: %+v", view.Pane)
	}
}

func TestMergeOverlap(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	if err := sh.Merge(2, 2, 4, 4); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	overlapping := []struct {
		name           string
		c1, r1, c2, r2 int
	}{
		{"identical", 2, 2, 4, 4},
		{"contained", 3, 3, 3, 3},
		{"partial", 4, 4, 6, 6},
		{"reversed coordinates", 4, 4, 2, 2},
	}
	for _, tt := range overlapping {
		t.Run(tt.name, func(t *testing.T) {
			if err := sh.Merge(tt.c1, tt.r1, tt.c2, tt.r2); err == nil {
				t.Fatal("want an overlap error, got nil")
			} else if !strings.Contains(err.Error(), "overlap") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	if err := sh.Merge(5, 5, 6, 6); err != nil {
		t.Errorf("disjoint merge: %v", err)
	}
	if got := writeAndOpen(t, wb).sheet(t, 1).mergeRefs(); !slices.Equal(got, []string{"B2:D4", "E5:F6"}) {
		t.Errorf("merges = %v", got)
	}
}

func TestMergeNormalizesCoordinates(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	if err := sh.Merge(4, 6, 2, 3); err != nil {
		t.Fatal(err)
	}
	ws := writeAndOpen(t, wb).sheet(t, 1)
	if got := ws.mergeRefs(); !slices.Equal(got, []string{"B3:D6"}) {
		t.Errorf("merges = %v, want [B3:D6]", got)
	}
	if ws.MergeCells.Count != 1 {
		t.Errorf("count = %d, want 1", ws.MergeCells.Count)
	}
}

func TestStyleDedup(t *testing.T) {
	wb := New()
	a := Style{Font: Font{Bold: true}}
	b := Style{Font: Font{Bold: true}}
	c := Style{Font: Font{Italic: true}}

	if wb.Style(a) != wb.Style(b) {
		t.Error("equal styles got different ids")
	}
	if wb.Style(a) == wb.Style(c) {
		t.Error("different styles got the same id")
	}
	if id := wb.Style(Style{}); id != 0 {
		t.Errorf("default style id = %d, want 0", id)
	}
	// Colour spelling must not create duplicates.
	if wb.Style(Style{Fill: Fill{Color: "4472c4"}}) != wb.Style(Style{Fill: Fill{Color: "FF4472C4"}}) {
		t.Error("equivalent colour spellings got different ids")
	}
}

func TestStyleReservedFills(t *testing.T) {
	wb := New()
	wb.AddSheet("s")
	wb.Style(Style{Fill: Fill{Color: "FF4472C4"}})
	ss := writeAndOpen(t, wb).styles(t)

	if len(ss.Fills.Fill) != 3 {
		t.Fatalf("fills = %d, want 3", len(ss.Fills.Fill))
	}
	if got := ss.Fills.Fill[0].PatternFill.PatternType; got != "none" {
		t.Errorf(`fills[0] patternType = %q, want "none"`, got)
	}
	if got := ss.Fills.Fill[1].PatternFill.PatternType; got != "gray125" {
		t.Errorf(`fills[1] patternType = %q, want "gray125"`, got)
	}
	third := ss.Fills.Fill[2].PatternFill
	if third.PatternType != "solid" || third.FgColor == nil || third.FgColor.RGB != "FF4472C4" {
		t.Errorf("fills[2] = %+v, want a solid FF4472C4 fill", third)
	}
	if third.BgColor == nil || third.BgColor.Indexed != 64 {
		t.Errorf("fills[2] bgColor = %+v, want indexed 64", third.BgColor)
	}
}

func TestStyleReservedSlots(t *testing.T) {
	wb := New()
	wb.AddSheet("s")
	ss := writeAndOpen(t, wb).styles(t)

	if len(ss.Fonts.Font) != 1 || ss.Fonts.Font[0].Name.Val != defaultFontName {
		t.Errorf("fonts = %+v, want a single default font", ss.Fonts.Font)
	}
	if len(ss.Borders.Border) != 1 || ss.Borders.Border[0].Left.Style != "" {
		t.Errorf("borders = %+v, want a single empty border", ss.Borders.Border)
	}
	if len(ss.CellXfs.Xf) != 1 || ss.CellXfs.Xf[0] != (rdXf{}) {
		t.Errorf("cellXfs = %+v, want a single default xf", ss.CellXfs.Xf)
	}
	if ss.CellStyleXfs.Count != 1 || ss.CellStyles.Count != 1 {
		t.Errorf("cellStyleXfs/cellStyles counts = %d/%d, want 1/1",
			ss.CellStyleXfs.Count, ss.CellStyles.Count)
	}
}

func TestStyleNumFmtID(t *testing.T) {
	wb := New()
	wb.AddSheet("s")
	general := wb.Style(Style{Font: Font{Bold: true}})
	first := wb.Style(Style{NumFmt: "0.00"})
	second := wb.Style(Style{NumFmt: `yyyy"年"m"月"d"日"`})
	again := wb.Style(Style{NumFmt: "0.00"})
	if first != again {
		t.Error("the same format code got two ids")
	}

	ss := writeAndOpen(t, wb).styles(t)
	if got := ss.CellXfs.Xf[general].NumFmtID; got != 0 {
		t.Errorf("General numFmtId = %d, want 0", got)
	}
	if got := ss.CellXfs.Xf[first].NumFmtID; got != 164 {
		t.Errorf("first custom numFmtId = %d, want 164", got)
	}
	if got := ss.CellXfs.Xf[second].NumFmtID; got != 165 {
		t.Errorf("second custom numFmtId = %d, want 165", got)
	}
	if len(ss.NumFmts.NumFmt) != 2 || ss.NumFmts.Count != 2 {
		t.Fatalf("numFmts = %+v", ss.NumFmts)
	}
	// The format code must be stored raw. Pre-escaping it would arrive here as
	// literal &quot; text after the encoder escaped the ampersand again.
	if got := ss.NumFmts.NumFmt[1].FormatCode; got != `yyyy"年"m"月"d"日"` {
		t.Errorf("formatCode = %q, want the raw code", got)
	}
}

func TestStyleSheetElementOrder(t *testing.T) {
	wb := New()
	wb.AddSheet("s")
	wb.Style(Style{NumFmt: "0.00", Fill: Fill{Color: "FFFF0000"}, Border: Box(EdgeThin, "FF000000")})
	doc := writeAndOpen(t, wb).part(t, partStyles)
	want := []string{
		"numFmts", "fonts", "fills", "borders",
		"cellStyleXfs", "cellXfs", "cellStyles",
	}
	if got := elementOrder(t, doc); !slices.Equal(got, want) {
		t.Errorf("styleSheet children = %v, want %v", got, want)
	}
}

func TestStyleCounts(t *testing.T) {
	wb := New()
	wb.AddSheet("s")
	wb.Style(Style{NumFmt: "0.00", Font: Font{Bold: true}, Fill: Fill{Color: "FFFF0000"}})
	wb.Style(Style{Border: Box(EdgeThick, "FF00FF00")})
	ss := writeAndOpen(t, wb).styles(t)

	counts := []struct {
		name  string
		count int
		have  int
	}{
		{"numFmts", ss.NumFmts.Count, len(ss.NumFmts.NumFmt)},
		{"fonts", ss.Fonts.Count, len(ss.Fonts.Font)},
		{"fills", ss.Fills.Count, len(ss.Fills.Fill)},
		{"borders", ss.Borders.Count, len(ss.Borders.Border)},
		{"cellXfs", ss.CellXfs.Count, len(ss.CellXfs.Xf)},
	}
	for _, c := range counts {
		if c.count != c.have {
			t.Errorf("%s count attribute = %d, but %d elements", c.name, c.count, c.have)
		}
	}
}

func TestStyleDeterminism(t *testing.T) {
	build := func() string {
		wb := New()
		for _, s := range []Style{
			{Font: Font{Name: "Yu Gothic", Size: 12, Bold: true, Color: "FFFFFFFF"}},
			{Fill: Fill{Color: "FF4472C4"}},
			{Border: Box(EdgeThin, "FF808080")},
			{NumFmt: "0.00"},
			{NumFmt: "#,##0"},
			{Align: Alignment{Horizontal: HAlignCenter, WrapText: true}},
		} {
			wb.Style(s)
		}
		wb.AddSheet("s")
		var buf bytes.Buffer
		if _, err := wb.WriteTo(&buf); err != nil {
			t.Fatal(err)
		}
		return openPkg(t, buf.Bytes()).part(t, partStyles)
	}
	if a, b := build(), build(); a != b {
		t.Errorf("styles.xml differs between runs:\n%s\n%s", a, b)
	}
}

func TestWorksheetElementOrder(t *testing.T) {
	wb := New()
	sh := wb.AddSheet("s")
	sh.SetDefaultRowHeight(15)
	sh.SetColWidth(1, 2, 12)
	sh.SetString(1, 1, "x", 0)
	if err := sh.Merge(1, 1, 2, 1); err != nil {
		t.Fatal(err)
	}
	doc := writeAndOpen(t, wb).part(t, "xl/worksheets/sheet1.xml")
	// CT_Worksheet is an xsd:sequence. cols must precede sheetData and
	// mergeCells must follow it; nothing but a schema validator notices when it
	// does not, so the order is asserted here.
	want := []string{"dimension", "sheetViews", "sheetFormatPr", "cols", "sheetData", "mergeCells"}
	if got := elementOrder(t, doc); !slices.Equal(got, want) {
		t.Errorf("worksheet children = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Every phase 1 feature in one workbook
// ---------------------------------------------------------------------------

// fullStyles are the styles used by the all-features workbook.
type fullStyles struct {
	header  StyleID
	body    StyleID
	stripe  StyleID
	number  StyleID
	wrapped StyleID
}

// buildFullWorkbook writes a workbook exercising every feature the package
// supports: multiple worksheets, string cells, numeric cells, cell styles,
// borders, background colours, fonts, column widths, row heights, merged cells
// and frozen panes.
func buildFullWorkbook() *Workbook {
	wb := New()
	wb.SetProperties(Properties{
		Title:       "jjf sml test",
		Creator:     "jjf",
		Description: "all phase 1 features",
		Created:     time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	st := fullStyles{
		header: wb.Style(Style{
			Font:   Font{Name: "Yu Gothic", Size: 11, Bold: true, Color: "FFFFFFFF"},
			Fill:   Fill{Color: "FF4472C4"},
			Border: Box(EdgeThin, "FF808080"),
			Align:  Alignment{Horizontal: HAlignCenter, Vertical: VAlignCenter},
		}),
		body: wb.Style(Style{
			Font:   Font{Name: "Yu Gothic", Size: 11},
			Border: Box(EdgeThin, "FF808080"),
		}),
		stripe: wb.Style(Style{
			Font:   Font{Name: "Yu Gothic", Size: 11},
			Fill:   Fill{Color: "FFF2F2F2"},
			Border: Box(EdgeThin, "FF808080"),
		}),
		number: wb.Style(Style{
			Font:   Font{Name: "Yu Gothic", Size: 11, Italic: true},
			Border: Border{Bottom: Edge{Style: EdgeDouble, Color: "FF000000"}},
			Align:  Alignment{Horizontal: HAlignRight},
			NumFmt: "#,##0.00",
		}),
		wrapped: wb.Style(Style{
			Font:  Font{Name: "Yu Gothic", Size: 9, Underline: true, Color: "FF808080"},
			Align: Alignment{Vertical: VAlignTop, WrapText: true},
		}),
	}

	// Sheet 1: a title merged across a bordered range, with a frozen header row.
	cover := wb.AddSheet("表紙")
	cover.SetColWidth(1, 1, 6)
	cover.SetColWidth(2, 4, 24)
	cover.SetRowHeight(1, 32)
	cover.SetDefaultRowHeight(15)
	for col := 1; col <= 4; col++ {
		cover.SetBlank(col, 1, st.header)
	}
	cover.SetString(1, 1, "データベース設計書", st.header)
	if err := cover.Merge(1, 1, 4, 1); err != nil {
		panic(err)
	}
	cover.SetString(1, 3, "  padded  ", st.body)
	cover.SetString(2, 3, "line1\nline2", st.wrapped)
	cover.SetString(3, 3, "日本語①㈱髙🙂", st.body)
	cover.SetInt(4, 3, 42, st.body)
	cover.SetFloat(4, 4, 1234.5, st.number)
	cover.Freeze(0, 1)

	// Sheet 2: a striped table with both axes frozen.
	list := wb.AddSheet("テーブル一覧")
	list.SetColWidth(1, 1, 4)
	list.SetColWidth(2, 2, 30)
	list.SetRowHeight(1, 24)
	for col, head := range []string{"No", "物理テーブル名", "論理テーブル名"} {
		list.SetString(col+1, 1, head, st.header)
	}
	for i, name := range []string{"users", "orders", "order_items"} {
		row := i + 2
		style := st.body
		if row%2 == 0 {
			style = st.stripe
		}
		list.SetInt(1, row, i+1, style)
		list.SetString(2, row, name, style)
		list.SetString(3, row, "ユーザー", style)
	}
	list.Freeze(1, 1)

	// Sheet 3: name collisions and column-only freeze.
	dup := wb.AddSheet("USERS")
	dup.SetString(1, 1, "x", st.body)
	dup.Freeze(2, 0)
	return wb
}

func TestFullWorkbookFeatures(t *testing.T) {
	p := writeAndOpen(t, buildFullWorkbook())

	// Feature: multiple worksheets.
	var book rdWorkbook
	p.unmarshal(t, partWorkbook, &book)
	var names []string
	for _, s := range book.Sheets.Sheet {
		names = append(names, s.Name)
	}
	if want := []string{"表紙", "テーブル一覧", "USERS"}; !slices.Equal(names, want) {
		t.Errorf("sheet names = %v, want %v", names, want)
	}

	ss := p.styles(t)
	cover := p.sheet(t, 1)
	cells := cover.cells()

	// Feature: string cells, including text that needs xml:space="preserve".
	for ref, want := range map[string]string{
		"A1": "データベース設計書",
		"A3": "  padded  ",
		"B3": "line1\nline2",
		"C3": "日本語①㈱髙🙂",
	} {
		c, ok := cells[ref]
		if !ok {
			t.Fatalf("cell %s missing", ref)
		}
		if c.T != "inlineStr" {
			t.Errorf("%s type = %q, want inlineStr", ref, c.T)
		}
		if got := c.text(); got != want {
			t.Errorf("%s = %q, want %q", ref, got, want)
		}
	}
	if raw := p.part(t, "xl/worksheets/sheet1.xml"); !strings.Contains(raw, `<t xml:space="preserve">  padded  </t>`) {
		t.Error(`the padded cell is missing xml:space="preserve"`)
	}

	// Feature: numeric cells. They carry no t attribute at all.
	for ref, want := range map[string]string{"D3": "42", "D4": "1234.5"} {
		c := cells[ref]
		if c.T != "" {
			t.Errorf("%s type = %q, want no type attribute", ref, c.T)
		}
		if c.V != want {
			t.Errorf("%s = %q, want %q", ref, c.V, want)
		}
	}

	// Feature: cell styles, fonts, background colours and borders.
	header := ss.CellXfs.Xf[cells["A1"].S]
	font := ss.Fonts.Font[header.FontID]
	if font.Name.Val != "Yu Gothic" || font.Sz.Val != 11 || font.B == nil {
		t.Errorf("header font = %+v, want bold 11pt Yu Gothic", font)
	}
	if font.Color == nil || font.Color.RGB != "FFFFFFFF" {
		t.Errorf("header font colour = %+v, want FFFFFFFF", font.Color)
	}
	fill := ss.Fills.Fill[header.FillID].PatternFill
	if fill.PatternType != "solid" || fill.FgColor == nil || fill.FgColor.RGB != "FF4472C4" {
		t.Errorf("header fill = %+v, want a solid FF4472C4 fill", fill)
	}
	border := ss.Borders.Border[header.BorderID]
	for _, edge := range []rdBorderPr{border.Left, border.Right, border.Top, border.Bottom} {
		if edge.Style != "thin" || edge.Color == nil || edge.Color.RGB != "FF808080" {
			t.Errorf("header border edge = %+v, want thin FF808080", edge)
		}
	}
	if header.Alignment == nil || header.Alignment.Horizontal != "center" ||
		header.Alignment.Vertical != "center" {
		t.Errorf("header alignment = %+v, want centered", header.Alignment)
	}
	wrapped := ss.CellXfs.Xf[cells["B3"].S]
	if wrapped.Alignment == nil || wrapped.Alignment.WrapText != 1 {
		t.Errorf("wrapped alignment = %+v, want wrapText", wrapped.Alignment)
	}
	if u := ss.Fonts.Font[wrapped.FontID].U; u == nil {
		t.Error("wrapped font is not underlined")
	}
	number := ss.CellXfs.Xf[cells["D4"].S]
	if number.NumFmtID < firstCustomNumFmtID {
		t.Errorf("number numFmtId = %d, want >= %d", number.NumFmtID, firstCustomNumFmtID)
	}
	if ss.Borders.Border[number.BorderID].Bottom.Style != "double" {
		t.Errorf("number bottom border = %+v, want double",
			ss.Borders.Border[number.BorderID].Bottom)
	}
	if ss.Fonts.Font[number.FontID].I == nil {
		t.Error("number font is not italic")
	}

	// Feature: column widths.
	wantCols := []struct {
		min, max int
		width    float64
	}{{1, 1, 6}, {2, 4, 24}}
	if len(cover.Cols.Col) != len(wantCols) {
		t.Fatalf("cols = %+v", cover.Cols.Col)
	}
	for i, want := range wantCols {
		got := cover.Cols.Col[i]
		if got.Min != want.min || got.Max != want.max || got.Width != want.width {
			t.Errorf("col[%d] = %+v, want %v", i, got, want)
		}
		if got.CustomWidth != 1 {
			t.Errorf("col[%d] has no customWidth; Excel would ignore the width", i)
		}
	}

	// Feature: row heights.
	var found bool
	for _, r := range cover.SheetData.Row {
		if r.R != 1 {
			continue
		}
		found = true
		if r.Ht != 32 || r.CustomHeight != 1 {
			t.Errorf("row 1 = ht %v customHeight %d, want 32 and 1", r.Ht, r.CustomHeight)
		}
	}
	if !found {
		t.Error("row 1 missing")
	}
	if cover.SheetFormatPr == nil || cover.SheetFormatPr.DefaultRowHeight != 15 {
		t.Errorf("sheetFormatPr = %+v, want defaultRowHeight 15", cover.SheetFormatPr)
	}

	// Feature: merged cells. Every cell of the range carries the border style,
	// because a border on the top left cell alone is not drawn around the range.
	if got := cover.mergeRefs(); !slices.Equal(got, []string{"A1:D1"}) {
		t.Errorf("merges = %v, want [A1:D1]", got)
	}
	for _, ref := range []string{"A1", "B1", "C1", "D1"} {
		if cells[ref].S != cells["A1"].S {
			t.Errorf("%s style = %d, want the merged range style %d", ref, cells[ref].S, cells["A1"].S)
		}
	}

	// Feature: frozen panes, one of each shape.
	for i, want := range []struct {
		x, y          float64
		topLeft, pane string
	}{
		{0, 1, "A2", "bottomLeft"},
		{1, 1, "B2", "bottomRight"},
		{2, 0, "C1", "topRight"},
	} {
		p := p.sheet(t, i+1).SheetViews.SheetView[0].Pane
		if p == nil {
			t.Fatalf("sheet%d has no pane", i+1)
		}
		if p.XSplit != want.x || p.YSplit != want.y || p.TopLeftCell != want.topLeft ||
			p.ActivePane != want.pane {
			t.Errorf("sheet%d pane = %+v, want %+v", i+1, *p, want)
		}
	}

	// Sheet names collide case-insensitively in Excel, so the third sheet must
	// have been renamed.
	if names[2] == "USERS" && strings.EqualFold(names[1], "USERS") {
		t.Error("case-insensitive sheet name collision was not resolved")
	}
}

func TestFullWorkbookSheetNameCollision(t *testing.T) {
	wb := New()
	for _, name := range []string{"users", "USERS", "users", "order/items"} {
		wb.AddSheet(name)
	}
	var book rdWorkbook
	writeAndOpen(t, wb).unmarshal(t, partWorkbook, &book)
	var got []string
	for _, s := range book.Sheets.Sheet {
		got = append(got, s.Name)
	}
	want := []string{"users", "USERS(2)", "users(3)", "order_items"}
	if !slices.Equal(got, want) {
		t.Errorf("sheet names = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Golden files
// ---------------------------------------------------------------------------

// goldenParts are the parts frozen as golden files. The whole .xlsx is never
// compared byte for byte: zip layout is not the thing under test and a binary
// diff is unreadable.
var goldenParts = []string{
	partContentTypes,
	partRootRels,
	partCoreProps,
	partWorkbook,
	partWorkbookRels,
	"xl/worksheets/sheet1.xml",
	"xl/worksheets/sheet2.xml",
	"xl/worksheets/sheet3.xml",
	partStyles,
}

func TestGoldenParts(t *testing.T) {
	p := writeAndOpen(t, buildFullWorkbook())
	for _, name := range goldenParts {
		checkGolden(t, goldenName(name), p.part(t, name))
	}
}

// goldenName maps a part name to a file name usable on every platform.
func goldenName(part string) string {
	return strings.NewReplacer("[", "", "]", "", "/", "_").Replace(part)
}

func checkGolden(t *testing.T, name, have string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(have), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/sml/ -update): %v", err)
	}
	if string(want) != have {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, have, want)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeAndOpen writes wb and reopens it as a package.
func writeAndOpen(t *testing.T, wb *Workbook) *pkg {
	t.Helper()
	return openPkg(t, writeWorkbook(t, wb))
}

func writeWorkbook(t *testing.T, wb *Workbook) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := wb.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteTo reported %d bytes but wrote %d", n, buf.Len())
	}
	return buf.Bytes()
}
