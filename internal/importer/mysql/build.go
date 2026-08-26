package mysql

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// Limits the JSON Schema puts on the fields this file fills. They are repeated
// here because the schema cannot enforce them until the document is written,
// and by then the line of the dump that caused the problem is long gone.
//
// The same four constants as internal/importer/postgres/build.go, with the same
// values, because they come from the same schema. They are copied rather than
// shared for the reason diag.go gives: two independent importers do not grow a
// common dependency, and the schema is the thing they already share.
const (
	maxIdentifierLength  = 128
	maxLogicalNameLength = 255
	maxDescriptionLength = 2000
	maxDefaultLength     = 255
)

// identifierPattern is the schema's identifier pattern, quoted in diagnostics so
// that a reader knows exactly what was expected.
const identifierPattern = "^[A-Za-z_][A-Za-z0-9_]*$"

// ---------------------------------------------------------------------------
// Table index
// ---------------------------------------------------------------------------

// tableIndex holds the tables that were imported.
//
// order is the invariant that matters: every walk whose result can reach the
// output or the diagnostics goes through the SLICE, and the maps exist purely
// for lookup and assignment. A single range over one of these maps would make
// the generated document differ between runs.
//
// The key is a table's BARE name, with any database qualification discarded.
// MySQL's two-part name is database.table rather than schema.table, a design
// document describes exactly one database, and mysqldump writes bare names
// throughout - a qualified name reaches this package only from a hand-written
// or concatenated file. Keying on the bare name is therefore what makes
// "REFERENCES `shop`.`orders`" in such a file resolve against the orders table
// the same file created, which is what its author meant. The one genuinely
// ambiguous case, two tables of one name from a dump of several databases, is
// an error in buildTables rather than a silent merge.
type tableIndex struct {
	order  []string
	byName map[string]*model.Table
	lines  map[string]int
	cols   map[string]map[string]*model.Column
}

// newTableIndex returns an empty index.
func newTableIndex() *tableIndex {
	return &tableIndex{
		byName: map[string]*model.Table{},
		lines:  map[string]int{},
		cols:   map[string]map[string]*model.Column{},
	}
}

// add registers a table and every column it already holds. Nothing may append
// to t.Columns afterwards: the column pointers handed out here point into that
// slice.
func (x *tableIndex) add(t *model.Table, line int) {
	x.order = append(x.order, t.Name)
	x.byName[t.Name] = t
	x.lines[t.Name] = line

	byColumn := make(map[string]*model.Column, len(t.Columns))
	for i := range t.Columns {
		if _, dup := byColumn[t.Columns[i].Name]; dup {
			continue // the first definition wins; see buildTables
		}
		byColumn[t.Columns[i].Name] = &t.Columns[i]
	}
	x.cols[t.Name] = byColumn
}

// table returns the imported table of that name, or nil.
func (x *tableIndex) table(name string) *model.Table { return x.byName[name] }

// column returns the imported column, or nil when either the table or the
// column is unknown.
func (x *tableIndex) column(table, column string) *model.Column { return x.cols[table][column] }

// resolvable reports whether every name in cols is a column of table and no
// name appears twice. Both are schema requirements: a key names columns of its
// own table, and columnNameList is uniqueItems.
func (x *tableIndex) resolvable(table string, cols []string) (string, bool) {
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		if x.column(table, c) == nil {
			return c, false
		}
		if seen[c] {
			return c, false
		}
		seen[c] = true
	}
	return "", true
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

// build resolves the intermediate representation into a design document.
//
// It exists for one reason, and it is a smaller reason than the PostgreSQL
// sibling's: mysqldump writes every column, key, index, foreign key and comment
// inside the CREATE TABLE that owns them, so nothing has to be CORRELATED
// across statements here. What does have to wait is RESOLUTION. mysqldump
// orders tables by name, so a foreign key routinely names a table defined
// further down the file, and a REFERENCES clause without a column list resolves
// against a primary key that may not have been read yet.
//
// The database name is decided FIRST, before a single table is built, because
// the one thing a name qualification can be compared against is the name of the
// database the document will describe.
//
// On failure it returns (nil, err): a half-built document would be worse than
// none, because it would look complete.
func build(dump *myDump, opt Options, d *diagList) (*model.Document, error) {
	database, err := databaseName(dump, opt)
	if err != nil {
		return nil, err
	}

	idx, err := buildTables(dump, database, d)
	if err != nil {
		return nil, err
	}
	if len(idx.order) == 0 {
		return nil, fmt.Errorf("no tables found in the dump")
	}

	applyConstraints(dump, idx, database, d)
	if err := applyIndexes(dump, idx, d); err != nil {
		return nil, err
	}

	doc := &model.Document{
		Schema:        schemadata.DBDesignURL,
		FormatVersion: model.CurrentFormatVersion,
		Database: model.Database{
			Name: database,
			// A dump says nothing about the logical name or the purpose of the
			// database, and inventing either would be worse than leaving an
			// optional field out. MySQL's CREATE DATABASE has no COMMENT
			// clause at all, so there is not even a place one could have been
			// written.
			DBMS: model.DBMSMySQL,
		},
	}
	for _, n := range idx.order {
		doc.Tables = append(doc.Tables, *idx.table(n))
	}
	return doc, nil
}

// buildTables collects the tables and their columns, in source order.
func buildTables(dump *myDump, database string, d *diagList) (*tableIndex, error) {
	idx := newTableIndex()
	for i := range dump.Tables {
		mt := &dump.Tables[i]
		if err := checkIdentifier("table name", mt.Name.Name, mt.Line); err != nil {
			return nil, err
		}
		reportQualification(mt.Name, database, "table "+mt.Name.String(), mt.Line, d)
		if first, dup := idx.lines[mt.Name.Name]; dup {
			// A dump of several databases, taken with --databases, is the way
			// this happens without the file having been edited: each database
			// may hold a table of one name, and a document has room for one of
			// them. Choosing either would describe a schema nobody has.
			return nil, errAt(mt.Line, "table %q is defined twice (first at line %d)", mt.Name.Name, first)
		}
		if len(mt.Columns) == 0 {
			// The schema requires at least one column, so there is nothing to
			// write.
			d.warnf(mt.Line, "table %s has no columns; not imported", mt.Name.Name)
			continue
		}

		t := &model.Table{Name: mt.Name.Name}
		t.LogicalName, t.Description = describeComment(mt.Comment, mt.Name.Name, mt.Name.Name, mt.Line, d)
		for _, mc := range mt.Columns {
			col, err := buildColumn(mt.Name.Name, mc, d)
			if err != nil {
				return nil, err
			}
			t.Columns = append(t.Columns, col)
		}
		idx.add(t, mt.Line)
	}
	return idx, nil
}

// buildColumn turns one parsed column into a document column.
//
// Everything a column can say is said here, where the PostgreSQL sibling has to
// leave the default and the comment to a later pass: MySQL writes the type, the
// nullability, the default, AUTO_INCREMENT and the COMMENT on the column
// itself, so there is nothing left for an ALTER TABLE to override.
//
// A generated column is imported as an ordinary one and is NOT warned about
// again here. The parser already warned, at the line the expression was on,
// which is the line a reader wants; repeating it would put two lines on
// standard error for one fact.
func buildColumn(table string, mc myColumn, d *diagList) (model.Column, error) {
	if err := checkIdentifier("column name", mc.Name, mc.Line); err != nil {
		return model.Column{}, err
	}
	obj := table + "." + mc.Name
	ct, err := normalizeType(mc.Type, obj, d)
	if err != nil {
		return model.Column{}, err
	}
	if mc.OnUpdateCurrentTimestamp {
		// The one column attribute that is neither a type, a default nor a
		// constraint. It has no field in the design format, and it must not be
		// folded into the default: the schema defines that field as a DEFAULT
		// expression and the exporter copies it out verbatim, so a document
		// carrying it would export a script MySQL refuses.
		d.warnf(mc.Line, "%s: ON UPDATE CURRENT_TIMESTAMP is not represented", obj)
	}

	col := model.Column{
		Name:          mc.Name,
		Type:          ct.Name,
		Length:        ct.Length,
		Precision:     ct.Precision,
		Scale:         ct.Scale,
		Nullable:      !mc.NotNull,
		AutoIncrement: mc.AutoIncrement,
	}
	col.LogicalName, col.Description = describeComment(mc.Comment, mc.Name, obj, mc.Line, d)
	if def, ok := columnDefault(mc, obj, d); ok {
		col.Default = &def
	}
	return col, nil
}

// columnDefault decides what a column's DEFAULT clause becomes, and whether it
// becomes anything at all.
//
// A bare DEFAULT NULL on a nullable column is dropped, silently and on purpose.
// MySQL stores NULL as the default of every nullable column that was not given
// one, and mysqldump writes the clause out for each of them - so recording it
// would put a default on very nearly every column of every imported document,
// and each one would state exactly what "nullable": true already states. The
// round trip closes over the omission too: a document with no default produces
// a column with none, MySQL defaults it to NULL again, and the next dump says
// DEFAULT NULL again.
//
// A warning was the alternative and is worse than silence here, for the reason
// skippable gives about SET and LOCK TABLES: a warning on all but a handful of
// columns would bury the warnings that matter.
//
// DEFAULT NULL on a NOT NULL column is a contradiction MySQL will not store, so
// it can only arrive from a hand-written file. It is kept verbatim rather than
// dropped: the document then says what the file said, and "jjf validate" is
// where a document that disagrees with itself is reported.
func columnDefault(mc myColumn, obj string, d *diagList) (string, bool) {
	if !mc.HasDefault {
		return "", false
	}
	expr := normalizeDefault(mc.Default)
	if expr == "" {
		return "", false
	}
	if !mc.NotNull && foldASCII([]byte(expr)) == "null" {
		return "", false
	}
	if utf8.RuneCountInString(expr) > maxDefaultLength {
		// A truncated SQL expression is a wrong SQL expression.
		d.warnf(mc.Line, "%s: default expression is longer than %d characters; not imported", obj, maxDefaultLength)
		return "", false
	}
	return expr, true
}

// importedTables returns the parsed tables that made it into the index, in
// source order. Walking dump.Tables is what keeps that order stable.
func importedTables(dump *myDump, idx *tableIndex) []*myTable {
	out := make([]*myTable, 0, len(idx.order))
	for i := range dump.Tables {
		mt := &dump.Tables[i]
		if idx.table(mt.Name.Name) != nil {
			out = append(out, mt)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Constraints
// ---------------------------------------------------------------------------

// applyConstraints attaches the keys and foreign keys of every imported table.
//
// Keys and unique constraints are resolved before foreign keys because a
// foreign key written as "REFERENCES t" without a column list resolves against
// the primary key of its target, and mysqldump orders tables by name, so
// nothing guarantees that the target's primary key was read earlier.
func applyConstraints(dump *myDump, idx *tableIndex, database string, d *diagList) {
	var all []myConstraint
	for _, mt := range importedTables(dump, idx) {
		all = append(all, mt.Constraints...)
	}
	all = append(all, dump.Constraints...)

	for _, c := range all {
		if c.Kind == constraintPrimaryKey || c.Kind == constraintUnique {
			applyKey(c, idx, d)
		}
	}
	for _, c := range all {
		if c.Kind == constraintForeign {
			applyForeignKey(c, idx, database, d)
		}
	}
}

// constraintName validates an optional constraint name. The schema makes
// primaryKey.name, uniqueKey.name and foreignKey.name OPTIONAL, so a name it
// cannot hold is dropped with a warning; index.name is REQUIRED, which is why
// an index in the same position is an error instead. The asymmetry is the
// schema's, not an oversight here.
func constraintName(kind, name string, line int, d *diagList) string {
	if name == "" || validIdentifier(name) {
		return name
	}
	d.warnf(line, "%s name %q cannot be represented in a jjf document; imported without a name", kind, name)
	return ""
}

// applyKey attaches a primary key or a unique key.
func applyKey(c myConstraint, idx *tableIndex, d *diagList) {
	t := idx.table(c.Table.Name)
	if t == nil {
		return
	}
	kind := "unique key"
	if c.Kind == constraintPrimaryKey {
		kind = "primary key"
	}
	if bad, ok := idx.resolvable(t.Name, c.Columns); !ok {
		d.warnf(c.Line, "table %s: %s names unknown or repeated column %q; not imported", t.Name, kind, bad)
		return
	}
	name := constraintName(kind, c.Name, c.NameLine, d)

	if c.Kind == constraintUnique {
		// No attempt is made to fold a unique key into an equal index or
		// primary key: the dump said both exist, and the document describes the
		// dump. mysqldump does write both - a UNIQUE KEY is an index in MySQL -
		// but it writes exactly one line for it, so the question does not
		// actually arise for a real capture.
		t.UniqueKeys = append(t.UniqueKeys, model.UniqueKey{Name: name, Columns: c.Columns})
		return
	}
	if t.PrimaryKey != nil {
		d.warnf(c.Line, "table %s: a second primary key is not imported", t.Name)
		return
	}
	t.PrimaryKey = &model.PrimaryKey{Name: name, Columns: c.Columns}
	// MySQL makes every primary key column NOT NULL whether or not the column
	// definition said so, and a document that disagreed with that would
	// describe a database nobody can create.
	for _, col := range c.Columns {
		idx.column(t.Name, col).Nullable = false
	}
}

// applyForeignKey attaches a foreign key, resolving its target.
func applyForeignKey(c myConstraint, idx *tableIndex, database string, d *diagList) {
	t := idx.table(c.Table.Name)
	if t == nil {
		return
	}
	label := "table " + t.Name + ": foreign key" + labelOf(c.Name)

	if bad, ok := idx.resolvable(t.Name, c.Columns); !ok {
		d.warnf(c.Line, "%s names unknown or repeated column %q; not imported", label, bad)
		return
	}
	reportQualification(c.RefTable, database, label+" reference", c.Line, d)
	ref := idx.table(c.RefTable.Name)
	if ref == nil {
		// Dropping this silently would hide a real relationship.
		d.warnf(c.Line, "%s references table %s, which was not imported; not imported", label, c.RefTable.Name)
		return
	}

	cols := c.RefColumns
	if len(cols) == 0 {
		// REFERENCES t without a column list means the primary key of t.
		if ref.PrimaryKey == nil {
			d.warnf(c.Line, "%s omits the referenced columns and %s has no primary key; not imported",
				label, ref.Name)
			return
		}
		cols = append([]string(nil), ref.PrimaryKey.Columns...)
	}
	if bad, ok := idx.resolvable(ref.Name, cols); !ok {
		d.warnf(c.Line, "%s references unknown or repeated column %s.%q; not imported", label, ref.Name, bad)
		return
	}
	if len(cols) != len(c.Columns) {
		d.warnf(c.Line, "%s names %d column(s) but references %d; not imported", label, len(c.Columns), len(cols))
		return
	}

	fk := model.ForeignKey{
		Name:       constraintName("foreign key", c.Name, c.NameLine, d),
		Columns:    c.Columns,
		References: model.Reference{Table: ref.Name, Columns: cols},
	}
	if action, ok := referentialAction(c.OnUpdate); ok {
		fk.OnUpdate = action
	}
	if action, ok := referentialAction(c.OnDelete); ok {
		fk.OnDelete = action
	}
	t.ForeignKeys = append(t.ForeignKeys, fk)
}

// referentialAction maps the parser's spelling onto the model constant. An
// action that was never written stays absent rather than being filled in with
// MySQL's NO ACTION default, because the document would then claim the dump
// said something it did not - and mysqldump drops an explicit NO ACTION on the
// way out, so a document that filled it in would not survive its own round
// trip.
func referentialAction(s string) (model.ReferentialAction, bool) {
	switch s {
	case "CASCADE":
		return model.ActionCascade, true
	case "RESTRICT":
		return model.ActionRestrict, true
	case "SET NULL":
		return model.ActionSetNull, true
	case "SET DEFAULT":
		return model.ActionSetDefault, true
	case "NO ACTION":
		return model.ActionNoAction, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Indexes
// ---------------------------------------------------------------------------

// applyIndexes attaches the indexes of every imported table.
//
// It runs after applyConstraints and has to, because of the rule below: whether
// an index is MySQL's own doing is decided by comparing its name against the
// foreign keys the table ended up with.
//
// It is the one attach step that can fail: index.name is REQUIRED by the
// schema, so an index name the schema cannot hold leaves nothing to fall back
// on. A constraint in the same position is only warned about, because its name
// is optional and dropping the name still leaves a usable constraint.
func applyIndexes(dump *myDump, idx *tableIndex, d *diagList) error {
	var all []myIndex
	for _, mt := range importedTables(dump, idx) {
		all = append(all, mt.Indexes...)
	}
	all = append(all, dump.Indexes...)

	for _, ix := range all {
		t := idx.table(ix.Table.Name)
		if t == nil {
			continue
		}
		if ix.Name == "" {
			d.warnf(ix.Line, "table %s: an index without a name cannot be imported", t.Name)
			continue
		}
		if err := checkIdentifier("index name", ix.Name, ix.Line); err != nil {
			return err
		}
		if bad, ok := idx.resolvable(t.Name, ix.Columns); !ok {
			d.warnf(ix.Line, "table %s: index %s names unknown or repeated column %q; not imported",
				t.Name, ix.Name, bad)
			continue
		}
		if backsAForeignKey(t, ix.Name) {
			d.warnf(ix.Line, "table %s: index %s has the name of a foreign key of the same table, which is how MySQL names the index it creates to back one; not imported separately",
				t.Name, ix.Name)
			continue
		}
		t.Indexes = append(t.Indexes, model.Index{Name: ix.Name, Columns: ix.Columns, Unique: ix.Unique})
	}
	return nil
}

// backsAForeignKey reports whether an index of this name is the one InnoDB
// created to carry a foreign key of the same table.
//
// This is the one place the importer declines to record something a dump
// plainly said, and the reason is the design format rather than a preference.
// MySQL keeps index names per TABLE and foreign key names per SCHEMA, so an
// index and a constraint may share a name there; a jjf document keeps
// constraint and index names in ONE namespace per table, which is what
// internal/check's C6b reports on. InnoDB names the index it auto-creates for a
// foreign key after that foreign key, so importing both would produce a
// document jjf itself refuses to export - the exact outcome this importer
// exists to make impossible.
//
// It is NOT the columns that are compared, and D6's rejected alternative -
// suppressing an index whose columns match a foreign key's, to make a round
// trip's first pass look more like its input - stays rejected. An index that
// covers a foreign key's columns under a name of its own is a fact the designer
// stated, it collides with nothing, and it is imported. Only the name collision
// forces a choice, and the foreign key wins it because it carries a referential
// action and a target that no index can express.
//
// Nothing is lost by the drop: the document's foreign key is what MySQL reads
// to create the index again, so a re-export produces the same database. The
// warning is here because a reader has no other way to learn the index was in
// the dump.
func backsAForeignKey(t *model.Table, name string) bool {
	if name == "" {
		return false
	}
	for _, fk := range t.ForeignKeys {
		if fk.Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

// describeComment splits one comment into a logical name and a description,
// falling back to the physical name whenever the comment cannot supply one.
//
// It is the exact inverse of internal/export/ddl/export.go's commentText, which
// both dialects share: the first line is the logical name and the rest is the
// description. That is what closes the round trip, so a change to either is a
// change to both. The PostgreSQL sibling splits a COMMENT ON statement the same
// way; MySQL has no such statement, and the text arrives on the column or on
// the table's COMMENT= option instead, which changes where it comes from and
// nothing about how it is read.
func describeComment(text, physical, obj string, line int, d *diagList) (string, string) {
	name, description := splitComment(text)
	if utf8.RuneCountInString(name) > maxLogicalNameLength {
		// A first line that long is prose, not a name.
		d.warnf(line, "%s: the first line of the comment is longer than %d characters; used as the description",
			obj, maxLogicalNameLength)
		name, description = "", strings.TrimSpace(text)
	}
	if name == "" {
		name = physical
	}
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		// Truncating free text is honest in a way that truncating a name or an
		// expression is not.
		d.warnf(line, "%s: description is longer than %d characters; truncated", obj, maxDescriptionLength)
		description = truncateRunes(description, maxDescriptionLength)
	}
	return name, description
}

// splitComment cuts a comment at its first newline: the first line names the
// object, the rest explains it.
func splitComment(text string) (logicalName, description string) {
	first, rest, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(first), strings.TrimSpace(rest)
}

// truncateRunes cuts s to at most n runes, never in the middle of one.
func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// ---------------------------------------------------------------------------
// Names, defaults and identifiers
// ---------------------------------------------------------------------------

// databaseName decides what the document calls the database: what the caller
// asked for, else what the dump itself said, else the name of the input file.
//
// The middle term is where this differs from the PostgreSQL sibling, and it is
// the one place MySQL gives more rather than less: a mysqldump file names the
// database it was taken from twice, in the header banner and in a USE
// statement, and parse has already decided which of the two wins. A pg_dump of
// one schema carries no database name at all.
func databaseName(dump *myDump, opt Options) (string, error) {
	name := opt.Database
	if name == "" {
		name = dump.Database
	}
	if name == "" && opt.Source != "" {
		base := filepath.Base(opt.Source)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if name == "" {
		return "", fmt.Errorf("cannot tell what the database is called; pass -database <name>")
	}
	if !validIdentifier(name) {
		return "", fmt.Errorf("database name %q cannot be represented in a jjf document: names must match %s and be at most %d characters; pass -database <name>",
			name, identifierPattern, maxIdentifierLength)
	}
	return name, nil
}

// reportQualification warns when a name carries a database qualification that
// is not the database this document describes.
//
// The object is still imported under its bare name, because that is all a
// document has room for. Saying so is what keeps the import honest: a file that
// says `archive`.`orders` and a document that says `orders` are only the same
// statement if the reader is told which database the document is about.
//
// A qualification that matches says nothing and is silent, which is the case
// jjf's own MySQL writer and any hand-qualified single-database dump produce.
func reportQualification(q qname, database, obj string, line int, d *diagList) {
	if q.Schema == "" || q.Schema == database {
		return
	}
	d.warnf(line, "%s: database qualification %q is not represented; a document describes the one database %s",
		obj, q.Schema, database)
}

// normalizeDefault puts a default expression on one line.
//
// Every token is copied verbatim from the source, delimiters included, so a
// string literal keeps the spacing it was written with: the schema says a
// default is written verbatim as a SQL expression, and 'a  b' is not the same
// value as 'a b'. Only the gaps BETWEEN tokens collapse, each to a single
// space where there was any separation and to nothing where there was none, so
// that b'1010' stays b'1010', uuid() stays uuid(), and a default that spanned
// three lines becomes readable in a generated table of columns.
//
// Collapsing the gaps also drops any comment that sat in one, which flattening
// to a single line requires: a surviving "--" or "#" would comment out the rest
// of the expression.
//
// An expression that does not lex is left to the whitespace-only fallback. It
// cannot happen for a default the parser accepted, but normalizeDefault must
// not lose the expression if it ever does.
//
// It is its PostgreSQL sibling with this package's lexer in place of that one,
// which is the whole of the difference: MySQL's backslash escapes and backtick
// identifiers are the lexer's business, not this function's.
func normalizeDefault(expr string) string {
	toks, err := lex([]byte(expr))
	if err != nil {
		return strings.Join(strings.Fields(expr), " ")
	}

	var b strings.Builder
	prevEnd := -1
	for _, t := range toks {
		if t.kind == kindEOF {
			break
		}
		if prevEnd >= 0 && t.pos > prevEnd {
			b.WriteByte(' ')
		}
		b.WriteString(expr[t.pos:t.end])
		prevEnd = t.end
	}
	return b.String()
}

// checkIdentifier refuses a name the design format cannot hold. A silently
// renamed table is worse than a refused import: the document would look right
// and describe a database that does not exist.
//
// MySQL is the reason this is not theoretical. Its own identifier rule allows
// almost any character inside backticks, so `order-items` and `2024_sales` are
// ordinary MySQL table names that a design document has no way to write.
func checkIdentifier(kind, name string, line int) error {
	if validIdentifier(name) {
		return nil
	}
	return errAt(line, "%s %q cannot be represented in a jjf document: names must match %s and be at most %d characters",
		kind, name, identifierPattern, maxIdentifierLength)
}

// validIdentifier implements the schema's identifier rule by hand. The
// character class is short enough that a loop states it more plainly than a
// regexp, and it cannot drift from the pattern quoted in the diagnostics above.
func validIdentifier(name string) bool {
	if name == "" || len(name) > maxIdentifierLength {
		return false
	}
	if !isASCIILetter(name[0]) && name[0] != '_' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if isASCIILetter(c) || isDigit(c) || c == '_' {
			continue
		}
		return false
	}
	return true
}
