package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
	"github.com/shutx-net/jumping-json-flush/internal/sml"
)

var update = flag.Bool("update", false, "update golden files")

// ---------------------------------------------------------------------------
// Fixtures and rendering helpers
// ---------------------------------------------------------------------------

// loadDoc reads a fixture and decodes it. The fixture is checked against the
// schema on the way through, because a fixture that no longer conforms tests
// nothing worth knowing.
func loadDoc(t *testing.T, name string) *model.Document {
	t.Helper()

	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(path, raw); err != nil {
		t.Fatalf("fixture does not conform to the schema: %v", err)
	}
	doc, err := model.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// render exports doc and returns the raw package bytes.
func render(t *testing.T, doc *model.Document, opt Options) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Export(&buf, doc, opt); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.Bytes()
}

// exportFixture loads a fixture, exports it and reopens the result.
func exportFixture(t *testing.T, name string, opt Options) *pkg {
	t.Helper()
	return openPkg(t, render(t, loadDoc(t, name), opt))
}

// ---------------------------------------------------------------------------
// Cover sheet
// ---------------------------------------------------------------------------

func TestCover(t *testing.T) {
	p := exportFixture(t, "full.json", DefaultOptions())

	if got := p.sheetNames(t)[0]; got != sheetNameCover {
		t.Errorf("first sheet = %q, want %q", got, sheetNameCover)
	}

	ws := p.sheet(t, 1)
	cells := ws.cells()

	// The title spans the sheet, and every cell of the merged range carries the
	// same style so the border is drawn around the whole band.
	wantString(t, cells, "A1", coverTitle)
	for _, r := range []string{"B1", "C1", "D1"} {
		if cells[r].S != cells["A1"].S {
			t.Errorf("%s style = %d, want the merged title style %d", r, cells[r].S, cells["A1"].S)
		}
	}

	wantMerges(t, ws, []string{"A1:D1", "B3:D3", "B4:D4", "B5:D5", "B6:D6", "B7:D7", "B8:D8"})

	for _, tt := range []struct{ labelAt, label, valueAt, value string }{
		{"A3", coverLabelName, "B3", "shop"},
		{"A4", coverLabelLogicalName, "B4", "オンラインショップ"},
		{"A5", coverLabelDBMS, "B5", "PostgreSQL"},
		{"A7", coverLabelFormatVersion, "B7", "1.0"},
		{"A8", coverLabelDescription, "B8", "受注から出荷までを扱う業務データベース。"},
	} {
		wantString(t, cells, tt.labelAt, tt.label)
		wantString(t, cells, tt.valueAt, tt.value)
	}

	// The table count is a number, not text.
	wantString(t, cells, "A6", coverLabelTableCount)
	wantNumber(t, cells, "B6", "3")
}

func TestCoverOmitsGeneratedAtByDefault(t *testing.T) {
	body := exportFixture(t, "full.json", DefaultOptions()).part(t, "xl/worksheets/sheet1.xml")
	if strings.Contains(body, coverLabelGeneratedAt) {
		t.Errorf("the cover names a generation time although Options.GeneratedAt is zero:\n%s", body)
	}
}

func TestCoverGeneratedAt(t *testing.T) {
	at := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	cells := exportFixture(t, "full.json", Options{IncludeCover: true, GeneratedAt: at}).
		sheet(t, 1).cells()

	wantString(t, cells, "A8", coverLabelGeneratedAt)
	wantString(t, cells, "B8", at.Format(generatedAtLayout))
	// The description keeps its place at the end of the list.
	wantString(t, cells, "A9", coverLabelDescription)
}

func TestCoverCanBeDisabled(t *testing.T) {
	p := exportFixture(t, "full.json", Options{})

	names := p.sheetNames(t)
	if slices.Contains(names, sheetNameCover) {
		t.Errorf("sheets = %v, want no %q sheet", names, sheetNameCover)
	}
	if names[0] != sheetNameTableList {
		t.Errorf("first sheet = %q, want %q", names[0], sheetNameTableList)
	}
}

func TestCoverOfDatabaseWithoutOptionalFields(t *testing.T) {
	cells := exportFixture(t, "edge.json", DefaultOptions()).sheet(t, 1).cells()

	wantString(t, cells, "B3", "edges")
	// An absent optional value leaves a blank cell rather than dropping the row.
	for _, ref := range []string{"B4", "B5", "B8"} {
		wantBlank(t, cells, ref)
	}
	wantString(t, cells, "A8", coverLabelDescription)
}

// ---------------------------------------------------------------------------
// Table list sheet
// ---------------------------------------------------------------------------

func TestTableList(t *testing.T) {
	doc := loadDoc(t, "full.json")
	p := openPkg(t, render(t, doc, DefaultOptions()))

	if got := p.sheetNames(t)[1]; got != sheetNameTableList {
		t.Errorf("second sheet = %q, want %q", got, sheetNameTableList)
	}

	ws := p.sheet(t, 2)
	cells := ws.cells()

	for i, heading := range []string{"No", "物理テーブル名", "論理テーブル名", "説明", "カラム数", "シート名"} {
		wantString(t, cells, ref(listColNo+i, listRowHeader), heading)
	}

	// Every table appears once, in document order.
	for i := range doc.Tables {
		row := listRowFirst + i
		tbl := &doc.Tables[i]
		wantNumber(t, cells, ref(listColNo, row), strconv.Itoa(i+1))
		wantString(t, cells, ref(listColName, row), tbl.Name)
		wantString(t, cells, ref(listColLogicalName, row), tbl.LogicalName)
		wantNumber(t, cells, ref(listColColumnCount, row), strconv.Itoa(len(tbl.Columns)))
	}
	if got, want := len(ws.SheetData.Row), len(doc.Tables)+1; got != want {
		t.Errorf("the list has %d rows, want %d", got, want)
	}

	// Alternating rows are striped, which means two distinct styles are in use.
	if cells["B2"].S == cells["B3"].S {
		t.Error("neighbouring body rows share a style, so the list is not striped")
	}
}

func TestTableListFreezesItsHeading(t *testing.T) {
	wantFreeze(t, exportFixture(t, "full.json", DefaultOptions()).sheet(t, 2), "A2", 0, 1)
}

func TestTableListShowsTheAllocatedSheetName(t *testing.T) {
	doc := loadDoc(t, "edge.json")
	p := openPkg(t, render(t, doc, DefaultOptions()))

	// The first two table names are longer than a sheet name may be and are
	// equal up to the limit, so one is truncated and the other numbered.
	names := p.sheetNames(t)[2:]
	cells := p.sheet(t, 2).cells()
	for i := range doc.Tables {
		row := listRowFirst + i
		if names[i] == doc.Tables[i].Name && len(doc.Tables[i].Name) > 31 {
			t.Errorf("sheet %d kept the over-long table name %q", i, names[i])
		}
		wantString(t, cells, ref(listColSheet, row), names[i])
	}
	if names[0] == names[1] {
		t.Errorf("sheets %q and %q collide", names[0], names[1])
	}
}

// ---------------------------------------------------------------------------
// Table definition sheets
// ---------------------------------------------------------------------------

func TestTableDef(t *testing.T) {
	doc := loadDoc(t, "full.json")
	p := openPkg(t, render(t, doc, DefaultOptions()))

	// Sheet 3 is the first table: the cover and the list come before it.
	ws := p.sheet(t, 3)
	cells := ws.cells()
	table := &doc.Tables[0]

	wantString(t, cells, "A1", defTitlePrefix+table.Name+" / "+table.LogicalName)
	wantString(t, cells, "A2", table.Description)
	for i, heading := range []string{
		"No", "物理カラム名", "論理カラム名", "型", "長さ", "NULL", "既定値", "自動採番", "説明",
	} {
		wantString(t, cells, ref(defColNo+i, defRowHeader), heading)
	}

	// Every column appears once, in document order.
	for i := range table.Columns {
		row := defRowFirst + i
		col := &table.Columns[i]
		wantNumber(t, cells, ref(defColNo, row), strconv.Itoa(i+1))
		wantString(t, cells, ref(defColName, row), col.Name)
		wantString(t, cells, ref(defColLogicalName, row), col.LogicalName)
		wantString(t, cells, ref(defColType, row), col.Type)
	}
}

func TestTableDefFreezesTheHeading(t *testing.T) {
	wantFreeze(t, exportFixture(t, "full.json", DefaultOptions()).sheet(t, 3), "A4", 0, 3)
}

func TestTableDefSizeColumn(t *testing.T) {
	cells := exportFixture(t, "full.json", DefaultOptions()).sheet(t, 3).cells()

	// id has neither length nor precision: the cell stays empty rather than
	// printing the zero value of the absent pointer.
	wantBlank(t, cells, ref(defColLength, defRowFirst))
	// email declares a length, credit_limit a precision and a scale.
	wantString(t, cells, ref(defColLength, defRowFirst+1), "255")
	wantString(t, cells, ref(defColLength, defRowFirst+3), "12,2")

	// precision without scale prints the precision alone.
	bare := exportFixture(t, "edge.json", DefaultOptions()).sheet(t, 5).cells()
	wantString(t, bare, ref(defColLength, defRowFirst+2), "8")
}

func TestTableDefDefaultColumn(t *testing.T) {
	cells := exportFixture(t, "full.json", DefaultOptions()).sheet(t, 3).cells()

	// id has no default at all, display_name defaults to the empty string. The
	// two must not look the same.
	wantBlank(t, cells, ref(defColDefault, defRowFirst))
	wantString(t, cells, ref(defColDefault, defRowFirst+2), "")
	wantString(t, cells, ref(defColDefault, defRowFirst+5), "CURRENT_TIMESTAMP")
}

func TestTableDefFlagColumns(t *testing.T) {
	cells := exportFixture(t, "full.json", DefaultOptions()).sheet(t, 3).cells()

	// id: not nullable, auto incremented. email: not nullable, not auto
	// incremented. credit_limit: nullable.
	wantBlank(t, cells, ref(defColNullable, defRowFirst))
	wantString(t, cells, ref(defColAutoIncrement, defRowFirst), markYes)
	wantBlank(t, cells, ref(defColAutoIncrement, defRowFirst+1))
	wantString(t, cells, ref(defColNullable, defRowFirst+3), markYes)
}

func TestTableDefConstraintBlocks(t *testing.T) {
	doc := loadDoc(t, "full.json")
	p := openPkg(t, render(t, doc, DefaultOptions()))

	// customers declares a primary key, a unique key and two indexes, but no
	// foreign key.
	customers := p.part(t, "xl/worksheets/sheet3.xml")
	for _, want := range []string{blockTitlePrimaryKey, blockTitleUniqueKeys, blockTitleIndexes} {
		if !strings.Contains(customers, want) {
			t.Errorf("the customers sheet has no %q block", want)
		}
	}
	if strings.Contains(customers, blockTitleForeignKeys) {
		t.Errorf("the customers sheet has a %q block although it declares none", blockTitleForeignKeys)
	}

	cells := p.sheet(t, 3).cells()
	// One blank row separates the column table from the first block, and each
	// block spends one row on its title and one on its headings.
	pk := defRowFirst + len(doc.Tables[0].Columns) + 1
	wantString(t, cells, ref(defColNo, pk), blockTitlePrimaryKey)
	wantString(t, cells, ref(defColNo, pk+1), labelConstraintName)
	wantString(t, cells, ref(defColLogicalName, pk+1), labelColumns)
	wantString(t, cells, ref(defColNo, pk+2), "pk_customers")
	wantString(t, cells, ref(defColLogicalName, pk+2), "id")

	uq := pk + 3 + 1
	wantString(t, cells, ref(defColNo, uq), blockTitleUniqueKeys)
	wantString(t, cells, ref(defColNo, uq+2), "uq_customers_email")

	ix := uq + 3 + 1
	wantString(t, cells, ref(defColNo, ix), blockTitleIndexes)
	wantString(t, cells, ref(defColLogicalName, ix+1), labelColumns)
	wantString(t, cells, ref(defColLength, ix+1), labelUnique)
	wantString(t, cells, ref(defColLogicalName, ix+3), "display_name, email")
	// Neither index is unique, so the flag cells stay empty.
	wantBlank(t, cells, ref(defColLength, ix+2))
}

func TestTableDefForeignKeyBlock(t *testing.T) {
	// order_lines is the third table, so its sheet is the fifth.
	doc := loadDoc(t, "full.json")
	cells := openPkg(t, render(t, doc, DefaultOptions())).sheet(t, 5).cells()

	// The columns, a blank row, then the primary key and unique key blocks, each
	// three rows tall with a blank row above it, and then the foreign keys.
	fk := defRowFirst + len(doc.Tables[2].Columns) + 1 + 4 + 4
	wantString(t, cells, ref(defColNo, fk), blockTitleForeignKeys)
	for col, label := range map[int]string{
		defColNo:            labelConstraintName,
		defColLogicalName:   labelColumns,
		defColType:          labelRefTable,
		defColLength:        labelRefColumns,
		defColDefault:       labelOnUpdate,
		defColAutoIncrement: labelOnDelete,
	} {
		wantString(t, cells, ref(col, fk+1), label)
	}
	wantString(t, cells, ref(defColNo, fk+2), "fk_order_lines_order")
	wantString(t, cells, ref(defColLogicalName, fk+2), "order_id")
	wantString(t, cells, ref(defColType, fk+2), "orders")
	wantString(t, cells, ref(defColLength, fk+2), "id")
	// The document leaves onUpdate out and sets onDelete.
	wantBlank(t, cells, ref(defColDefault, fk+2))
	wantString(t, cells, ref(defColAutoIncrement, fk+2), "CASCADE")
}

func TestTableDefWithoutConstraints(t *testing.T) {
	// The "bare" table of the edge fixture declares no constraint at all.
	body := exportFixture(t, "edge.json", DefaultOptions()).part(t, "xl/worksheets/sheet5.xml")
	for _, unwanted := range []string{
		blockTitlePrimaryKey, blockTitleUniqueKeys, blockTitleForeignKeys, blockTitleIndexes,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a table without constraints still has a %q block", unwanted)
		}
	}
}

func TestTableDefUnnamedConstraint(t *testing.T) {
	// order_lines has a unique key without a name; the cell is blank rather
	// than an empty string, and its border survives.
	doc := loadDoc(t, "full.json")
	cells := openPkg(t, render(t, doc, DefaultOptions())).sheet(t, 5).cells()
	row := defRowFirst + len(doc.Tables[2].Columns) + 1 + 4 + 2
	wantBlank(t, cells, ref(defColNo, row))
	wantString(t, cells, ref(defColLogicalName, row), "order_id, product_code")
	if cells[ref(defColNo, row)].S == 0 {
		t.Error("the blank constraint name cell has the default style, so it has no border")
	}
}

// ---------------------------------------------------------------------------
// Workbook level behaviour
// ---------------------------------------------------------------------------

func TestExportSheetOrder(t *testing.T) {
	doc := loadDoc(t, "full.json")
	want := []string{sheetNameCover, sheetNameTableList}
	for i := range doc.Tables {
		want = append(want, doc.Tables[i].Name)
	}
	if got := openPkg(t, render(t, doc, DefaultOptions())).sheetNames(t); !slices.Equal(got, want) {
		t.Errorf("sheets = %v, want %v", got, want)
	}
}

func TestExportPackageEntries(t *testing.T) {
	p := exportFixture(t, "full.json", DefaultOptions())
	want := []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"docProps/core.xml",
		"xl/workbook.xml",
		"xl/_rels/workbook.xml.rels",
		"xl/worksheets/sheet1.xml",
		"xl/worksheets/sheet2.xml",
		"xl/worksheets/sheet3.xml",
		"xl/worksheets/sheet4.xml",
		"xl/worksheets/sheet5.xml",
		"xl/styles.xml",
	}
	if !slices.Equal(p.order, want) {
		t.Errorf("package entries =\n%v\nwant\n%v", p.order, want)
	}
}

func TestExportDeterminism(t *testing.T) {
	doc := loadDoc(t, "full.json")
	first := render(t, doc, DefaultOptions())

	if second := render(t, doc, DefaultOptions()); !bytes.Equal(first, second) {
		t.Errorf("two exports of the same document differ: %d vs %d bytes", len(first), len(second))
	}
	// The zip format records local timestamps, so a different time zone is the
	// obvious way for determinism to regress.
	for _, tz := range []string{"Asia/Tokyo", "America/Los_Angeles", "UTC"} {
		t.Run("TZ="+tz, func(t *testing.T) {
			t.Setenv("TZ", tz)
			if got := render(t, doc, DefaultOptions()); !bytes.Equal(first, got) {
				t.Errorf("exporting under TZ=%s produced different bytes", tz)
			}
		})
	}
}

// errWriter fails every write, standing in for a full disk or a closed pipe.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestExportWriteError(t *testing.T) {
	boom := errors.New("boom")
	err := Export(errWriter{err: boom}, loadDoc(t, "full.json"), DefaultOptions())

	if err == nil {
		t.Fatal("Export to a failing writer returned no error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Export error = %v, want it to wrap %v", err, boom)
	}
	if got, want := exitcode.Of(err), exitcode.OutputFailed; got != want {
		t.Errorf("exitcode.Of(err) = %d, want %d", got, want)
	}
}

func TestExportProperties(t *testing.T) {
	doc := loadDoc(t, "full.json")
	body := openPkg(t, render(t, doc, DefaultOptions())).part(t, "docProps/core.xml")

	for _, want := range []string{coverTitle, creator, doc.Database.Description} {
		if !strings.Contains(body, want) {
			t.Errorf("docProps/core.xml does not mention %q:\n%s", want, body)
		}
	}
	// A creation time would make two runs differ.
	if strings.Contains(body, "dcterms:created") {
		t.Errorf("docProps/core.xml records a creation time:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Theme
// ---------------------------------------------------------------------------

// TestThemeResolvesEveryStyle walks the theme by reflection so that a style
// added later cannot silently be left unregistered. Style id 0 is the workbook
// default format, which no deliberate style of this package should be.
func TestThemeResolvesEveryStyle(t *testing.T) {
	v := reflect.ValueOf(*newTheme(sml.New()))
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		switch f := v.Field(i); f.Type() {
		case reflect.TypeOf(sml.StyleID(0)):
			if f.Int() == 0 {
				t.Errorf("theme.%s is the workbook default style", name)
			}
		case reflect.TypeOf(styles{}):
			for j := 0; j < f.NumField(); j++ {
				if f.Field(j).Int() == 0 {
					t.Errorf("theme.%s.%s is the workbook default style", name, f.Type().Field(j).Name)
				}
			}
		default:
			t.Errorf("theme.%s has unexpected type %s", name, f.Type())
		}
	}
}

// TestThemeAppliesThePalette checks the styles that carry the document's colours
// all the way into styles.xml.
func TestThemeAppliesThePalette(t *testing.T) {
	p := exportFixture(t, "full.json", DefaultOptions())
	ss := p.styles(t)
	cells := p.sheet(t, 3).cells()

	heading := ss.CellXfs.Xf[cells[ref(defColNo, defRowHeader)].S]
	font := ss.Fonts.Font[heading.FontID]
	if font.Name.Val != fontName || font.B == nil {
		t.Errorf("heading font = %+v, want bold %s", font, fontName)
	}
	if font.Color == nil || font.Color.RGB != string(colorHeaderFG) {
		t.Errorf("heading font colour = %+v, want %s", font.Color, colorHeaderFG)
	}
	fill := ss.Fills.Fill[heading.FillID].PatternFill
	if fill.PatternType != "solid" || fill.FgColor == nil || fill.FgColor.RGB != string(colorHeaderBG) {
		t.Errorf("heading fill = %+v, want a solid %s fill", fill, colorHeaderBG)
	}
	for _, edge := range ss.Borders.Border[heading.BorderID].edges() {
		if edge.Style != string(sml.EdgeThin) || edge.Color == nil || edge.Color.RGB != string(colorGrid) {
			t.Errorf("heading border edge = %+v, want thin %s", edge, colorGrid)
		}
	}

	// A striped body row differs from a plain one by its fill alone.
	plain := ss.CellXfs.Xf[cells[ref(defColName, defRowFirst)].S]
	striped := ss.CellXfs.Xf[cells[ref(defColName, defRowFirst+1)].S]
	if plain.FillID == striped.FillID {
		t.Error("striped and plain body rows share a fill")
	}
	if got := ss.Fills.Fill[striped.FillID].PatternFill.FgColor; got == nil || got.RGB != string(colorStripe) {
		t.Errorf("stripe fill = %+v, want %s", got, colorStripe)
	}
}

func TestThemeAppliesWidthsAndHeights(t *testing.T) {
	ws := exportFixture(t, "full.json", DefaultOptions()).sheet(t, 3)

	if got, want := len(ws.Cols.Col), defColLast; got != want {
		t.Fatalf("the table definition sheet sets %d column widths, want %d", got, want)
	}
	for i, col := range ws.Cols.Col {
		if col.Min != i+1 || col.Max != i+1 {
			t.Errorf("col[%d] covers %d..%d, want column %d alone", i, col.Min, col.Max, i+1)
		}
		if col.CustomWidth != 1 {
			t.Errorf("col[%d] has no customWidth, so Excel ignores its width", i)
		}
	}
	if got := ws.Cols.Col[defColNo-1].Width; got != widthNo {
		t.Errorf("the No column is %v wide, want %v", got, widthNo)
	}

	for _, want := range []struct {
		row    int
		height float64
	}{{defRowTitle, heightSheetTitle}, {defRowHeader, heightHeader}} {
		var found bool
		for _, r := range ws.SheetData.Row {
			if r.R != want.row {
				continue
			}
			found = true
			if r.Ht != want.height || r.CustomHeight != 1 {
				t.Errorf("row %d = ht %v customHeight %d, want %v and 1",
					r.R, r.Ht, r.CustomHeight, want.height)
			}
		}
		if !found {
			t.Errorf("row %d is missing", want.row)
		}
	}
}

// ---------------------------------------------------------------------------
// Golden files
// ---------------------------------------------------------------------------

// goldenParts are the parts frozen as golden files. The .xlsx is never compared
// byte for byte: the zip layout is not what these tests are about and a binary
// diff cannot be read. styles.xml is left out on purpose so that no colour
// literal lives outside theme.go.
var goldenParts = []string{
	"docProps/core.xml",
	"xl/workbook.xml",
	"xl/worksheets/sheet1.xml",
	"xl/worksheets/sheet2.xml",
	"xl/worksheets/sheet3.xml",
	"xl/worksheets/sheet4.xml",
	"xl/worksheets/sheet5.xml",
}

func TestGoldenParts(t *testing.T) {
	p := exportFixture(t, "full.json", DefaultOptions())
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
		t.Fatalf("read golden (run: go test ./internal/export/xlsx/ -update): %v", err)
	}
	if string(want) != have {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, have, want)
	}
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

func wantString(t *testing.T, cells map[string]rdCell, at, want string) {
	t.Helper()
	c, ok := cells[at]
	if !ok {
		t.Errorf("cell %s is missing, want the string %q", at, want)
		return
	}
	if c.Is == nil {
		t.Errorf("cell %s is not a string cell (t=%q v=%q), want %q", at, c.T, c.V, want)
		return
	}
	if c.Is.T != want {
		t.Errorf("cell %s = %q, want %q", at, c.Is.T, want)
	}
}

func wantNumber(t *testing.T, cells map[string]rdCell, at, want string) {
	t.Helper()
	c, ok := cells[at]
	if !ok {
		t.Errorf("cell %s is missing, want the number %s", at, want)
		return
	}
	if c.T != "" || c.Is != nil {
		t.Errorf("cell %s is not a numeric cell (t=%q), want %s", at, c.T, want)
		return
	}
	if c.V != want {
		t.Errorf("cell %s = %s, want %s", at, c.V, want)
	}
}

// wantBlank asserts that a cell carries a style but no value, which is how an
// absent value is told apart from a value that is present and empty.
func wantBlank(t *testing.T, cells map[string]rdCell, at string) {
	t.Helper()
	c, ok := cells[at]
	if !ok {
		t.Errorf("cell %s is missing, want a blank cell", at)
		return
	}
	if c.Is != nil {
		t.Errorf("cell %s holds the string %q, want a blank cell", at, c.Is.T)
	}
	if c.V != "" {
		t.Errorf("cell %s holds the value %q, want a blank cell", at, c.V)
	}
}

func wantMerges(t *testing.T, ws *rdWorksheet, want []string) {
	t.Helper()
	if got := ws.mergeRefs(); !slices.Equal(got, want) {
		t.Errorf("merges = %v, want %v", got, want)
	}
}

func wantFreeze(t *testing.T, ws *rdWorksheet, topLeft string, x, y float64) {
	t.Helper()
	if len(ws.SheetViews.SheetView) == 0 {
		t.Fatal("the sheet has no sheetView")
	}
	pane := ws.SheetViews.SheetView[0].Pane
	if pane == nil {
		t.Fatalf("the sheet has no frozen pane, want one at %s", topLeft)
	}
	if pane.State != "frozen" || pane.TopLeftCell != topLeft || pane.XSplit != x || pane.YSplit != y {
		t.Errorf("pane = %+v, want frozen at %s with split %v/%v", *pane, topLeft, x, y)
	}
}

// ref returns the A1 reference of a 1-based column and row. The generated sheets
// stay well inside the first 26 columns, so one letter is enough.
func ref(col, row int) string {
	if col < 1 || col > 26 {
		panic(fmt.Sprintf("column %d is outside the range this helper covers", col))
	}
	return string(rune('A'+col-1)) + strconv.Itoa(row)
}

// ---------------------------------------------------------------------------
// A deliberately naive .xlsx reader, used only by these tests. It is built on
// archive/zip and encoding/xml alone so the test suite needs no third party
// dependency, and its structs are separate from the writer's so that a mistake
// in a writer struct tag cannot hide itself.
// ---------------------------------------------------------------------------

type pkg struct {
	files map[string]string
	order []string
}

func openPkg(t *testing.T, b []byte) *pkg {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	p := &pkg{files: make(map[string]string, len(zr.File))}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		p.files[f.Name] = string(data)
		p.order = append(p.order, f.Name)
	}
	return p
}

func (p *pkg) part(t *testing.T, name string) string {
	t.Helper()
	body, ok := p.files[name]
	if !ok {
		t.Fatalf("part %s is missing; the package holds %v", name, p.order)
	}
	return body
}

func (p *pkg) unmarshal(t *testing.T, name string, v any) {
	t.Helper()
	if err := xml.Unmarshal([]byte(p.part(t, name)), v); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
}

// sheetNames lists the worksheet names in workbook order.
func (p *pkg) sheetNames(t *testing.T) []string {
	t.Helper()
	var book struct {
		Sheets struct {
			Sheet []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	p.unmarshal(t, "xl/workbook.xml", &book)
	out := make([]string, 0, len(book.Sheets.Sheet))
	for _, s := range book.Sheets.Sheet {
		out = append(out, s.Name)
	}
	return out
}

// sheet reads the nth worksheet, counting from 1 as the part names do.
func (p *pkg) sheet(t *testing.T, n int) *rdWorksheet {
	t.Helper()
	var ws rdWorksheet
	p.unmarshal(t, fmt.Sprintf("xl/worksheets/sheet%d.xml", n), &ws)
	return &ws
}

func (p *pkg) styles(t *testing.T) *rdStyleSheet {
	t.Helper()
	var ss rdStyleSheet
	p.unmarshal(t, "xl/styles.xml", &ss)
	return &ss
}

type rdWorksheet struct {
	SheetViews struct {
		SheetView []struct {
			Pane *struct {
				XSplit      float64 `xml:"xSplit,attr"`
				YSplit      float64 `xml:"ySplit,attr"`
				TopLeftCell string  `xml:"topLeftCell,attr"`
				State       string  `xml:"state,attr"`
			} `xml:"pane"`
		} `xml:"sheetView"`
	} `xml:"sheetViews"`
	Cols struct {
		Col []struct {
			Min         int     `xml:"min,attr"`
			Max         int     `xml:"max,attr"`
			Width       float64 `xml:"width,attr"`
			CustomWidth int     `xml:"customWidth,attr"`
		} `xml:"col"`
	} `xml:"cols"`
	SheetData struct {
		Row []struct {
			R            int      `xml:"r,attr"`
			Ht           float64  `xml:"ht,attr"`
			CustomHeight int      `xml:"customHeight,attr"`
			C            []rdCell `xml:"c"`
		} `xml:"row"`
	} `xml:"sheetData"`
	MergeCells struct {
		MergeCell []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCell"`
	} `xml:"mergeCells"`
}

type rdCell struct {
	R  string `xml:"r,attr"`
	S  int    `xml:"s,attr"`
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	Is *struct {
		T string `xml:"t"`
	} `xml:"is"`
}

// cells flattens a worksheet into an A1-keyed map.
func (ws *rdWorksheet) cells() map[string]rdCell {
	out := make(map[string]rdCell)
	for _, r := range ws.SheetData.Row {
		for _, c := range r.C {
			out[c.R] = c
		}
	}
	return out
}

func (ws *rdWorksheet) mergeRefs() []string {
	out := make([]string, 0, len(ws.MergeCells.MergeCell))
	for _, m := range ws.MergeCells.MergeCell {
		out = append(out, m.Ref)
	}
	return out
}

type rdStyleSheet struct {
	Fonts struct {
		Font []struct {
			B  *struct{} `xml:"b"`
			Sz struct {
				Val float64 `xml:"val,attr"`
			} `xml:"sz"`
			Color *struct {
				RGB string `xml:"rgb,attr"`
			} `xml:"color"`
			Name struct {
				Val string `xml:"val,attr"`
			} `xml:"name"`
		} `xml:"font"`
	} `xml:"fonts"`
	Fills struct {
		Fill []struct {
			PatternFill struct {
				PatternType string `xml:"patternType,attr"`
				FgColor     *struct {
					RGB string `xml:"rgb,attr"`
				} `xml:"fgColor"`
			} `xml:"patternFill"`
		} `xml:"fill"`
	} `xml:"fills"`
	Borders struct {
		Border []rdBorder `xml:"border"`
	} `xml:"borders"`
	CellXfs struct {
		Xf []struct {
			FontID   int `xml:"fontId,attr"`
			FillID   int `xml:"fillId,attr"`
			BorderID int `xml:"borderId,attr"`
		} `xml:"xf"`
	} `xml:"cellXfs"`
}

type rdBorder struct {
	Left   rdEdge `xml:"left"`
	Right  rdEdge `xml:"right"`
	Top    rdEdge `xml:"top"`
	Bottom rdEdge `xml:"bottom"`
}

type rdEdge struct {
	Style string `xml:"style,attr"`
	Color *struct {
		RGB string `xml:"rgb,attr"`
	} `xml:"color"`
}

func (b rdBorder) edges() []rdEdge { return []rdEdge{b.Left, b.Right, b.Top, b.Bottom} }
