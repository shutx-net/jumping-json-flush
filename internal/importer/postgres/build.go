package postgres

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

// tableIndex holds the tables that survived the schema filter.
//
// order is the invariant that matters: every walk whose result can reach the
// output or the diagnostics goes through the SLICE, and the maps exist purely
// for lookup. A single range over one of these maps would make the generated
// document differ between runs.
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
			continue // the first definition wins; see buildTable
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
// This is what the two-phase design exists for. pg_dump writes every table
// first and its constraints, indexes and comments afterwards, so nothing below
// could have been decided while parsing.
//
// On failure it returns (nil, err): a half-built document would be worse than
// none, because it would look complete.
func build(dump *pgDump, opt Options, d *diagList) (*model.Document, error) {
	target := opt.Schema
	if target == "" {
		target = DefaultSchema
	}

	idx, err := buildTables(dump, target, d)
	if err != nil {
		return nil, err
	}
	if len(idx.order) == 0 {
		return nil, fmt.Errorf("no tables found in schema %q", target)
	}

	applyNotNulls(dump, idx, target, d)
	applyDefaults(dump, idx, target, d)
	applyIdentities(dump, idx, target, d)
	applyConstraints(dump, idx, target, d)
	if err := applyIndexes(dump, idx, target, d); err != nil {
		return nil, err
	}
	applyComments(dump, idx, target, d)

	name, err := databaseName(dump, opt)
	if err != nil {
		return nil, err
	}
	doc := &model.Document{
		Schema:        schemadata.DBDesignURL,
		FormatVersion: model.CurrentFormatVersion,
		Database: model.Database{
			Name: name,
			// A dump says nothing about the logical name or the purpose of the
			// database, and inventing either would be worse than leaving an
			// optional field out.
			DBMS: model.DBMSPostgreSQL,
		},
	}
	for _, n := range idx.order {
		doc.Tables = append(doc.Tables, *idx.table(n))
	}
	return doc, nil
}

// buildTables collects the tables and columns of the target schema, in source
// order.
func buildTables(dump *pgDump, target string, d *diagList) (*tableIndex, error) {
	idx := newTableIndex()
	for i := range dump.Tables {
		pt := &dump.Tables[i]
		if !inTargetSchema(pt.Name, target) {
			// Out of scope, not a problem: nothing was asked of this table.
			continue
		}
		if err := checkIdentifier("table name", pt.Name.Name, pt.Line); err != nil {
			return nil, err
		}
		if first, dup := idx.lines[pt.Name.Name]; dup {
			return nil, errAt(pt.Line, "table %q is defined twice (first at line %d)", pt.Name.Name, first)
		}
		if len(pt.Columns) == 0 {
			// The schema requires at least one column, so there is nothing to
			// write.
			d.warnf(pt.Line, "table %s has no columns; not imported", pt.Name.Name)
			continue
		}

		t := &model.Table{Name: pt.Name.Name, LogicalName: pt.Name.Name}
		for _, pc := range pt.Columns {
			col, err := buildColumn(pt.Name.Name, pc, d)
			if err != nil {
				return nil, err
			}
			t.Columns = append(t.Columns, col)
		}
		idx.add(t, pt.Line)
	}
	return idx, nil
}

// buildColumn turns one parsed column into a document column. The default is
// deliberately left out: it is resolved later, once the ALTER TABLE statements
// that may override it have been seen.
func buildColumn(table string, pc pgColumn, d *diagList) (model.Column, error) {
	if err := checkIdentifier("column name", pc.Name, pc.Line); err != nil {
		return model.Column{}, err
	}
	ct, err := normalizeType(pc.Type, table+"."+pc.Name, d)
	if err != nil {
		return model.Column{}, err
	}
	return model.Column{
		Name:        pc.Name,
		LogicalName: pc.Name, // replaced by a COMMENT ON, if the dump has one
		Type:        ct.Name,
		Length:      ct.Length,
		Precision:   ct.Precision,
		Scale:       ct.Scale,
		Nullable:    !pc.NotNull,
		// A serial pseudo-type and an inline IDENTITY both say so outright; the
		// other two sources are resolved further down.
		AutoIncrement: ct.AutoIncrement || pc.Identity,
	}, nil
}

// importedTables returns the parsed tables that made it into the index, in
// source order. Walking dump.Tables is what keeps that order stable.
func importedTables(dump *pgDump, idx *tableIndex, target string) []*pgTable {
	out := make([]*pgTable, 0, len(idx.order))
	for i := range dump.Tables {
		pt := &dump.Tables[i]
		if inTargetSchema(pt.Name, target) && idx.table(pt.Name.Name) != nil {
			out = append(out, pt)
		}
	}
	return out
}

// applyNotNulls honours a NOT NULL that a dump stated in its own statement
// rather than inline.
func applyNotNulls(dump *pgDump, idx *tableIndex, target string, d *diagList) {
	for _, nn := range dump.NotNulls {
		if !inTargetSchema(nn.Table, target) {
			continue
		}
		col := idx.column(nn.Table.Name, nn.Column)
		if col == nil {
			d.warnf(nn.Line, "%s.%s: NOT NULL names a column that was not imported", nn.Table.Name, nn.Column)
			continue
		}
		col.Nullable = nn.Drop
	}
}

// applyDefaults resolves every column default, inline or added later, and
// decides which of them really mean "auto increment".
//
// The inline defaults come first and the ALTER TABLE ones after, so that the
// statement written last wins - which is also the order PostgreSQL applies them
// in.
func applyDefaults(dump *pgDump, idx *tableIndex, target string, d *diagList) {
	var defaults []pgDefault
	for _, pt := range importedTables(dump, idx, target) {
		for _, pc := range pt.Columns {
			if pc.HasDefault {
				defaults = append(defaults, pgDefault{
					Table: pt.Name, Column: pc.Name, Expr: pc.Default, Line: pc.Line,
				})
			}
		}
	}
	defaults = append(defaults, dump.Defaults...)

	// The map is a lookup only; the loop below that reaches the document walks
	// the index's order slice.
	type record struct {
		expr string
		line int
	}
	pending := map[*model.Column]record{}
	for _, def := range defaults {
		if !inTargetSchema(def.Table, target) {
			continue
		}
		col := idx.column(def.Table.Name, def.Column)
		if col == nil {
			d.warnf(def.Line, "%s.%s: DEFAULT names a column that was not imported", def.Table.Name, def.Column)
			continue
		}
		pending[col] = record{expr: def.Expr, line: def.Line}
	}

	for _, name := range idx.order {
		t := idx.table(name)
		for i := range t.Columns {
			col := &t.Columns[i]
			rec, ok := pending[col]
			if !ok {
				continue
			}
			obj := name + "." + col.Name
			expr := normalizeDefault(rec.expr)
			if expr == "" {
				continue
			}
			if seq, isNextval := sequenceFromNextval(expr); isNextval {
				// autoIncrement already says everything the nextval() said, and
				// keeping both would state the same fact twice.
				col.AutoIncrement = true
				checkSequenceOwner(dump, seq, name, col.Name, rec.line, d)
				continue
			}
			if utf8.RuneCountInString(expr) > maxDefaultLength {
				// A truncated SQL expression is a wrong SQL expression.
				d.warnf(rec.line, "%s: default expression is longer than %d characters; not imported",
					obj, maxDefaultLength)
				continue
			}
			col.Default = &expr
		}
	}
}

// checkSequenceOwner reports a nextval() default whose sequence belongs to some
// other column. Recording the sequences is what makes that check possible; it
// is the only thing they are kept for.
func checkSequenceOwner(dump *pgDump, sequence, table, column string, line int, d *diagList) {
	seq := findSequence(dump, sequence)
	if seq == nil || seq.OwnedColumn == "" {
		return
	}
	if seq.OwnedTable.Name == table && seq.OwnedColumn == column {
		return
	}
	d.warnf(line, "%s.%s: sequence %s is owned by %s.%s; imported as auto increment anyway",
		table, column, sequence, seq.OwnedTable.Name, seq.OwnedColumn)
}

// findSequence looks a sequence up by name, ignoring a schema qualification that
// only one of the two spellings carries.
func findSequence(dump *pgDump, name string) *pgSequence {
	schema, bare, qualified := strings.Cut(name, ".")
	if !qualified {
		bare, schema = name, ""
	}
	for i := range dump.Sequences {
		s := &dump.Sequences[i]
		if s.Name.Name != bare {
			continue
		}
		if schema != "" && s.Name.Schema != "" && s.Name.Schema != schema {
			continue
		}
		return s
	}
	return nil
}

// applyIdentities honours ALTER TABLE ... ADD GENERATED ... AS IDENTITY.
func applyIdentities(dump *pgDump, idx *tableIndex, target string, d *diagList) {
	for _, id := range dump.Identities {
		if !inTargetSchema(id.Table, target) {
			continue
		}
		col := idx.column(id.Table.Name, id.Column)
		if col == nil {
			d.warnf(id.Line, "%s.%s: IDENTITY names a column that was not imported", id.Table.Name, id.Column)
			continue
		}
		col.AutoIncrement = true
	}
}

// ---------------------------------------------------------------------------
// Constraints
// ---------------------------------------------------------------------------

// applyConstraints attaches the keys of every imported table.
//
// Keys and unique constraints are resolved before foreign keys because a
// foreign key written as "REFERENCES t" without a column list resolves against
// the primary key of its target, and nothing guarantees that the target's
// primary key was declared earlier in the file.
func applyConstraints(dump *pgDump, idx *tableIndex, target string, d *diagList) {
	var all []pgConstraint
	for _, pt := range importedTables(dump, idx, target) {
		all = append(all, pt.Constraints...)
	}
	all = append(all, dump.Constraints...)

	for _, c := range all {
		if c.Kind == constraintPrimaryKey || c.Kind == constraintUnique {
			applyKey(c, idx, target, d)
		}
	}
	for _, c := range all {
		if c.Kind == constraintForeign {
			applyForeignKey(c, idx, target, d)
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
func applyKey(c pgConstraint, idx *tableIndex, target string, d *diagList) {
	if !inTargetSchema(c.Table, target) {
		return
	}
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
		// dump.
		t.UniqueKeys = append(t.UniqueKeys, model.UniqueKey{Name: name, Columns: c.Columns})
		return
	}
	if t.PrimaryKey != nil {
		d.warnf(c.Line, "table %s: a second primary key is not imported", t.Name)
		return
	}
	t.PrimaryKey = &model.PrimaryKey{Name: name, Columns: c.Columns}
	// PostgreSQL makes every primary key column NOT NULL whether or not the
	// column definition said so.
	for _, col := range c.Columns {
		idx.column(t.Name, col).Nullable = false
	}
}

// applyForeignKey attaches a foreign key, resolving its target.
func applyForeignKey(c pgConstraint, idx *tableIndex, target string, d *diagList) {
	if !inTargetSchema(c.Table, target) {
		return
	}
	t := idx.table(c.Table.Name)
	if t == nil {
		return
	}
	label := "table " + t.Name + ": foreign key" + labelOf(c.Name)

	if bad, ok := idx.resolvable(t.Name, c.Columns); !ok {
		d.warnf(c.Line, "%s names unknown or repeated column %q; not imported", label, bad)
		return
	}
	if !inTargetSchema(c.RefTable, target) {
		// Dropping this silently would hide a real relationship.
		d.warnf(c.Line, "%s references %s outside schema %s; not imported", label, c.RefTable, target)
		return
	}
	ref := idx.table(c.RefTable.Name)
	if ref == nil {
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
// PostgreSQL's NO ACTION default, because the document would then claim the
// dump said something it did not.
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

// applyIndexes attaches the indexes of every imported table.
//
// It is the one attach step that can fail: index.name is REQUIRED by the
// schema, so an index name the schema cannot hold leaves nothing to fall back
// on. A constraint in the same position is only warned about, because its name
// is optional and dropping the name still leaves a usable constraint.
func applyIndexes(dump *pgDump, idx *tableIndex, target string, d *diagList) error {
	for _, ix := range dump.Indexes {
		if !inTargetSchema(ix.Table, target) {
			continue
		}
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
		t.Indexes = append(t.Indexes, model.Index{Name: ix.Name, Columns: ix.Columns, Unique: ix.Unique})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

// applyComments turns COMMENT ON statements into logical names and
// descriptions.
//
// The schema requires a logicalName on every table and column and a dump has
// none, so the rule is: the first line of the comment is the logical name, the
// rest is the description, and a table or column without a comment is named
// after itself.
func applyComments(dump *pgDump, idx *tableIndex, target string, d *diagList) {
	for _, c := range dump.Comments {
		if !inTargetSchema(c.Table, target) {
			continue
		}
		t := idx.table(c.Table.Name)
		if t == nil {
			d.warnf(c.Line, "comment on %s refers to an object that was not imported", c.Table.Name)
			continue
		}
		if c.Target == commentTable {
			t.LogicalName, t.Description = describeComment(c.Text, t.Name, t.Name, c.Line, d)
			continue
		}
		col := idx.column(t.Name, c.Column)
		if col == nil {
			d.warnf(c.Line, "comment on %s.%s refers to an object that was not imported", t.Name, c.Column)
			continue
		}
		col.LogicalName, col.Description = describeComment(c.Text, col.Name, t.Name+"."+col.Name, c.Line, d)
	}
}

// describeComment splits one comment into a logical name and a description,
// falling back to the physical name whenever the comment cannot supply one.
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
// asked for, else what a \connect line in the dump said, else the name of the
// input file. A dump of a single schema carries no database name of its own.
func databaseName(dump *pgDump, opt Options) (string, error) {
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

// normalizeDefault puts a default expression on one line.
//
// Every token is copied verbatim from the source, delimiters included, so a
// string literal keeps the spacing it was written with: the schema says a
// default is written verbatim as a SQL expression, and 'a  b' is not the same
// value as 'a b'. Only the gaps BETWEEN tokens collapse, each to a single
// space where there was any separation and to nothing where there was none, so
// that now() stays now() and a default that spanned three lines becomes
// readable in a generated table of columns.
//
// Collapsing the gaps also drops any comment that sat in one, which flattening
// to a single line requires: a surviving "--" would comment out the rest of the
// expression.
//
// An expression that does not lex is left to the whitespace-only fallback. It
// cannot happen for a default the parser accepted, but normalizeDefault must
// not lose the expression if it ever does.
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

// sequenceFromNextval reports the sequence a default reads from, when the
// default is exactly a nextval() call.
//
// The expression is lexed rather than matched: that handles the cast pg_dump
// writes, its absence in a hand-written dump, any spacing, a schema-qualified
// sequence name and a doubled quote inside the literal, all without a pattern
// that could go subtly wrong.
func sequenceFromNextval(expr string) (string, bool) {
	toks, err := lex([]byte(expr))
	if err != nil {
		return "", false
	}
	if len(toks) < 5 || !toks[0].isWord("nextval") || !toks[1].isPunct("(") || toks[2].kind != kindString {
		return "", false
	}
	rest := toks[3:]
	if rest[0].isPunct("::") { // ::regclass, which pg_dump always writes
		if len(rest) < 2 || rest[1].kind != kindWord {
			return "", false
		}
		rest = rest[2:]
	}
	// Exactly a closing parenthesis and the end of the expression: anything
	// more means the nextval was only part of a larger expression.
	if len(rest) != 2 || !rest[0].isPunct(")") || rest[1].kind != kindEOF {
		return "", false
	}
	return toks[2].text, true
}

// inTargetSchema reports whether an object belongs to the schema being
// imported. An unqualified name counts as belonging to it: older dumps rely on
// SET search_path and write bare names.
func inTargetSchema(q qname, target string) bool { return q.Schema == "" || q.Schema == target }

// checkIdentifier refuses a name the design format cannot hold. A silently
// renamed table is worse than a refused import: the document would look right
// and describe a database that does not exist.
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
