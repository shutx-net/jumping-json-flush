package erd

import (
	"slices"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// Crow's foot cardinality
// ---------------------------------------------------------------------------

// End is one side of a relationship in crow's foot terms.
//
// Two independent bools rather than an enum of four constants, because the two
// questions - how many rows, and may there be none - are answered from
// different places, and WHICH place answers which depends on the end. On the
// child end, how many comes from the keys the child table declares, while may
// there be none is not read out of the document at all: it is always yes, for
// the reason ChildEnd gives. On the parent end it is the other way round: how
// many is always one, because a foreign key names one specific row, and may
// there be none comes from the nullability of the foreign key's own columns.
// Combining them into a notation belongs to the exporter, not here:
// internal/export/svg's appendEnd is one such mapping.
type End struct {
	Many     bool
	Optional bool
}

// ParentEnd derives the parent side of the relationship that fk creates on t:
// how many rows of the referenced table one row of t points at.
//
// This is INFERENCE, exactly as ChildEnd is, and it reads the same one table.
// The rules, in full:
//
//   - The parent side is always ONE. A foreign key names one specific row of
//     the referenced table, in every foreign key of every document, so the
//     maximum at this end is not something a document can change. It is
//     deliberately not taken from the referenced table's keys either - see
//     below.
//   - The parent side is OPTIONAL when any foreign key column is nullable, and
//     MANDATORY when every one of them is NOT NULL. A nullable foreign key
//     column is exactly what lets a child row exist while pointing at no
//     parent, and "may there be none" is the minimum at THIS end. This is the
//     bit the child end used to carry, where it said something the constraint
//     does not say.
//
// Only the CHILD table is consulted here too. Nothing about the referenced
// table is read, which is why a self-referencing foreign key needs no special
// case and why a foreign key naming a table the document does not define still
// gets a cardinality.
func ParentEnd(t *model.Table, fk *model.ForeignKey) End {
	var e End
	for _, name := range fk.Columns {
		// A column the table does not define contributes nothing: see
		// findColumn for why that is a legal document and why absent
		// information must not invent an optional relationship.
		if c := findColumn(t, name); c != nil && c.Nullable {
			e.Optional = true
			break
		}
	}
	return e
}

// ChildEnd derives the child side of the relationship that fk creates on t:
// how many rows of t point at one row of the referenced table.
//
// This is INFERENCE. A jjf document never states a cardinality; it states
// columns, keys and nullability, and the notation below is read out of those.
// The rules, in full:
//
//   - The child side is ONE when fk's columns, as a set, are constrained to be
//     unique in t - by its primary key, by one of its unique keys, or by a
//     unique index. Otherwise the child side is MANY, which is the default,
//     because a plain foreign key column carries no uniqueness of its own.
//   - The child side is always OPTIONAL. Nothing a jjf document can state makes
//     it mandatory: a primary key, a unique key and NOT NULL all constrain how
//     many children one parent row may have, never how few, so a minimum of one
//     here would be a claim about participation that the document does not
//     make. Nullability is not read on this end either - it says whether a
//     child may exist with no parent, which is the minimum at the PARENT end,
//     and ParentEnd is where it is read.
//
// A unique INDEX counts alongside the declared keys because the schema says it
// does: indexes[].unique is documented as "Whether the index enforces
// uniqueness", so a foreign key whose columns match one is genuinely 1:1 in
// the database, and a diagram that said many would be describing a different
// database from the one the document describes. Which of the three equivalent
// spellings an author picked must not change the notation.
//
// Only the CHILD table is consulted. Nothing about the referenced table is
// read, which is why a self-referencing foreign key needs no special case and
// why a foreign key naming a table the document does not define still gets a
// cardinality.
func ChildEnd(t *model.Table, fk *model.ForeignKey) End {
	return End{Many: !uniqueIn(t, fk.Columns), Optional: true}
}

// uniqueIn reports whether t constrains cols, as a set, to be unique.
//
// The three sources are walked in the order a reader would look for them -
// primary key, unique keys, unique indexes - and each is a slice, so the answer
// is reached deterministically even though it does not depend on which of them
// matched. A non-unique index is not a source of uniqueness and is skipped.
func uniqueIn(t *model.Table, cols []string) bool {
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
// is still one relationship.
//
// Comparing lengths and then containment is a correct set comparison only
// because $defs/columnNameList carries uniqueItems: true, so neither list can
// hold a duplicate in a document that passed validation - and the exporter is
// only ever reached after validation. Without that, this same code would call
// {a, a} and {a, b} equal.
//
// The comparison is byte for byte and case sensitive. jjf folds identifier case
// nowhere - the importer preserves what the dump said - so USER_ID and user_id
// are two different columns here, and a document that mixes them derives many.
//
// slices.Contains rather than a map: the lists hold a handful of names, and
// keeping the package free of any map whose iteration order could be observed
// is worth more than the asymptotics. It also allocates nothing.
func sameColumnSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, name := range a {
		if !slices.Contains(b, name) {
			return false
		}
	}
	return true
}

// findColumn returns t's first column called name, or nil.
//
// It returns nil rather than reporting an error because a foreign key naming a
// column the table does not define is a LEGAL document: $defs/foreignKey
// constrains columns to $defs/columnNameList, which is a list of identifiers
// and nothing more. The derivation must therefore be total. "jjf validate" is
// where such a document is reported, as a warning; this package derives from
// it regardless.
//
// Such a column is treated as NOT NULL, that is, it does not make the PARENT
// end optional (the child end is optional either way): absent information must
// not invent an optional relationship, and any other choice would let a typo
// silently change the notation. It is not an error and it is not reported -
// this package reports nothing, ever, it derives from exactly what the
// document claims and leaves the drawing to its callers.
func findColumn(t *model.Table, name string) *model.Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}
