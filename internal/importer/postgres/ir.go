package postgres

import "strings"

// This file holds the intermediate representation the parser fills: a
// PostgreSQL-shaped picture of one dump, deliberately NOT the jjf model.
//
// Two passes are unavoidable. pg_dump writes every CREATE TABLE first, then the
// ALTER TABLE ADD CONSTRAINT statements, then the indexes, then the comments, so
// a constraint is nearly always seen before nothing at all is known about its
// table. On top of that, whether a column auto-increments can only be decided by
// correlating a DEFAULT nextval(...) with CREATE SEQUENCE and
// ALTER SEQUENCE ... OWNED BY. Resolution therefore lives in build.go, and this
// representation only has to be faithful.
//
// Every slice preserves source order. That, and nothing else, is what makes the
// generated document deterministic: no sorting and no map walks are involved.

// qname is a possibly schema-qualified SQL name, with the line it was written
// on so that any later complaint about it can point at the dump.
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
func (q qname) same(o qname) bool { return q.Schema == o.Schema && q.Name == o.Name }

// pgType is a parsed PostgreSQL type reference.
//
// Words holds the lower-cased words of the type name in order, so that
// "timestamp without time zone" arrives as four words and stays
// distinguishable from "timestamp with time zone". Args holds the text of each
// parenthesised argument, which is a length for character types, a precision
// for date/time types, and precision plus scale for numeric. ArrayDims counts
// the "[]" suffixes.
type pgType struct {
	Words     []string
	Args      []string
	ArrayDims int
	Line      int
}

// String renders the type roughly as the dump wrote it, for diagnostics.
func (t pgType) String() string {
	var b strings.Builder
	b.WriteString(strings.Join(t.Words, " "))
	if len(t.Args) > 0 {
		b.WriteString("(" + strings.Join(t.Args, ",") + ")")
	}
	b.WriteString(strings.Repeat("[]", t.ArrayDims))
	return b.String()
}

// pgColumn is one column of a CREATE TABLE statement.
//
// Default holds the VERBATIM source text of the DEFAULT expression rather than
// a re-rendering of its tokens, because the schema documents a default as
// "written verbatim as a SQL expression" and because nextval('x'::regclass) has
// to survive byte for byte to be recognised later.
type pgColumn struct {
	Name       string
	Line       int
	Type       pgType
	NotNull    bool
	HasDefault bool
	Default    string
	// Identity records GENERATED ... AS IDENTITY, which is the modern spelling
	// of an auto-incrementing column.
	Identity bool
	// Generated records GENERATED ALWAYS AS (expr) STORED, which the design
	// format cannot express; the column is imported as an ordinary one.
	Generated bool
}

// constraintKind is the kind of a table constraint.
type constraintKind uint8

// Constraint kinds recognised by the parser. Check and exclude constraints are
// recognised only so that they can be reported and dropped.
const (
	constraintPrimaryKey constraintKind = iota
	constraintUnique
	constraintForeign
	constraintCheck
	constraintExclude
)

// pgConstraint is a constraint, whether written inline in CREATE TABLE or added
// later by ALTER TABLE.
//
// NameLine is separate from Line so that a constraint whose NAME cannot be
// written to a document is reported at the place the name appears rather than
// at the start of the statement.
type pgConstraint struct {
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

// pgIndex is a CREATE INDEX statement. Method is the access method, defaulting
// to btree when the statement leaves USING out.
type pgIndex struct {
	Name    string
	Table   qname
	Unique  bool
	Columns []string
	Method  string
	Line    int
}

// commentTarget is what a COMMENT ON statement describes. Only the two targets
// the design format has a place for are represented.
type commentTarget uint8

// Comment targets recognised by the parser.
const (
	commentTable commentTarget = iota
	commentColumn
)

// pgComment is one COMMENT ON TABLE or COMMENT ON COLUMN statement.
type pgComment struct {
	Target commentTarget
	Table  qname
	Column string
	Text   string
	Line   int
}

// pgSequence is a sequence and, once ALTER SEQUENCE ... OWNED BY has been seen,
// the column that owns it. It exists for one purpose: deciding which column a
// DEFAULT nextval(...) makes auto-incrementing.
type pgSequence struct {
	Name        qname
	OwnedTable  qname
	OwnedColumn string
	Line        int
}

// pgDefault is a DEFAULT added by ALTER TABLE rather than written inline, which
// is how pg_dump emits the default of a serial column.
type pgDefault struct {
	Table  qname
	Column string
	Expr   string
	Line   int
}

// pgNotNull is a NOT NULL added or dropped by ALTER TABLE. pg_dump writes the
// clause inline, but a concatenated or hand-edited dump may separate it.
type pgNotNull struct {
	Table  qname
	Column string
	Drop   bool
	Line   int
}

// pgIdentity is an identity added by ALTER TABLE ... ADD GENERATED ... AS
// IDENTITY.
type pgIdentity struct {
	Table  qname
	Column string
	Line   int
}

// pgTable is one CREATE TABLE statement with its columns and the constraints
// written inside it.
type pgTable struct {
	Name        qname
	Line        int
	Columns     []pgColumn
	Constraints []pgConstraint
}

// pgDump is everything one dump file said that jjf can use.
//
// Constraints holds only the constraints added by ALTER TABLE; the ones written
// inside a CREATE TABLE stay on their pgTable, because at parse time the table
// is right there and nothing has to be resolved.
type pgDump struct {
	Tables      []pgTable
	Constraints []pgConstraint
	Indexes     []pgIndex
	Comments    []pgComment
	Sequences   []pgSequence
	Defaults    []pgDefault
	NotNulls    []pgNotNull
	Identities  []pgIdentity

	// Database is the name a \connect line named, when the dump carried one.
	// Plain pg_dump does not write one; pg_dumpall does.
	Database string

	// DumpVersion is the version string from the "Dumped by pg_dump version"
	// banner, and DumpVersionLine the line it was on. Both are empty and zero
	// when the dump has no banner.
	DumpVersion     string
	DumpVersionLine int
}

// table returns the parsed table with the given name, or nil. Callers walk the
// slice through this helper instead of keeping a map, so that nothing about the
// output can depend on map iteration order.
func (d *pgDump) table(name qname) *pgTable {
	for i := range d.Tables {
		if d.Tables[i].Name.same(name) {
			return &d.Tables[i]
		}
	}
	return nil
}
