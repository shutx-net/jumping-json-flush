// Package ddl renders a database design document as a DDL script.
//
// jjf writes SQL TEXT and nothing else. It never connects to a database, never
// links a client library and never runs a statement, so the binary keeps the
// project's no-CGO, no-runtime-dependency property; applying the script is the
// reader's own step, with their own client:
//
//	jjf export ddl db-design.json -o schema.sql
//	psql -d mydb -f schema.sql
//
// The script creates a schema from nothing. Applying it to a database that
// already has one is not a supported operation and will not become one:
// knowing how to move an existing schema from one state to another means
// knowing the state it is in, which means introspection, which is a different
// tool. The .sql is a build artifact exactly as the .xlsx and the .svg are -
// regenerate it, never edit it, never treat it as the design - and the file
// says so in its own first two lines.
//
// Which dialect is written is database.dbms, and the field must be there. The
// dialects jjf writes are the ones that can be answered by something other
// than their own output: an importer to read a real database back, and a
// live-server round trip in CI to run the two against each other. A dialect
// that cannot be checked end to end would ship on golden files alone, which
// prove nothing but that the generator emits what it emitted, so the table in
// dialect.go is short on purpose and grows one entry at a time.
// design/ddl-export.md states that gate and records what each dialect decided.
// The dialects supported today are PostgreSQL and MySQL.
//
// The two writers differ in more than a delimiter, and the differences are
// enumerated as M1 to M8 in that document rather than discovered by reading the
// code: MySQL writes three phases instead of four, because it has no COMMENT ON
// statement and the comments have to ride on the definitions themselves; it
// quotes with backticks; autoIncrement is AUTO_INCREMENT and carries four rules
// PostgreSQL's identity columns do not; its foreign key names are schema-wide
// while its index names are per table, which is the exact inverse of
// PostgreSQL; and a backslash inside a string literal is an escape character
// there and an ordinary byte here.
//
// The output is deterministic: the same document always produces the same
// bytes, because nothing here reads the clock, no tool version and no input
// path is written into the file, and every loop walks a slice rather than a Go
// map.
//
// The schema has no place for these, so the script never contains them: CHECK
// constraints, CREATE TYPE, schemas other than the default, collations, partial
// and expression indexes, index methods, DEFERRABLE, storage parameters,
// partitioning, and row-level security. The MySQL script is the same idea
// against a different grammar: no CHECK constraints, no triggers, no views, no
// table options - engine, character set, collation, row format, partitioning -
// and no ON UPDATE CURRENT_TIMESTAMP, because a script that guessed at an
// engine or a collation would be writing a design decision nobody made. Nor are
// database.logicalName and database.description emitted: with no CREATE SCHEMA
// and no CREATE DATABASE there is no object for a COMMENT ON to attach to, and
// the importer never fills either field, so the round trip loses nothing.
//
// One of those omissions has a sharp edge worth stating plainly. Column types
// are opaque strings, so a document naming a user-defined type - an enum or a
// domain imported from PostgreSQL - produces a script that references a type no
// statement in it creates. The script parses and fails on execution. That is a
// limitation of the document format, not a bug here, and closing it would mean
// teaching the schema about type definitions.
//
// ENUM and SET are the same edge in MySQL, one step sharper: their parenthesis
// holds a value list rather than a number, the format has nowhere to keep one,
// and MySQL answers a bare ENUM at parse time rather than on execution. They
// are emitted as the document writes them all the same, because every ENUM
// column is in that state - including one the importer has just produced from a
// real database - so refusing them would refuse documents jjf itself wrote. A
// VARCHAR with no length is not the same case and IS refused: mysqldump always
// writes the length, so the refusal is unreachable from an imported document
// and cannot break the round trip.
package ddl

import (
	"bufio"
	"io"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// indent is the leading whitespace of one item inside CREATE TABLE. Four
// spaces, which is what pg_dump writes, so a generated script and a dump of the
// database it created read alike.
//
// mysqldump writes two, and the MySQL writer indents with four anyway. One
// constant for the package rather than a width per dialect, because the round
// trip compares DOCUMENTS and never SQL text - design/ddl-export.md says so for
// both dialects - so the width buys a reader's eye and nothing else, and two
// scripts from one tool that indent differently would cost more than they
// bought.
const indent = "    "

// Export renders doc and writes it to dst as a DDL script in the dialect the
// document names.
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
// internal/export/svg's structure and its reasoning applies unchanged:
// threading an error through every writer would add code and a type for no
// extra information.
func Export(dst io.Writer, doc *model.Document) error {
	if err := Accept(doc); err != nil {
		return err
	}
	// The lookup cannot fail here: Accept's first act is the same lookup, and
	// it returns an error when it misses. It is repeated rather than threaded
	// out of Accept because Accept's answer is "may this be written", not
	// "how", and widening it to return a dialect would export the seam into a
	// signature every caller in cmd/jjf would have to carry.
	d, ok := lookupDialect(doc.Database.DBMS)
	if !ok {
		return exitcode.Wrap(exitcode.InvalidInput, "", errNoDialect(doc.Database.DBMS))
	}

	bw := bufio.NewWriter(dst)
	d.write(bw, doc)
	if err := bw.Flush(); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "write ddl", err)
	}
	return nil
}

// commentText composes the comment of one object and reports whether it is
// worth writing.
//
// The composition is the exact inverse of the importer's splitComment, which
// cuts a comment at its first newline and makes the first line the logical name
// and the rest the description. Joining them the same way is what closes the
// round trip, and it is why the newline is a real one rather than an escape.
// Every dialect joins them identically, whatever statement it then puts them
// in, so this is shared rather than per-dialect.
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

// hasIndexes and hasForeignKeys decide whether their section exists at all. A
// document without one must still produce a syntactically complete script with
// no dangling section comment and no dangling blank line.
//
// They are shared because they ask about the document rather than about a
// dialect: every dialect that writes indexes at all writes none when there are
// none. A guard that depends on what a dialect does with a field - phase 4's,
// which exists only for a dialect with a COMMENT ON statement - belongs beside
// that dialect's writer instead, as pgHasComments does.

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
