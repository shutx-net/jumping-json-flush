package ddl

import (
	"bufio"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// The dialect table
// ---------------------------------------------------------------------------

// dialect is everything this package needs to know about one target database
// system from the outside: which database.dbms value selects it, what a
// message is to call it, what it adds to internal/check's findings, and how it
// writes a script.
//
// Four struct fields and not a Go interface. An interface would cost a named
// type, a method set and a receiver to carry what two function values already
// carry, and the display NAME - which every refusal message needs and which no
// writer and no check ever consults - would have to sit outside it anyway.
// AGENTS.md rules out unnecessary interfaces by name, and cmd/jjf/export.go
// answered the identical question the identical way with exportFormat, whose
// comment gives the same argument for the same reason.
//
// The seam is deliberately this narrow. Everything else a dialect knows - its
// quoting, its type table, the writer of each of its phases - is reached only
// from inside that dialect's own file and needs no entry here. A wider seam
// would invite a caller to ask a dialect a question that this package should
// be answering once, for both of them.
type dialect struct {
	// dbms is the document value that selects this dialect. lookupDialect
	// compares it exactly; nothing folds case or trims.
	dbms model.DBMS
	// name is what a message calls this dialect. It is the display spelling
	// rather than the enum value because RefusedError carries it out to
	// cmd/jjf, which cannot see this table and must not have to guess.
	name string
	// check reports what is true of this dialect rather than of the document.
	// It returns ONLY its own findings and never calls check.Document: Check
	// concatenates the two groups, which is what keeps them un-interleaved.
	check func(*model.Document) []check.Finding
	// write emits the whole script. It returns no error, because every writer
	// below it writes into a bufio.Writer that latches its first failure and
	// turns every later write into a no-op, so Export's Flush is the single
	// place a failure can surface.
	write func(*bufio.Writer, *model.Document)
}

// dialects lists the dialects jjf writes, in the order a message names them.
//
// It is a function returning a fresh slice rather than a package-level var,
// for the reason cmd/jjf/export.go's exportFormats gives verbatim: a
// package-level slice of structs holding function values is exactly the
// mutable package-level state AGENTS.md rules out. It is called a handful of
// times per export, so the allocation is beside the point.
//
// A dialect belongs in this table when it can be answered by something other
// than its own output - an importer and a live-server round trip in CI - which
// is the gate design/ddl-export.md states and the reason the table is short.
func dialects() []dialect {
	return []dialect{
		{
			dbms:  model.DBMSPostgreSQL,
			name:  "PostgreSQL",
			check: pgCheck,
			write: pgWriteScript,
		},
	}
}

// lookupDialect finds the dialect selected by d, and reports whether jjf has
// one at all.
//
// The comparison is exact, and the schema's dbms enum is what makes that safe:
// a document that reached this package has been validated against
// schema/db-design.schema.json, so "postgresql" and "Postgres" are not
// spellings to be tolerant of but values no document can carry. A slice walk
// rather than a map lookup, for the reason cmd/jjf/export.go's
// lookupExportFormat gives: the table is a handful of entries and its order is
// meaningful, since it is the order dialectNames prints and therefore the
// order the CLI advertises.
func lookupDialect(d model.DBMS) (dialect, bool) {
	for _, entry := range dialects() {
		if entry.dbms == d {
			return entry, true
		}
	}
	return dialect{}, false
}

// dialectNames lists the dialect names for the messages that have to say what
// jjf can do.
//
// Generated from the table rather than spelled out in the message, so that the
// day a dialect is added the sentence is already right. That is the drift
// argument cmd/jjf/export.go's exportUsage makes for the format list, and it
// matters more here: the refusal is the one place a reader learns which
// systems jjf writes DDL for, and it is therefore the sentence they trust
// most.
func dialectNames() string {
	names := make([]string, 0, len(dialects()))
	for _, entry := range dialects() {
		names = append(names, entry.name)
	}
	return strings.Join(names, ", ")
}
