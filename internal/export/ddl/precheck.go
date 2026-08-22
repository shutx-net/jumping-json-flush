package ddl

import (
	"fmt"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// Accept reports why doc cannot be rendered as PostgreSQL DDL, and returns nil
// when it can.
//
// This is the one exporter in jjf that refuses its input, and the asymmetry is
// deliberate rather than an inconsistency waiting to be tidied away. A document
// that contradicts itself still makes a useful workbook and a useful diagram -
// internal/export/dot even draws a foreign key with no target as a dashed stub
// on purpose, and says in its own package comment that it reports nothing,
// ever. DDL a database rejects is worth nothing at all, so here the same
// contradictions are a refusal. "jjf validate" is where they are reported for
// every other purpose.
//
// The dbms guard comes first, before a single finding is computed: a MySQL
// document has no business being lectured about PostgreSQL's index namespace,
// and the remedy is a different one - change the target or change the tool,
// not the contents of the document.
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
	switch doc.Database.DBMS {
	case model.DBMSPostgreSQL:
	case "":
		return exitcode.Wrap(exitcode.InvalidInput, "",
			fmt.Errorf(`ddl export needs the document to name its target; add "dbms": "PostgreSQL" to "database"`))
	default:
		return exitcode.Wrap(exitcode.InvalidInput, "",
			fmt.Errorf("ddl export supports PostgreSQL only; this document names %q", doc.Database.DBMS))
	}

	if findings := Check(doc); len(findings) > 0 {
		return exitcode.Wrap(exitcode.InvalidInput, "", &RefusedError{Findings: findings})
	}
	return nil
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
}

// Error returns the one-line summary. The itemised lines are the caller's.
func (e *RefusedError) Error() string {
	return fmt.Sprintf("%d problem(s) prevent PostgreSQL DDL generation", len(e.Findings))
}

// Check reports every reason doc cannot become PostgreSQL DDL: first everything
// check.Document reports about the document contradicting itself, in its order,
// then the things that are true of PostgreSQL rather than of the document, in
// document order. It returns nil when there is nothing to report.
//
// One list of one type, because the reader is getting one answer to one
// question - why was this export refused - and every entry in it is a reason
// the DDL would be rejected or would not match the document. The one real
// difference, that "jjf validate" reports the first group and not the second,
// is carried by the messages themselves: the PostgreSQL ones name PostgreSQL
// outright, so a reader who then runs "jjf validate" and is told the document
// is fine can see why.
//
// The two groups are not interleaved. They answer different questions and
// merging them would mean either re-walking the document inside internal/check
// or exporting its walk. Both halves are deterministic and neither sorts.
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

	// claimed maps a name in the schema-wide namespace to a description of
	// whatever claimed it first, so that a collision can say what it collided
	// with. The map is only ever LOOKED UP and assigned, NEVER ranged over:
	// ranging would make the order of the findings depend on Go's map
	// iteration and destroy the determinism the goldens and the CLI output
	// rest on. The order comes from the slice walk below and from nowhere
	// else.
	claimed := make(map[string]string, len(doc.Tables))
	// reported remembers the names already reported, for the same reason
	// internal/check's usedNames does: three tables sharing an index name is
	// one mistake to fix, not two findings.
	reported := make(map[string]bool)

	// claim records name as taken by owner. It returns what already held the
	// name and whether this collision is worth reporting: a name already
	// reported once is not reported again, for the reason internal/check's
	// usedNames gives - three tables sharing an index name is one mistake to
	// fix, not two findings.
	claim := func(name, owner string) (prior string, collides bool) {
		if name == "" {
			// The schema makes primaryKey.name and uniqueKeys[].name
			// optional, so several unnamed constraints in one document are
			// ordinary rather than a collision. tables[].name and
			// indexes[].name are required, but the model can still hold ""
			// for a document that was never validated, and this function has
			// to stay total over those.
			return "", false
		}
		prior, taken := claimed[name]
		if !taken {
			claimed[name] = owner
			return "", false
		}
		if reported[name] {
			return "", false
		}
		reported[name] = true
		return prior, true
	}

	// namespaceRule is the clause every P1 finding ends with. It names
	// PostgreSQL outright, which is what lets these findings share one list
	// and one prefix with internal/check's without confusing a reader who then
	// runs "jjf validate" and is told the document is fine.
	const namespaceRule = "; PostgreSQL puts tables, indexes and the indexes behind PRIMARY KEY and UNIQUE in one namespace per schema"

	for i := range doc.Tables {
		t := &doc.Tables[i]
		label := tableLabel(t.Name)

		// P1 - a table name is in the namespace too. internal/check
		// deliberately does not report duplicate table names, so without this
		// a document naming two tables "orders" would emit CREATE TABLE twice
		// and PostgreSQL would reject the script.
		if prior, collides := claim(t.Name, "table "+t.Name); collides {
			// Two tables of one name is the collision a reader will hit most
			// often and it deserves to be said plainly; anything else that
			// took the name is named instead.
			if prior == "table "+t.Name {
				findings = append(findings, check.Finding{
					Where:   label,
					Message: fmt.Sprintf("is the second table this document calls %q; PostgreSQL cannot create two tables of one name in a schema", t.Name),
				})
			} else {
				findings = append(findings, check.Finding{
					Where:   label,
					Message: fmt.Sprintf("is called %q, a name already used by %s%s", t.Name, prior, namespaceRule),
				})
			}
		}

		for j := range t.Columns {
			c := &t.Columns[j]
			if !c.AutoIncrement {
				continue
			}
			// P2 - PostgreSQL refuses "both default and identity specified
			// for column". autoIncrement renders as GENERATED BY DEFAULT AS
			// IDENTITY and default renders verbatim, so a column carrying
			// both would emit a statement the database rejects.
			if c.Default != nil {
				findings = append(findings, check.Finding{
					Where:   columnLabel(c.Name, t.Name),
					Message: "is autoIncrement and also declares a default; PostgreSQL refuses a column that is both an identity column and has a DEFAULT",
				})
				// At most one finding comes out per column, the rule
				// internal/check's checkColumnDefault already follows: one
				// authoring mistake, one finding.
				continue
			}
			// P3 - the silent case. PostgreSQL accepts a nullable identity
			// column and makes it NOT NULL anyway, so the database and the
			// document would disagree without anything saying so. That is
			// C5's argument exactly; C5 only reaches the column when it is
			// also part of the primary key.
			if c.Nullable {
				findings = append(findings, check.Finding{
					Where:   columnLabel(c.Name, t.Name),
					Message: "is autoIncrement and declared nullable; PostgreSQL makes an identity column NOT NULL, so the database would not match the document",
				})
			}
		}

		// The rest of P1, in the order a reader meets the objects in the JSON.
		// Foreign keys are skipped entirely: their names live in pg_constraint,
		// which is unique per TABLE, so two tables may each carry a foreign key
		// called fk_parent and PostgreSQL accepts both. internal/check's C6
		// already covers the per-table case.
		if pk := t.PrimaryKey; pk != nil {
			if prior, collides := claim(pk.Name, "primary key "+pk.Name+" on table "+t.Name); collides {
				findings = append(findings, check.Finding{
					Where:   label,
					Message: fmt.Sprintf("declares primary key %q, a name already used by %s%s", pk.Name, prior, namespaceRule),
				})
			}
		}
		for _, uk := range t.UniqueKeys {
			if prior, collides := claim(uk.Name, "unique key "+uk.Name+" on table "+t.Name); collides {
				findings = append(findings, check.Finding{
					Where:   label,
					Message: fmt.Sprintf("declares unique key %q, a name already used by %s%s", uk.Name, prior, namespaceRule),
				})
			}
		}
		for _, ix := range t.Indexes {
			if prior, collides := claim(ix.Name, "index "+ix.Name+" on table "+t.Name); collides {
				findings = append(findings, check.Finding{
					Where:   label,
					Message: fmt.Sprintf("declares index %q, a name already used by %s%s", ix.Name, prior, namespaceRule),
				})
			}
		}
	}
	return findings
}

// tableLabel and columnLabel name an object in a finding, in the two shapes
// this package needs.
//
// They are re-implementations of internal/check/check.go's tableLabel and
// constraintLabel, which are unexported there. Exporting them would widen that
// package's API for a formatting detail; the precedent for copying instead is
// internal/check's own sameColumnSet, re-implemented rather than shared because
// the requirements differed. The two must keep reading alike, so a change to
// either is a change to both.
func tableLabel(name string) string { return "table " + name }

func columnLabel(name, table string) string { return "column " + name + " on table " + table }
