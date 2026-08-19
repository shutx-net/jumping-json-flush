package xlsx

import (
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/sml"
)

// Cover sheet layout. The label column names a field, the value spans the rest.
const (
	coverColLabel    = 1
	coverColValue    = 2
	coverColValueEnd = 4

	coverRowTitle = 1
	coverRowFirst = 3
)

// Cover sheet headings.
const (
	coverTitle = "データベース設計書"

	coverLabelName          = "データベース名"
	coverLabelLogicalName   = "論理名"
	coverLabelDBMS          = "DBMS"
	coverLabelTableCount    = "テーブル数"
	coverLabelFormatVersion = "フォーマットバージョン"
	coverLabelGeneratedAt   = "生成日時"
	coverLabelDescription   = "説明"
)

// generatedAtLayout is how Options.GeneratedAt is spelled on the cover sheet.
const generatedAtLayout = "2006-01-02 15:04:05 MST"

// renderCover draws the cover sheet: the document title, then the identity of
// the database the document describes.
//
// Nothing here reads the clock. A generation timestamp appears only when the
// caller supplies one, so that the default output stays byte-identical across
// runs.
func renderCover(s *sml.Sheet, th *theme, doc *model.Document, opt Options) {
	s.SetColWidth(coverColLabel, coverColLabel, widthCoverLabel)
	s.SetColWidth(coverColValue, coverColValueEnd, widthCoverValue)

	title := span{first: coverColLabel, last: coverColValueEnd}
	s.SetRowHeight(coverRowTitle, heightCoverTitle)
	putText(s, title, coverRowTitle, coverTitle, th.coverTitle)

	value := span{first: coverColValue, last: coverColValueEnd}
	row := coverRowFirst

	// field writes one label and value row of fixed height. An absent optional
	// value leaves an empty cell rather than dropping the row, so that the cover
	// of every document has the same shape.
	field := func(label, v string) {
		s.SetRowHeight(row, heightCoverRow)
		s.SetString(coverColLabel, row, label, th.label)
		putOptional(s, value, row, v, th.text.plain)
		row++
	}

	db := &doc.Database
	field(coverLabelName, db.Name)
	field(coverLabelLogicalName, db.LogicalName)
	field(coverLabelDBMS, string(db.DBMS))

	// The table count is a real number so that it can be referred to in a
	// formula rather than only read.
	s.SetRowHeight(row, heightCoverRow)
	s.SetString(coverColLabel, row, coverLabelTableCount, th.label)
	putInt(s, value, row, len(doc.Tables), th.center.plain)
	row++

	field(coverLabelFormatVersion, doc.FormatVersion)
	if !opt.GeneratedAt.IsZero() {
		field(coverLabelGeneratedAt, opt.GeneratedAt.Format(generatedAtLayout))
	}

	// The description comes last because it is the one field that can run to
	// several lines. Its row is left without a height so the reader fits it.
	s.SetString(coverColLabel, row, coverLabelDescription, th.label)
	putOptional(s, value, row, db.Description, th.wrap.plain)
}
