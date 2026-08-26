package ddl

import (
	"fmt"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// Accept reports why doc cannot be rendered as DDL, and returns nil when it
// can.
//
// This is the one exporter in jjf that refuses its input, and the asymmetry is
// deliberate rather than an inconsistency waiting to be tidied away. A document
// that contradicts itself still makes a useful workbook and a useful diagram -
// internal/export/svg even draws a foreign key with no target as a dashed stub
// on purpose, and says in its own package comment that it reports nothing,
// ever. DDL a database rejects is worth nothing at all, so here the same
// contradictions are a refusal. "jjf validate" is where they are reported for
// every other purpose.
//
// The dialect lookup comes first, before a single finding is computed: a
// MariaDB document has no business being lectured about PostgreSQL's index
// namespace, and the remedy is a different one - change the target or change
// the tool, not the contents of the document. MariaDB is the live example
// rather than a hypothetical one, and it stays refused deliberately: it has no
// importer and no live-server leg, so it could only ship on golden files,
// which is what design/ddl-export.md's gate exists to forbid.
//
// This is also the only place in jjf that reads database.dbms, and it reads it
// strictly. An absent value is an error rather than a default: guessing
// PostgreSQL for a document that never said so would generate SQL for a
// database nobody named.
//
// Every error carries exitcode.InvalidInput, because what is wrong is the input
// document rather than the writing of the file, and cmd/jjf must be able to
// tell "fix your document" from "fix your runner", which is what
// exitcode.OutputFailed means.
func Accept(doc *model.Document) error {
	d, ok := lookupDialect(doc.Database.DBMS)
	if !ok {
		return exitcode.Wrap(exitcode.InvalidInput, "", errNoDialect(doc.Database.DBMS))
	}

	// Check owns the call to check.Document; Accept must never make it too, or
	// an export would walk the document twice for one answer.
	if findings := Check(doc); len(findings) > 0 {
		return exitcode.Wrap(exitcode.InvalidInput, "", &RefusedError{Findings: findings, Dialect: d.name})
	}
	return nil
}

// errNoDialect explains that jjf writes no DDL for the target the document
// names, in the two shapes the two mistakes deserve.
//
// The supported list is generated from dialects() rather than spelled out,
// because this is the one sentence in which a reader learns what jjf can do
// and it must not be able to go stale - the drift argument
// cmd/jjf/export.go's exportUsage makes for the format list, applied to the
// message that matters most.
//
// An absent value gets its own sentence naming the values to write, because a
// list of what is supported does not tell an author what the fix looks like.
// That list is generated too, from the same table, so the sentence cannot come
// to offer a dialect jjf has stopped writing or omit one it has started.
func errNoDialect(d model.DBMS) error {
	if d == "" {
		return fmt.Errorf(`ddl export needs the document to name its target; add "dbms": %s to "database"`, dialectValues())
	}
	return fmt.Errorf("ddl export supports %s; this document names %q", dialectNames(), d)
}

// RefusedError carries every reason a document was refused.
//
// It is a type rather than a formatted string because cmd/jjf recovers the
// findings with errors.As and prints them one per line, prefixed with the input
// path so that an editor or a CI annotator can locate them - the same shape
// "jjf validate" already prints, and the same recovery *schema.InvalidDocumentError
// gets in cmd/jjf/main.go. Only cmd/jjf knows that path, so only cmd/jjf can
// write those lines.
type RefusedError struct {
	Findings []check.Finding
	// Dialect is the display name of the dialect that refused, so that the
	// summary can say which one. It travels in the error rather than being
	// looked up again by the caller: cmd/jjf cannot see the dialect table, and
	// asking it to ask a second question about a document it already has an
	// error for is how the sentence would come to be written in two packages
	// and drift.
	Dialect string
}

// Error returns the one-line summary. The itemised lines are the caller's.
func (e *RefusedError) Error() string {
	return fmt.Sprintf("%d problem(s) prevent %s DDL generation", len(e.Findings), e.Dialect)
}

// Check reports every reason doc cannot become DDL: first everything
// check.Document reports about the document contradicting itself, in its order,
// then the things that are true of the target system rather than of the
// document, in document order. It returns nil when there is nothing to report.
//
// One list of one type, because the reader is getting one answer to one
// question - why was this export refused - and every entry in it is a reason
// the DDL would be rejected or would not match the document. The one real
// difference, that "jjf validate" reports the first group and not the second,
// is carried by the messages themselves: the dialect's own findings name their
// database outright, so a reader who then runs "jjf validate" and is told the
// document is fine can see why.
//
// The two groups are not interleaved. They answer different questions and
// merging them would mean either re-walking the document inside internal/check
// or exporting its walk. Both halves are deterministic and neither sorts.
//
// For a dbms value no dialect claims - absent, or one of the four jjf does not
// write - the answer is check.Document's findings alone and nothing is added.
// Falling back to some dialect's rules would lecture a MariaDB document about
// PostgreSQL's namespace, which is exactly what Accept's lookup exists to
// prevent, and Accept refuses such a document before anything is written
// anyway. The honest answer to "what is wrong with this document" when no
// target has been chosen is what is wrong with the document.
//
// Nothing here decides what a finding costs - that is Accept's job, and the
// doctrine internal/check states in its own package comment. Check stays pure
// so that a test, and a round-trip harness, can ask what is wrong without
// asking for SQL.
//
// It is total: doc need not have passed JSON Schema validation first, because
// check.Document makes the same promise and a caller may reasonably hold this
// one to it. An empty name, a table with no columns and a nil primary key are
// all reachable states of the decoded model.
func Check(doc *model.Document) []check.Finding {
	findings := check.Document(doc)
	if d, ok := lookupDialect(doc.Database.DBMS); ok {
		findings = append(findings, d.check(doc)...)
	}
	return findings
}

// tableLabel and columnLabel name an object in a finding, in the two shapes
// this package needs. Both dialects' checks use them, so that two findings
// about one table read alike whichever database they are about.
//
// They are re-implementations of internal/check/check.go's tableLabel and
// constraintLabel, which are unexported there. Exporting them would widen that
// package's API for a formatting detail; the precedent for copying instead is
// internal/check's own sameColumnSet, re-implemented rather than shared because
// the requirements differed. The two must keep reading alike, so a change to
// either is a change to both.
func tableLabel(name string) string { return "table " + name }

func columnLabel(name, table string) string { return "column " + name + " on table " + table }
