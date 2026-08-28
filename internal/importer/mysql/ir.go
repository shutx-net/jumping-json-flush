package mysql

import "strings"

// This file holds the intermediate representation the parser fills: a
// MySQL-shaped picture of one dump, deliberately NOT the jjf model.
//
// A reader arriving from internal/importer/postgres/ir.go will have that file's
// "two passes are unavoidable" paragraph in mind, and only HALF of it applies
// here. Saying which half is the most useful sentence in this package, because
// it is what makes the smaller size below legible as a fact about MySQL rather
// than as an omission.
//
// Correlation does NOT arise. There is no CREATE SEQUENCE to tie to a
// DEFAULT nextval(...) and no ALTER SEQUENCE ... OWNED BY to decide which
// column owns it, because MySQL writes AUTO_INCREMENT on the column itself.
// There is no separately stated DEFAULT, no separately stated NOT NULL, no
// separately added identity and no COMMENT ON statement: mysqldump writes every
// one of those inside the CREATE TABLE that declares the column. So five of the
// PostgreSQL sibling's eight top-level slices - Comments, Sequences, Defaults,
// NotNulls and Identities - have no MySQL counterpart and are not created.
// Keeping them empty for symmetry would be five places a reader has to work out
// that nothing ever fills them.
//
// Forward REFERENCE does arise, and it is why build.go still exists. mysqldump
// orders tables by name and writes foreign keys inline, so a foreign key
// routinely names a table defined further down the file; a parser that built
// the document directly would have to resolve a name it has not read yet.
//
// Every slice preserves source order. That, and nothing else, is what makes the
// generated document deterministic: no sorting and no map walks are involved.

// qname is a possibly qualified SQL name, with the line it was written on so
// that any later complaint about it can point at the dump.
//
// MySQL's two-part name is database.table rather than schema.table, and the
// Schema field carries the database part under the sibling's name so that the
// two importers' diagnostics read alike. mysqldump writes bare names throughout
// a single-database dump, so the field is empty in practice and populated only
// by a hand-written or concatenated file.
type qname struct {
	Schema string
	Name   string
	Line   int
}

// String renders the name the way the dump wrote it. It is used in diagnostics
// only, never to build a document.
func (q qname) String() string {
	if q.Schema == "" {
		return q.Name
	}
	return q.Schema + "." + q.Name
}

// same reports whether two names denote the same object. The line is ignored:
// the same table is named on many different lines of a dump.
//
// The comparison is byte-wise, with no case folding in either part. MySQL's
// lower_case_table_names decides at the SERVER whether two spellings are one
// table, and jjf has no server to ask; it records what the dump wrote.
func (q qname) same(o qname) bool { return q.Schema == o.Schema && q.Name == o.Name }

// myType is a parsed MySQL type reference.
//
// Words holds the lower-cased words of the type name in order, so that
// "double precision" stays distinguishable from "double". Args holds the text
// of each parenthesised argument, which is a length for character types, a
// precision for date/time types, precision plus scale for decimal, and the
// whole value list for ENUM and SET - one shape for all four, exactly as the
// PostgreSQL sibling's pgType.Args does.
//
// Unsigned and Zerofill are attributes rather than words because MySQL writes
// them AFTER the parenthesised arguments: "decimal(10,2) unsigned" is one type,
// and appending them to Words would make the type name depend on where the
// parser happened to stop.
type myType struct {
	Words    []string
	Args     []string
	Unsigned bool
	Zerofill bool
	// Collation is the CHARACTER SET, CHARSET and COLLATE clauses the column
	// carried, verbatim and joined by a space, or "" when it carried none.
	//
	// It records what the dump wrote rather than what it means, exactly as
	// Zerofill does: the design format has nowhere to keep a per-column
	// collation, so the only thing this is for is the warning that says so.
	// It is deliberately absent from String(), which renders the type for the
	// diagnostics of OTHER losses - a message about a dropped type parameter
	// should not grow a collation it is not talking about.
	Collation string
	Line      int
}

// String renders the type roughly as the dump wrote it, for diagnostics.
func (t myType) String() string {
	var b strings.Builder
	b.WriteString(strings.Join(t.Words, " "))
	if len(t.Args) > 0 {
		b.WriteString("(" + strings.Join(t.Args, ",") + ")")
	}
	if t.Unsigned {
		b.WriteString(" unsigned")
	}
	if t.Zerofill {
		b.WriteString(" zerofill")
	}
	return b.String()
}

// myColumn is one column of a CREATE TABLE statement.
//
// Default holds the VERBATIM source text of the DEFAULT expression rather than
// a re-rendering of its tokens, because the schema documents a default as
// "written verbatim as a SQL expression". mysqldump writes DEFAULT (uuid()) and
// DEFAULT b'1010' as well as DEFAULT 'a', and only a slice of the source
// reproduces all three.
type myColumn struct {
	Name       string
	Line       int
	Type       myType
	NotNull    bool
	HasDefault bool
	Default    string
	// AutoIncrement records the MySQL spelling of an auto-incrementing column.
	// It is a column attribute here, where PostgreSQL needs a sequence object
	// and a default that names it.
	AutoIncrement bool
	// Generated records GENERATED ALWAYS AS (expr), which the design format
	// cannot express; the column is imported as an ordinary one.
	Generated bool
	// OnUpdateCurrentTimestamp records the automatic update rule that only
	// MySQL has. It is a flag and never part of Default: the schema defines
	// that field as a DEFAULT expression, the exporter copies it out verbatim,
	// and "CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" in a DEFAULT clause
	// is a script MySQL refuses.
	OnUpdateCurrentTimestamp bool
	// Comment is the column's own COMMENT string. MySQL has no COMMENT ON
	// statement, so it arrives attached to the column and never has to be
	// resolved against an object seen earlier.
	Comment string
}

// constraintKind is the kind of a table constraint.
type constraintKind uint8

// Constraint kinds recognised by the parser. Check constraints are recognised
// only so that they can be reported and dropped.
const (
	constraintPrimaryKey constraintKind = iota
	constraintUnique
	constraintForeign
	constraintCheck
)

// myConstraint is a constraint, whether written inline in CREATE TABLE or added
// later by ALTER TABLE.
//
// NameLine is separate from Line so that a constraint whose NAME cannot be
// written to a document is reported at the place the name appears rather than
// at the start of the statement.
type myConstraint struct {
	Kind       constraintKind
	Name       string
	NameLine   int
	Table      qname
	Columns    []string
	RefTable   qname
	RefColumns []string
	OnUpdate   string
	OnDelete   string
	Line       int
}

// myIndex is a KEY or INDEX element of a CREATE TABLE, an ALTER TABLE ... ADD
// INDEX, or a standalone CREATE INDEX.
//
// There is no Method field, where the PostgreSQL sibling's pgIndex has one.
// MySQL's USING BTREE and USING HASH are a hint the storage engine may ignore
// entirely - InnoDB has only B-trees and accepts USING HASH in silence - so
// recording it would be recording something that need not be true. The one
// engine where it is true, MEMORY, already warns for being a non-InnoDB engine.
type myIndex struct {
	Name    string
	Table   qname
	Unique  bool
	Columns []string
	Line    int
}

// myTable is one CREATE TABLE statement with everything written inside it.
//
// Constraints and Indexes are both here, unlike the PostgreSQL sibling's
// pgTable which has constraints only. mysqldump writes a table's plain KEY
// elements inside the CREATE TABLE, so an index arrives with its table in hand
// and nothing has to be resolved.
type myTable struct {
	Name        qname
	Line        int
	Columns     []myColumn
	Constraints []myConstraint
	Indexes     []myIndex
	// Comment is the table's COMMENT= option.
	Comment string
	// Engine is the ENGINE= option, lower-cased, or empty when the statement
	// left it out. It is kept because a MyISAM table silently ignores the
	// foreign keys a document would claim it has.
	Engine string
}

// myDump is everything one dump file said that jjf can use.
//
// Three statement slices where pgDump has eight. Constraints and Indexes hold
// only what an ALTER TABLE or a standalone CREATE INDEX contributed; everything
// written inside a CREATE TABLE stays on its myTable, because at parse time the
// table is right there.
type myDump struct {
	Tables      []myTable
	Constraints []myConstraint
	Indexes     []myIndex

	// Database is the name the dump connected to: the first USE statement when
	// there is one, and the banner's "Database:" field otherwise.
	Database string

	// ServerVersion is the version string from the "Server version" banner
	// line, and ServerVersionLine the line it was on. Both are empty and zero
	// when the dump has no banner.
	//
	// The SERVER version rather than the mysqldump version, which is the
	// opposite of the PostgreSQL sibling's DumpVersion. mysqldump's own version
	// number has been 10.13 since 2005 and says nothing; the banner's other
	// line is the one that identifies what wrote the file.
	ServerVersion     string
	ServerVersionLine int
}

// table returns the parsed table with the given name, or nil. Callers walk the
// slice through this helper instead of keeping a map, so that nothing about the
// output can depend on map iteration order.
func (d *myDump) table(name qname) *myTable {
	for i := range d.Tables {
		if d.Tables[i].Name.same(name) {
			return &d.Tables[i]
		}
	}
	return nil
}
