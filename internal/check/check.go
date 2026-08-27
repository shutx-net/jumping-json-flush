// Package check reports the ways a decoded database design document
// contradicts itself: keys and indexes naming columns their table does not
// define, foreign keys pointing at nothing, a primary key column declared
// nullable, names a single table uses twice, and defaults that are empty or do
// not read as a SQL expression.
//
// The line this package draws is deliberate: a foreign key with no target is
// not a bad design, it is not a document. Whether the design is a good one is
// none of jjf's business and is never checked - normalization, index strategy,
// type suitability and naming conventions belong to the author. Neither are the
// things that are properties of a particular database system rather than of the
// document: the uniqueness of index names across a schema is a PostgreSQL rule
// and belongs with a future DDL exporter, not here. Duplicate TABLE names in
// one document are likewise not checked; only duplicates within one table are.
//
// Findings are located by a readable label - "foreign key fk_orders_customer on
// table orders" - and never by a line, a byte offset or a JSON Pointer. The
// decoded model carries no source positions, internal/model must not grow any,
// and a pointer-shaped report would be mistaken for a JSON Schema violation,
// which is a different failure with a different exit code.
//
// Nothing here decides what a finding costs. Document returns a slice; the CLI
// prints it as warnings and decides whether -strict promotes it to a failure.
package check

import (
	"slices"
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// Document reports every way doc contradicts itself, in document order, and
// returns nil when it contradicts itself in none.
//
// It is total: doc need not have passed JSON Schema validation first. A table
// with no columns, a foreign key with empty column lists, an absent primary key
// and repeated names are all reachable states of the decoded model, and none of
// them may panic here.
func Document(doc *model.Document) []Finding {
	var f findingList

	defined := definedTables(doc)
	for i := range doc.Tables {
		checkTable(&doc.Tables[i], defined, &f)
	}
	return f.all()
}

// definedTables maps every table name in doc to the table that carries it.
//
// The map is only ever LOOKED UP, never ranged over: ranging would make the
// order of the findings depend on Go's map iteration and destroy the
// determinism the tests and the CLI output rest on.
//
// Two tables sharing a name is not forbidden by the schema and is not one of
// the checks; the FIRST occurrence wins and a later one does not replace it, so
// that a foreign key resolves to the table a reader scrolling from the top
// would find.
func definedTables(doc *model.Document) map[string]*model.Table {
	defined := make(map[string]*model.Table, len(doc.Tables))
	for i := range doc.Tables {
		if _, ok := defined[doc.Tables[i].Name]; !ok {
			defined[doc.Tables[i].Name] = &doc.Tables[i]
		}
	}
	return defined
}

// checkTable walks one table, appending findings in the order a reader meets
// the objects in the JSON: the columns, the primary key, the unique keys, the
// foreign keys, then the indexes.
func checkTable(t *model.Table, defined map[string]*model.Table, f *findingList) {
	label := tableLabel(t.Name)

	// C6a - a table that defines the same column twice does not describe one
	// table, whichever of the two definitions a reader believes.
	var columnNames usedNames
	for i := range t.Columns {
		if columnNames.add(t.Columns[i].Name) {
			f.addf(label, "defines column %q more than once", t.Columns[i].Name)
		}
		// C7 and C8 - a column's default is the one thing about a column that
		// can disagree with itself rather than with something else, so it is
		// asked about here, inside the walk that already visits the columns in
		// document order.
		checkColumnDefault(&t.Columns[i], i+1, t.Name, f)
	}

	// C6b - constraint and index names share one namespace per table, and the
	// generated documents address a constraint by its name alone. Columns and
	// constraints do NOT share a namespace, so this is a second, separate set.
	var constraintNames usedNames

	if pk := t.PrimaryKey; pk != nil {
		// A table has at most one primary key, so its label needs no position.
		pkLabel := constraintLabel("primary key", pk.Name, 0, t.Name)
		// This cannot fire: add reports a repeat, and the primary key is the
		// first name to enter a set created empty just above. It is written
		// anyway so the four rules here read as one rule applied four times,
		// and so that reordering them makes it live rather than silently
		// wrong. A name a unique key repeats is still reported, by its own add.
		if constraintNames.add(pk.Name) {
			f.addf(label, "declares more than one constraint or index called %q", pk.Name)
		}
		for _, name := range pk.Columns {
			c := findColumn(t, name)
			if c == nil {
				// C1 - and there is no nullability to speak of, so C5 is not
				// evaluated: one authoring mistake, one finding.
				f.addf(pkLabel, "names column %q, which the table does not define", name)
				continue
			}
			// C5 - SQL forces a primary key column NOT NULL, so a document
			// that says otherwise disagrees with the database it describes.
			if c.Nullable {
				f.addf(pkLabel, "names column %q, which the table declares nullable", name)
			}
		}
	}

	for i, uk := range t.UniqueKeys {
		ukLabel := constraintLabel("unique key", uk.Name, i+1, t.Name)
		if constraintNames.add(uk.Name) {
			f.addf(label, "declares more than one constraint or index called %q", uk.Name)
		}
		reportMissingColumns(t, ukLabel, uk.Columns, f)
	}

	for i, fk := range t.ForeignKeys {
		fkLabel := constraintLabel("foreign key", fk.Name, i+1, t.Name)
		if constraintNames.add(fk.Name) {
			f.addf(label, "declares more than one constraint or index called %q", fk.Name)
		}
		checkForeignKey(t, fkLabel, fk, defined, f)
	}

	for i, ix := range t.Indexes {
		ixLabel := constraintLabel("index", ix.Name, i+1, t.Name)
		if constraintNames.add(ix.Name) {
			f.addf(label, "declares more than one constraint or index called %q", ix.Name)
		}
		reportMissingColumns(t, ixLabel, ix.Columns, f)
	}
}

// checkForeignKey covers the three checks a foreign key adds to C1: that both
// ends name the same number of columns, that the target table is one this
// document defines, and that the target columns are constrained to be unique
// there.
func checkForeignKey(t *model.Table, label string, fk model.ForeignKey, defined map[string]*model.Table, f *findingList) {
	// C1 on the near end, first: a column the child table does not define is a
	// problem regardless of what sits at the other end.
	reportMissingColumns(t, label, fk.Columns, f)

	// C3 - comparing two lengths needs no target table, so it is evaluated
	// whether or not the target exists.
	matched := len(fk.Columns) == len(fk.References.Columns)
	if !matched {
		f.addf(label, "names %d column(s) but references %d", len(fk.Columns), len(fk.References.Columns))
	}

	// C2 - the reference is byte for byte and case sensitive. jjf folds
	// identifier case nowhere, so a reference to Orders in a document that
	// defines orders is an undefined target.
	target, ok := defined[fk.References.Table]
	if !ok {
		f.addf(label, "references table %q, which this document does not define", fk.References.Table)
		return
	}

	// C4 - only reachable once there is a target to compare against and the two
	// ends agree on how many columns to compare. Reporting it after either of
	// those would be a second, derived finding for one mistake.
	if !matched {
		return
	}
	if !constrainedUnique(target, fk.References.Columns) {
		f.addf(label, "references (%s) of table %s, which no primary key, unique key or unique index there constrains to be unique",
			strings.Join(fk.References.Columns, ", "), target.Name)
	}
}

// reportMissingColumns records C1 for every name in cols that t does not
// define, in declaration order.
//
// Every missing name is reported, not just the first: the importer stops at the
// first because it is about to drop the whole constraint, but this is a report,
// and the author has to fix all of them.
func reportMissingColumns(t *model.Table, label string, cols []string, f *findingList) {
	for _, name := range cols {
		if findColumn(t, name) == nil {
			f.addf(label, "names column %q, which the table does not define", name)
		}
	}
}

// constrainedUnique reports whether t constrains cols, as a set, to be unique.
//
// All three sources count. A plain UNIQUE INDEX is a valid foreign key target
// in PostgreSQL, not only a UNIQUE constraint, so which of the equivalent
// spellings the author picked must not change the answer - the same reasoning
// internal/export/erd/cardinality.go gives for the diagram. They are walked in
// the order a reader would look for them: primary key, unique keys, unique
// indexes. A non-unique index is not a source of uniqueness and is skipped.
func constrainedUnique(t *model.Table, cols []string) bool {
	// A table without a primary key is explicitly representable, so the nil
	// check comes before the dereference.
	if t.PrimaryKey != nil && sameColumnSet(cols, t.PrimaryKey.Columns) {
		return true
	}
	for _, uk := range t.UniqueKeys {
		if sameColumnSet(cols, uk.Columns) {
			return true
		}
	}
	for _, ix := range t.Indexes {
		if ix.Unique && sameColumnSet(cols, ix.Columns) {
			return true
		}
	}
	return false
}

// sameColumnSet reports whether a and b name the same columns, AS SETS: order
// does not matter, so a foreign key on (b, a) against a primary key on (a, b)
// is still the same target.
//
// Containment is checked in BOTH directions. internal/export/erd has a helper
// of the same idea that compares lengths and then containment one way, which is
// correct only because $defs/columnNameList carries uniqueItems: true and the
// exporter is only ever reached after validation. This package must be total
// over documents that were never validated, where that shorter version would
// call {a, a} and {a, b} equal. The two are therefore not the same function and
// are deliberately not shared.
//
// The comparison is byte for byte and case sensitive, for the same reason as
// everywhere else in jjf: identifier case is folded nowhere.
func sameColumnSet(a, b []string) bool {
	for _, name := range a {
		if !slices.Contains(b, name) {
			return false
		}
	}
	for _, name := range b {
		if !slices.Contains(a, name) {
			return false
		}
	}
	return true
}

// findColumn returns t's first column called name, or nil when it defines none.
func findColumn(t *model.Table, name string) *model.Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

// usedNames remembers the names one table has already spent, within a single
// namespace.
//
// It is a slice rather than a map for the same reason the rest of this package
// avoids maps it would have to iterate: the lists hold a handful of names, and
// keeping the order of the findings out of Go's hands is worth more than the
// asymptotics.
type usedNames struct {
	seen     []string
	reported []string
}

// add records name and reports whether it is a duplicate worth reporting.
//
// An empty name is never one: the schema makes primaryKey.name,
// uniqueKeys[].name and foreignKeys[].name optional, so several unnamed
// constraints in one table are ordinary, not a collision. A name already
// reported once is not reported again either - a column defined three times is
// one mistake, not two.
func (u *usedNames) add(name string) bool {
	if name == "" {
		return false
	}
	if !slices.Contains(u.seen, name) {
		u.seen = append(u.seen, name)
		return false
	}
	if slices.Contains(u.reported, name) {
		return false
	}
	u.reported = append(u.reported, name)
	return true
}

// tableLabel names a table in a finding. C6 uses it because both halves of that
// check are properties of the table rather than of any one constraint.
func tableLabel(name string) string { return "table " + name }

// constraintLabel names a constraint, an index or a column in a finding, always
// in the shape "<kind> <who> on table <table>". A column fits that shape
// exactly, so it is labelled here rather than by a second function that would
// differ from this one in nothing.
//
// An unnamed constraint - or a column whose name the document left out, which
// is reachable because this package is total over unvalidated documents - falls
// back to its 1-based position in the array that holds it, because the reader
// is counting entries in a JSON array by eye and starts at one - unlike the JSON Pointers internal/schema reports, which are
// 0-based because that is what the pointer syntax means. Pass pos 0 for an
// object a table can only have one of, which needs no position at all.
func constraintLabel(kind, name string, pos int, table string) string {
	switch {
	case name != "":
		return kind + " " + name + " on table " + table
	case pos > 0:
		return kind + " #" + strconv.Itoa(pos) + " on table " + table
	default:
		return kind + " on table " + table
	}
}
