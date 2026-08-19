package xlsx

import (
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/sml"
)

// Table list layout. The heading occupies the first row and the tables follow
// one per row.
const (
	listColNo          = 1
	listColName        = 2
	listColLogicalName = 3
	listColDescription = 4
	listColColumnCount = 5
	listColSheet       = 6

	listRowHeader = 1
	listRowFirst  = 2
)

// renderTableList draws the index of every table in the document.
//
// names holds the sheet name that was actually allocated for each table, in the
// same order as doc.Tables.
func renderTableList(s *sml.Sheet, th *theme, doc *model.Document, names []string) {
	s.SetColWidth(listColNo, listColNo, widthNo)
	s.SetColWidth(listColName, listColName, widthPhysName)
	s.SetColWidth(listColLogicalName, listColLogicalName, widthLogicalName)
	s.SetColWidth(listColDescription, listColDescription, widthDescription)
	s.SetColWidth(listColColumnCount, listColColumnCount, widthCount)
	s.SetColWidth(listColSheet, listColSheet, widthSheetName)

	s.SetRowHeight(listRowHeader, heightHeader)
	for i, heading := range [...]string{
		"No", "物理テーブル名", "論理テーブル名", "説明", "カラム数", "シート名",
	} {
		s.SetString(listColNo+i, listRowHeader, heading, th.tableHdr)
	}
	// Keep the heading in view while scrolling a long list.
	s.Freeze(0, listRowHeader)

	for i := range doc.Tables {
		t := &doc.Tables[i]
		row := listRowFirst + i
		striped := row%2 == 0
		text, center := th.text.pick(striped), th.center.pick(striped)

		s.SetInt(listColNo, row, i+1, center)
		s.SetString(listColName, row, t.Name, text)
		s.SetString(listColLogicalName, row, t.LogicalName, text)
		putOptional(s, one(listColDescription), row, t.Description, th.wrap.pick(striped))
		s.SetInt(listColColumnCount, row, len(t.Columns), center)
		// The allocated sheet name rather than the table name: it is what the
		// reader has to click on, and it shows when a long or duplicated name was
		// truncated or numbered.
		s.SetString(listColSheet, row, names[i], text)
	}
}
