// Package erd holds the document-level derivations an entity relationship
// diagram needs: the questions about the design itself, as against the
// questions about the file the design is being written into.
//
// That boundary is what a piece of code is a statement ABOUT. A question about
// the DOCUMENT is answered here: which end of a relationship is many, whether
// it is optional, how a type with a declared size reads as one string, which
// keys a column takes part in, what an unnamed foreign key is called, and which
// tables a foreign key names that the document never defines. A question about
// the OUTPUT FORMAT is answered by the exporter and never here: choosing a
// glyph, placing it, or escaping a name for the file it is going into are
// statements about a file, not about the design.
//
// They are here rather than inside the exporter that draws, because an answer
// about the document is not the drawing's private business.
// internal/check/check.go reasons about the same uniqueness rule when it
// decides whether a foreign key has a legal target, and its sameColumnSet says
// in its own comment why check's version of that helper has to be the longer
// one. internal/export/ddl/typemap.go names RenderType's size precedence as one
// of the places that rule lives. A derivation three packages have an opinion
// about does not belong inside the one that happens to draw it, and a copy of
// it inside the drawing could come to disagree with them with no test able to
// see it: each file would be individually correct for the code that wrote it.
//
// Nothing here checks anything or reports anything. A foreign key naming a
// table the document does not define, or a column its own table does not have,
// is a legal document; "jjf validate" is where that is reported, and these
// functions answer exactly what the document claims.
//
// One exception, stated plainly rather than left for a reader to find:
// RenderType's size precedence mirrors sizeOf in
// internal/export/xlsx/tabledef.go exactly, and putting it here did not remove
// that duplication. The rule still lives in two places, erd and xlsx, because
// the workbook answers a different question - the type and the size in two
// cells rather than one string - and the comment tying the two together is what
// keeps them in step.
package erd

import (
	"slices"
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// RenderType folds a column's declared size back into its type name, which is
// the opposite of what the workbook does: the xlsx exporter puts the type in
// one column and the size in another, while a diagram table cell wants one
// string.
//
// The precedence mirrors sizeOf in internal/export/xlsx/tabledef.go exactly -
// length first, then precision with scale, then precision alone - so that the
// two exporters can never disagree about the same document. All three are
// pointers, so an absent size prints nothing rather than a zero.
//
// Nothing is escaped here. Escaping happens once, where the string is written
// into a label, so that a caller cannot double-escape by accident.
// $defs/columnType is ^[A-Za-z][A-Za-z0-9_ ]*$, so a type name may contain
// spaces, and nothing downstream has to mangle them: the string goes into a
// text cell in the picture and a spreadsheet cell in the workbook, and a space
// is a space in both. TIMESTAMP WITH TIME ZONE needs no substitute spelling.
func RenderType(c *model.Column) string {
	switch {
	case c.Length != nil:
		return c.Type + "(" + strconv.Itoa(*c.Length) + ")"
	case c.Precision != nil && c.Scale != nil:
		return c.Type + "(" + strconv.Itoa(*c.Precision) + "," + strconv.Itoa(*c.Scale) + ")"
	case c.Precision != nil:
		return c.Type + "(" + strconv.Itoa(*c.Precision) + ")"
	default:
		return c.Type
	}
}

// Marker names the keys the column takes part in: PK, FK, both, or nothing.
//
// Unique keys and indexes get no marker. The diagram shows the two kinds of
// key that shape the relationships; a third mark would need a third cell in
// every row for something the table sheet of the workbook already lists.
func Marker(t *model.Table, column string) string {
	// A table without a primary key is explicitly representable, hence the nil
	// check before the dereference.
	pk := t.PrimaryKey != nil && slices.Contains(t.PrimaryKey.Columns, column)

	fk := false
	for i := range t.ForeignKeys {
		if slices.Contains(t.ForeignKeys[i].Columns, column) {
			fk = true
			break
		}
	}

	switch {
	case pk && fk:
		return "PK,FK"
	case pk:
		return "PK"
	case fk:
		return "FK"
	default:
		return ""
	}
}

// EdgeLabel names a relationship on the diagram.
//
// The constraint name when the document gives one. A name is optional in the
// schema, and two parallel edges still have to be told apart, so the fallback
// is the child column list - the next most specific thing the document
// actually says about the relationship. The label is therefore never empty, so
// no relationship is drawn without a name a reader can match against the JSON.
func EdgeLabel(fk *model.ForeignKey) string {
	if fk.Name != "" {
		return fk.Name
	}
	return strings.Join(fk.Columns, ", ")
}

// definedTables collects the names the document defines.
//
// The map is only ever LOOKED UP, never ranged over: ranging would make the
// order of the stub nodes depend on Go's map iteration and destroy the
// byte-for-byte determinism the golden files rest on. Two tables sharing a
// name is not forbidden by the schema and simply sets the same key twice.
func definedTables(doc *model.Document) map[string]bool {
	defined := make(map[string]bool, len(doc.Tables))
	for i := range doc.Tables {
		defined[doc.Tables[i].Name] = true
	}
	return defined
}

// UndefinedTargets lists the tables that need a stub, in first-reference
// order: the document's tables in order, and within each its foreign keys in
// order. Returning a slice rather than a set is what keeps the output
// deterministic; the map that remembers what has already been appended is,
// like defined, only ever looked up.
//
// Comparison is byte for byte, so a reference to Orders in a document that
// defines orders is an undefined target and gets a stub. jjf folds identifier
// case nowhere.
func UndefinedTargets(doc *model.Document) []string {
	defined := definedTables(doc)

	var out []string
	seen := make(map[string]bool)

	for i := range doc.Tables {
		for _, fk := range doc.Tables[i].ForeignKeys {
			name := fk.References.Table
			if defined[name] || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
