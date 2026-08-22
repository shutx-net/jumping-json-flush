// Package ddl renders a database design document as a PostgreSQL DDL script.
//
// jjf writes SQL TEXT and nothing else. It never connects to a database, never
// links a client library and never runs a statement, so the binary keeps the
// project's no-CGO, no-runtime-dependency property; applying the script is the
// reader's own step, with their own psql:
//
//	jjf export ddl db-design.json -o schema.sql
//	psql -d mydb -f schema.sql
//
// The script creates a schema from nothing. Applying it to a database that
// already has one is not a supported operation and will not become one:
// knowing how to move an existing schema from one state to another means
// knowing the state it is in, which means introspection, which is a different
// tool. The .sql is a build artifact exactly as the .xlsx and the .dot are -
// regenerate it, never edit it, never treat it as the design - and the file
// says so in its own first two lines.
//
// PostgreSQL only, and database.dbms must say so. Round-trip verification is
// possible only where an importer exists and only PostgreSQL has one; a
// dialect that cannot be checked end to end would ship on golden files alone,
// which prove nothing but that the generator emits what it emitted. The
// default field is the second reason: it holds verbatim SQL expression text,
// and '{}'::jsonb is PostgreSQL syntax.
//
// The output is deterministic: the same document always produces the same
// bytes, because nothing here reads the clock, no tool version and no input
// path is written into the file, and every loop walks a slice rather than a Go
// map.
//
// The schema has no place for these, so the script never contains them: CHECK
// constraints, CREATE TYPE, schemas other than the default, collations, partial
// and expression indexes, index methods, DEFERRABLE, storage parameters,
// partitioning, and row-level security. Nor are database.logicalName and
// database.description emitted: with no CREATE SCHEMA and no CREATE DATABASE
// there is no object for a COMMENT ON to attach to, and the importer never
// fills either field, so the round trip loses nothing.
//
// One of those omissions has a sharp edge worth stating plainly. Column types
// are opaque strings, so a document naming a user-defined type - an enum or a
// domain imported from PostgreSQL - produces a script that references a type no
// statement in it creates. The script parses and fails on execution. That is a
// limitation of the document format, not a bug here, and closing it would mean
// teaching the schema about type definitions.
package ddl

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// indent is the leading whitespace of one item inside CREATE TABLE. Four
// spaces, which is what pg_dump writes, so a generated script and a dump of the
// database it created read alike.
const indent = "    "

// Export renders doc and writes it to dst as a PostgreSQL DDL script.
//
// A document Accept refuses produces no output at all - not one byte, and no
// error from anywhere further down. The specification calls this all or
// nothing: a partially written script that fails on statement 40 after creating
// twelve tables is worse than no script. cmd/jjf calls Accept earlier still, so
// that the refusal is diagnosed before an output file is even opened, and this
// call is what keeps the package correct on its own terms - a promise a caller
// has to remember to keep is not one, and checking a small in-memory structure
// twice costs nothing.
//
// Nothing below this function returns an error. They all write into the
// bufio.Writer, which latches its first write error and turns every later write
// into a no-op, so Flush is the single place a failure can surface. This is
// internal/export/dot's structure and its reasoning applies unchanged:
// threading an error through every writer would add code and a type for no
// extra information.
func Export(dst io.Writer, doc *model.Document) error {
	if err := Accept(doc); err != nil {
		return err
	}

	bw := bufio.NewWriter(dst)
	writeScript(bw, doc)
	if err := bw.Flush(); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "write ddl", err)
	}
	return nil
}

// writeScript emits the whole script in the four fixed phases: every
// CREATE TABLE, then every CREATE INDEX, then every foreign key as an
// ALTER TABLE, then every COMMENT ON.
//
// The order is the specification's first choice and the reason there is no
// topological sort anywhere in this package. Nothing in phase 1 refers to
// another table, so mutual references and self references need no ordering
// between tables and no cycle handling. Phase 2 must precede phase 3 because
// PostgreSQL accepts a plain UNIQUE INDEX as a foreign key target, not only a
// UNIQUE constraint, so indexes[].unique is a legitimate source of the
// uniqueness a foreign key requires and has to exist first.
//
// The header carries no timestamp, no tool version and no input path. A version
// would make two builds of jjf disagree about the same document, which matters
// most for this artifact of the three, because this is the one that gets
// diffed.
func writeScript(w *bufio.Writer, doc *model.Document) {
	io.WriteString(w, "-- Generated by jjf from a database design document.\n")
	io.WriteString(w, "-- The JSON is the source of truth: edit it and export again, never this file.\n")

	// The schema requires at least one table with at least one column, so this
	// section is never empty and needs no guard. The three below do.
	io.WriteString(w, "\n-- Tables\n\n")
	for i := range doc.Tables {
		if i > 0 {
			io.WriteString(w, "\n")
		}
		writeCreateTable(w, &doc.Tables[i])
	}

	if hasIndexes(doc) {
		io.WriteString(w, "\n-- Indexes\n\n")
		writeIndexes(w, doc)
	}
	if hasForeignKeys(doc) {
		io.WriteString(w, "\n-- Foreign keys\n\n")
		writeForeignKeys(w, doc)
	}
	if hasComments(doc) {
		io.WriteString(w, "\n-- Comments\n\n")
		writeComments(w, doc)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: tables
// ---------------------------------------------------------------------------

// writeCreateTable emits one table: every column in document order, then the
// primary key, then the unique keys.
//
// Foreign keys are never inline, not even a self-reference. That is what makes
// phase 1 free of any dependency between tables.
func writeCreateTable(w *bufio.Writer, t *model.Table) {
	fmt.Fprintf(w, "CREATE TABLE %s (\n", quoteIdent(t.Name))

	items := make([]string, 0, len(t.Columns)+1+len(t.UniqueKeys))
	for i := range t.Columns {
		items = append(items, columnItem(&t.Columns[i]))
	}
	if pk := t.PrimaryKey; pk != nil {
		items = append(items, constraintPrefix(pk.Name)+"PRIMARY KEY ("+quotedList(pk.Columns)+")")
	}
	for _, uk := range t.UniqueKeys {
		items = append(items, constraintPrefix(uk.Name)+"UNIQUE ("+quotedList(uk.Columns)+")")
	}

	for i, item := range items {
		io.WriteString(w, indent)
		io.WriteString(w, item)
		if i < len(items)-1 {
			io.WriteString(w, ",")
		}
		io.WriteString(w, "\n")
	}
	io.WriteString(w, ");\n")
}

// columnItem renders one column definition.
//
// PostgreSQL accepts the column constraints in any order, so the order here is
// a style choice whose only requirement is that it never changes. IDENTITY and
// DEFAULT are mutually exclusive - Accept refuses a column carrying both,
// because PostgreSQL refuses one - so their relative order is never observable.
// NOT NULL last is what pg_dump writes.
//
// NOT NULL is emitted only when the document says the column is not nullable;
// NULL is SQL's own default and stating it adds nothing. A primary key column
// therefore carries both NOT NULL and PRIMARY KEY, which is redundant and is
// again what pg_dump writes.
//
// The default is copied out verbatim, with no quoting and no normalisation: the
// field is defined as SQL expression text, and internal/check has already
// refused an empty one or one that does not read as an expression.
func columnItem(c *model.Column) string {
	var b strings.Builder
	b.WriteString(quoteIdent(c.Name))
	b.WriteString(" ")
	b.WriteString(renderType(c))
	if c.AutoIncrement {
		// Standard SQL, and the form PostgreSQL recommends over SERIAL.
		// Available since PostgreSQL 10, so it needs no version gate anywhere
		// in the supported range.
		b.WriteString(" GENERATED BY DEFAULT AS IDENTITY")
	}
	if c.Default != nil {
		b.WriteString(" DEFAULT ")
		b.WriteString(*c.Default)
	}
	if !c.Nullable {
		b.WriteString(" NOT NULL")
	}
	return b.String()
}

// constraintPrefix names a constraint, or returns nothing when the document
// leaves it unnamed and PostgreSQL is left to invent one. The schema permits
// both for a primary key and for a unique key.
func constraintPrefix(name string) string {
	if name == "" {
		return ""
	}
	return "CONSTRAINT " + quoteIdent(name) + " "
}

// quotedList renders a column list for the inside of a parenthesis.
func quotedList(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteIdent(c)
	}
	return strings.Join(quoted, ", ")
}

// ---------------------------------------------------------------------------
// Phase 2: indexes
// ---------------------------------------------------------------------------

// writeIndexes emits one statement per index, tables in document order and each
// table's indexes in document order.
func writeIndexes(w *bufio.Writer, doc *model.Document) {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		for _, ix := range t.Indexes {
			unique := ""
			if ix.Unique {
				unique = "UNIQUE "
			}
			fmt.Fprintf(w, "CREATE %sINDEX %s ON %s (%s);\n",
				unique, quoteIdent(ix.Name), quoteIdent(t.Name), quotedList(ix.Columns))
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 3: foreign keys
// ---------------------------------------------------------------------------

// writeForeignKeys emits one ALTER TABLE per foreign key.
//
// The referential actions are written as the document spells them, which is
// legal PostgreSQL text for all five. ON UPDATE precedes ON DELETE, matching
// the field order in model.ForeignKey and the property order in the schema. NO
// ACTION is PostgreSQL's own default and pg_dump omits it, so a document that
// states it explicitly does not get it back from a round trip; that is expected
// rather than a bug, and the documentation says so.
func writeForeignKeys(w *bufio.Writer, doc *model.Document) {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		for _, fk := range t.ForeignKeys {
			fmt.Fprintf(w, "ALTER TABLE %s ADD %sFOREIGN KEY (%s) REFERENCES %s (%s)",
				quoteIdent(t.Name), constraintPrefix(fk.Name), quotedList(fk.Columns),
				quoteIdent(fk.References.Table), quotedList(fk.References.Columns))
			if fk.OnUpdate != "" {
				fmt.Fprintf(w, " ON UPDATE %s", fk.OnUpdate)
			}
			if fk.OnDelete != "" {
				fmt.Fprintf(w, " ON DELETE %s", fk.OnDelete)
			}
			io.WriteString(w, ";\n")
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 4: comments
// ---------------------------------------------------------------------------

// writeComments emits COMMENT ON statements: tables in document order, and
// within each table its own comment first, then its columns in document order.
func writeComments(w *bufio.Writer, doc *model.Document) {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		if text, ok := commentText(t.Name, t.LogicalName, t.Description); ok {
			fmt.Fprintf(w, "COMMENT ON TABLE %s IS %s;\n", quoteIdent(t.Name), quoteLiteral(text))
		}
		for j := range t.Columns {
			c := &t.Columns[j]
			if text, ok := commentText(c.Name, c.LogicalName, c.Description); ok {
				fmt.Fprintf(w, "COMMENT ON COLUMN %s.%s IS %s;\n",
					quoteIdent(t.Name), quoteIdent(c.Name), quoteLiteral(text))
			}
		}
	}
}

// commentText composes the comment of one object and reports whether it is
// worth writing.
//
// The composition is the exact inverse of the importer's splitComment, which
// cuts a comment at its first newline and makes the first line the logical name
// and the rest the description. Joining them the same way is what closes the
// round trip, and it is why the newline is a real one rather than an escape.
//
// Nothing is written for an object whose logical name is just its physical name
// and which has no description: that is precisely the state the importer
// creates for an object the dump carried no comment on, so skipping it is the
// exact inverse of that fallback and still round-trips. It also keeps a
// fifty-table document from ending in hundreds of statements that say nothing.
func commentText(physical, logical, description string) (string, bool) {
	if logical == physical && description == "" {
		return "", false
	}
	if description == "" {
		return logical, true
	}
	return logical + "\n" + description, true
}

// ---------------------------------------------------------------------------
// Section guards
// ---------------------------------------------------------------------------

// hasIndexes, hasForeignKeys and hasComments decide whether their section
// exists at all. A document without one must still produce a syntactically
// complete script with no dangling section comment and no dangling blank line -
// internal/export/dot's hasForeignKeys is the same guard for the same reason.

func hasIndexes(doc *model.Document) bool {
	for i := range doc.Tables {
		if len(doc.Tables[i].Indexes) > 0 {
			return true
		}
	}
	return false
}

func hasForeignKeys(doc *model.Document) bool {
	for i := range doc.Tables {
		if len(doc.Tables[i].ForeignKeys) > 0 {
			return true
		}
	}
	return false
}

func hasComments(doc *model.Document) bool {
	for i := range doc.Tables {
		t := &doc.Tables[i]
		if _, ok := commentText(t.Name, t.LogicalName, t.Description); ok {
			return true
		}
		for j := range t.Columns {
			c := &t.Columns[j]
			if _, ok := commentText(c.Name, c.LogicalName, c.Description); ok {
				return true
			}
		}
	}
	return false
}
