// Package xlsx renders a database design document as an Excel workbook.
//
// It owns the layout of the generated document - sheet order, headings, colours,
// widths - and delegates every detail of the file format to internal/sml. The
// output is deterministic: the same document always produces the same bytes,
// because nothing here reads the clock and every loop walks a slice rather than
// a Go map.
package xlsx

import (
	"io"
	"time"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/sml"
)

// Options controls what the generated workbook contains.
type Options struct {
	// IncludeCover emits the cover sheet.
	IncludeCover bool
	// GeneratedAt, when non-zero, is printed on the cover sheet. Leave it zero
	// for byte-identical output across runs.
	GeneratedAt time.Time
}

// DefaultOptions returns the options the jjf command generates with: a cover
// sheet, and no generation timestamp so that repeated runs agree byte for byte.
func DefaultOptions() Options { return Options{IncludeCover: true} }

// creator is recorded as the author of the generated workbook. It is the tool
// name alone: a version would make the output differ between builds.
const creator = "jjf"

// Export renders doc and writes it to dst as an .xlsx package.
//
// The sheets appear in a fixed order: the cover, the list of tables, then one
// sheet per table in document order.
func Export(dst io.Writer, doc *model.Document, opt Options) error {
	wb := sml.New()
	wb.SetProperties(sml.Properties{
		Title:       coverTitle,
		Creator:     creator,
		Description: doc.Database.Description,
	})
	th := newTheme(wb)

	if opt.IncludeCover {
		renderCover(wb.AddSheet(sheetNameCover), th, doc, opt)
	}
	list := wb.AddSheet(sheetNameTableList)

	// Pass 1: reserve a sheet for every table before writing anything, so that
	// the list sheet can print the names that were actually allocated. A name is
	// sanitised, truncated to 31 characters and numbered on collision, none of
	// which is visible until the sheet exists.
	//
	// The effective names are kept in a slice rather than a map keyed by table
	// name: nothing forbids two tables from sharing a name, and a map would then
	// report the wrong sheet for one of them.
	sheets := make([]*sml.Sheet, len(doc.Tables))
	names := make([]string, len(doc.Tables))
	for i := range doc.Tables {
		sheets[i] = wb.AddSheet(doc.Tables[i].Name)
		names[i] = sheets[i].Name()
	}

	// Pass 2: fill in the contents, walking doc.Tables in order.
	renderTableList(list, th, doc, names)
	for i := range doc.Tables {
		renderTableDef(sheets[i], th, &doc.Tables[i])
	}

	if _, err := wb.WriteTo(dst); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "write workbook", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cell placement
// ---------------------------------------------------------------------------

// span is an inclusive range of columns occupied by one field.
type span struct{ first, last int }

// one returns the span of a single column.
func one(col int) span { return span{first: col, last: col} }

// fill styles the cells of the span that its value does not occupy and merges
// the range.
//
// Every cell of a merged range needs the same style: a border set on the top
// left cell alone is not drawn around the range. The ranges come from a fixed
// layout, so an overlap is a programming error rather than a runtime condition,
// which is why it panics here just as sml does for a coordinate out of range.
func (at span) fill(s *sml.Sheet, row int, style sml.StyleID) {
	if at.last <= at.first {
		return
	}
	for col := at.first + 1; col <= at.last; col++ {
		s.SetBlank(col, row, style)
	}
	if err := s.Merge(at.first, row, at.last, row); err != nil {
		panic(err)
	}
}

// putText writes v into the field at row.
func putText(s *sml.Sheet, at span, row int, v string, style sml.StyleID) {
	s.SetString(at.first, row, v, style)
	at.fill(s, row, style)
}

// putOptional writes v, or leaves a blank but still bordered cell when v is
// empty. It is the right choice wherever an absent value must not be mistaken
// for a value that is present and empty.
func putOptional(s *sml.Sheet, at span, row int, v string, style sml.StyleID) {
	if v == "" {
		s.SetBlank(at.first, row, style)
	} else {
		s.SetString(at.first, row, v, style)
	}
	at.fill(s, row, style)
}

// putInt writes n into the field at row.
func putInt(s *sml.Sheet, at span, row, n int, style sml.StyleID) {
	s.SetInt(at.first, row, n, style)
	at.fill(s, row, style)
}
