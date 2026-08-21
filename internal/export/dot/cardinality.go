package dot

import (
	"slices"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// Crow's foot cardinality
// ---------------------------------------------------------------------------

// end is one side of a relationship in crow's foot terms.
//
// Two independent bools rather than an enum of four constants, because the two
// questions - how many rows, and may there be none - are answered by different
// parts of the document: how many by the keys the table declares, may there be
// none by the nullability of the foreign key's own columns. Combining them is
// what arrow does.
type end struct {
	many     bool
	optional bool
}

// arrow names the graphviz arrow type that draws e.
//
// graphviz composes an arrow type from up to four primitives and draws the
// FIRST one closest to the node. That is what puts the cardinality mark - the
// crow's foot or the bar - against the table box and the optionality mark
// inboard of it, which is what crow's foot notation requires. Rendering one
// diagram confirms it: for arrowhead="crowodot" the crow's polygon touches the
// node's boundary and the circle sits further from the node.
//
// The mapping is written as a switch on the two bools rather than as the
// concatenation of two halves so that each of the four arrow type strings
// appears literally, exactly once, and can be found by grep.
func (e end) arrow() string {
	switch {
	case e.many && e.optional:
		return "crowodot"
	case e.many:
		return "crowtee"
	case e.optional:
		return "teeodot"
	default:
		return "teetee"
	}
}

// parentArrow is the arrow type of the parent side of every relationship: one,
// and mandatory.
//
// It is a constant rather than a derivation because there is nothing to
// derive. A foreign key names one specific row of the referenced table, in
// every foreign key of every document, so the parent end never varies. Both
// ends therefore carry exactly two arrow primitives, which keeps every edge in
// the diagram visually uniform.
const parentArrow = "teetee"

// childEnd derives the child side of the relationship that fk creates on t.
//
// This is INFERENCE. A jjf document never states a cardinality; it states
// columns, keys and nullability, and the notation below is read out of those.
// The rules, in full:
//
//   - The child side is ONE when fk's columns, as a set, are constrained to be
//     unique in t - by its primary key, by one of its unique keys, or by a
//     unique index. Otherwise the child side is MANY, which is the default,
//     because a plain foreign key column carries no uniqueness of its own.
//   - The child side is OPTIONAL when any foreign key column is nullable, and
//     MANDATORY when every one of them is NOT NULL.
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
func childEnd(t *model.Table, fk *model.ForeignKey) end {
	e := end{many: !uniqueIn(t, fk.Columns)}
	for _, name := range fk.Columns {
		// A column the table does not define contributes nothing: see
		// findColumn for why that is a legal document and why absent
		// information must not invent an optional relationship.
		if c := findColumn(t, name); c != nil && c.Nullable {
			e.optional = true
			break
		}
	}
	return e
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
// where such a document is reported, as a warning; this package renders it.
//
// Such a column is treated as NOT NULL, that is, it does not make the child end
// optional: absent information must not invent an optional relationship, and
// any other choice would let a typo silently change the notation. It is not an
// error and it is not reported - this package reports nothing, ever, it renders
// exactly what the document claims.
func findColumn(t *model.Table, name string) *model.Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}
