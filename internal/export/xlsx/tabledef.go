package xlsx

import (
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/sml"
)

// Table definition layout, one sheet per table: a title band, the table's own
// description, the column definitions, then one block per kind of constraint.
const (
	defColNo            = 1
	defColName          = 2
	defColLogicalName   = 3
	defColType          = 4
	defColLength        = 5
	defColNullable      = 6
	defColDefault       = 7
	defColAutoIncrement = 8
	defColDescription   = 9
	defColLast          = defColDescription

	defRowTitle       = 1
	defRowDescription = 2
	defRowHeader      = 3
	defRowFirst       = 4
)

// Table definition headings.
const (
	defTitlePrefix = "テーブル定義: "

	blockTitlePrimaryKey  = "主キー (PRIMARY KEY)"
	blockTitleUniqueKeys  = "ユニークキー (UNIQUE)"
	blockTitleForeignKeys = "外部キー (FOREIGN KEY)"
	blockTitleIndexes     = "インデックス (INDEX)"

	labelConstraintName = "制約名"
	labelIndexName      = "インデックス名"
	labelColumns        = "対象カラム"
	labelRefTable       = "参照先テーブル"
	labelRefColumns     = "参照先カラム"
	labelOnUpdate       = "ON UPDATE"
	labelOnDelete       = "ON DELETE"
	labelUnique         = "ユニーク"
)

// markYes is how a boolean column says yes. Saying no is left to an empty cell,
// which is the convention Japanese design documents follow.
const markYes = "○"

// renderTableDef draws the definition of one table.
func renderTableDef(s *sml.Sheet, th *theme, t *model.Table) {
	s.SetColWidth(defColNo, defColNo, widthNo)
	s.SetColWidth(defColName, defColName, widthPhysName)
	s.SetColWidth(defColLogicalName, defColLogicalName, widthLogicalName)
	s.SetColWidth(defColType, defColType, widthType)
	s.SetColWidth(defColLength, defColLength, widthLength)
	s.SetColWidth(defColNullable, defColNullable, widthFlag)
	s.SetColWidth(defColDefault, defColDefault, widthDefault)
	s.SetColWidth(defColAutoIncrement, defColAutoIncrement, widthFlag)
	s.SetColWidth(defColDescription, defColDescription, widthDescription)

	full := span{first: defColNo, last: defColLast}
	s.SetRowHeight(defRowTitle, heightSheetTitle)
	putText(s, full, defRowTitle, defTitlePrefix+t.Name+" / "+t.LogicalName, th.sheetTitle)
	if t.Description != "" {
		putText(s, full, defRowDescription, t.Description, th.caption)
	}

	s.SetRowHeight(defRowHeader, heightHeader)
	for i, heading := range [...]string{
		"No", "物理カラム名", "論理カラム名", "型", "長さ",
		"NULL", "既定値", "自動採番", "説明",
	} {
		s.SetString(defColNo+i, defRowHeader, heading, th.tableHdr)
	}
	// Keep the title and the heading row in view while scrolling a long table.
	s.Freeze(0, defRowHeader)

	for i := range t.Columns {
		renderColumn(s, th, &t.Columns[i], defRowFirst+i, i+1)
	}

	row := defRowFirst + len(t.Columns)
	for _, b := range blocksOf(t) {
		row = b.render(s, th, row+1) // one blank row above every block
	}
}

// renderColumn writes one column definition as row number no.
func renderColumn(s *sml.Sheet, th *theme, c *model.Column, row, no int) {
	striped := row%2 == 1
	text, center := th.text.pick(striped), th.center.pick(striped)

	s.SetInt(defColNo, row, no, center)
	s.SetString(defColName, row, c.Name, text)
	s.SetString(defColLogicalName, row, c.LogicalName, text)
	s.SetString(defColType, row, c.Type, text)
	putOptional(s, one(defColLength), row, sizeOf(c), center)
	putOptional(s, one(defColNullable), row, mark(c.Nullable), center)
	// A default of "" is a DEFAULT clause with nothing in it - which is a
	// mistake, and one internal/check reports - while no default at all is no
	// DEFAULT clause. The two are therefore written as different kinds of cell,
	// an empty string and a blank cell: rendering both as blank would hide the
	// mistake in the very artifact the author reads to find it.
	if c.Default != nil {
		s.SetString(defColDefault, row, *c.Default, text)
	} else {
		s.SetBlank(defColDefault, row, text)
	}
	putOptional(s, one(defColAutoIncrement), row, mark(c.AutoIncrement), center)
	putOptional(s, one(defColDescription), row, c.Description, th.wrap.pick(striped))
}

// sizeOf renders the declared size of a column: a length, or a precision with
// an optional scale. All three are pointers, so an absent size stays an empty
// cell instead of printing a zero.
func sizeOf(c *model.Column) string {
	switch {
	case c.Length != nil:
		return strconv.Itoa(*c.Length)
	case c.Precision != nil && c.Scale != nil:
		return strconv.Itoa(*c.Precision) + "," + strconv.Itoa(*c.Scale)
	case c.Precision != nil:
		return strconv.Itoa(*c.Precision)
	default:
		return ""
	}
}

// mark renders a boolean column value.
func mark(on bool) string {
	if on {
		return markYes
	}
	return ""
}

// joinColumns renders a list of column names as one cell.
func joinColumns(names []string) string { return strings.Join(names, ", ") }

// ---------------------------------------------------------------------------
// Constraint blocks
// ---------------------------------------------------------------------------

// field is one column of a constraint block: the cells it occupies and its
// heading.
type field struct {
	at    span
	label string
}

// block is a constraint table drawn below the column definitions.
type block struct {
	title  string
	fields []field
	rows   [][]string // one entry per row, one string per field
}

// render draws b with its title at row and returns the first free row below it.
func (b block) render(s *sml.Sheet, th *theme, row int) int {
	putText(s, span{first: b.fields[0].at.first, last: b.fields[len(b.fields)-1].at.last},
		row, b.title, th.sectionHdr)
	row++
	for _, f := range b.fields {
		putText(s, f.at, row, f.label, th.tableHdr)
	}
	row++
	for _, values := range b.rows {
		for i, f := range b.fields {
			// A constraint name is optional in the document, so an unnamed one
			// leaves a blank cell rather than claiming an empty name.
			putOptional(s, f.at, row, values[i], th.text.plain)
		}
		row++
	}
	return row
}

// layoutFields lays labels out over consecutive columns starting at the first
// one of the sheet, giving each label the number of columns listed in spans.
func layoutFields(labels []string, spans ...int) []field {
	out := make([]field, 0, len(labels))
	col := defColNo
	for i, label := range labels {
		out = append(out, field{at: span{first: col, last: col + spans[i] - 1}, label: label})
		col += spans[i]
	}
	return out
}

// blocksOf lists the constraint blocks of t in a fixed order. A constraint the
// document does not define produces no block at all.
//
// Each block is only as wide as its own fields need, which is why the column
// counts differ from block to block.
func blocksOf(t *model.Table) []block {
	var out []block

	keyFields := func(name string) []field {
		return layoutFields([]string{name, labelColumns}, 2, 2)
	}

	if pk := t.PrimaryKey; pk != nil {
		out = append(out, block{
			title:  blockTitlePrimaryKey,
			fields: keyFields(labelConstraintName),
			rows:   [][]string{{pk.Name, joinColumns(pk.Columns)}},
		})
	}

	if len(t.UniqueKeys) > 0 {
		rows := make([][]string, 0, len(t.UniqueKeys))
		for _, k := range t.UniqueKeys {
			rows = append(rows, []string{k.Name, joinColumns(k.Columns)})
		}
		out = append(out, block{
			title:  blockTitleUniqueKeys,
			fields: keyFields(labelConstraintName),
			rows:   rows,
		})
	}

	if len(t.ForeignKeys) > 0 {
		rows := make([][]string, 0, len(t.ForeignKeys))
		for _, k := range t.ForeignKeys {
			rows = append(rows, []string{
				k.Name,
				joinColumns(k.Columns),
				k.References.Table,
				joinColumns(k.References.Columns),
				string(k.OnUpdate),
				string(k.OnDelete),
			})
		}
		out = append(out, block{
			title: blockTitleForeignKeys,
			fields: layoutFields([]string{
				labelConstraintName, labelColumns, labelRefTable,
				labelRefColumns, labelOnUpdate, labelOnDelete,
			}, 2, 1, 1, 2, 1, 1),
			rows: rows,
		})
	}

	if len(t.Indexes) > 0 {
		rows := make([][]string, 0, len(t.Indexes))
		for _, ix := range t.Indexes {
			rows = append(rows, []string{ix.Name, joinColumns(ix.Columns), mark(ix.Unique)})
		}
		out = append(out, block{
			title:  blockTitleIndexes,
			fields: layoutFields([]string{labelIndexName, labelColumns, labelUnique}, 2, 2, 2),
			rows:   rows,
		})
	}

	return out
}
