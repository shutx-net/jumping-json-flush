package ddl

import (
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// TestEveryDialectIsFullyPopulated holds the table to being a table. A missing
// write would be a nil call at the first export of that dbms value and a
// missing check would silently drop every precondition the dialect owns, both
// of which are a great deal easier to find here than in a golden diff.
func TestEveryDialectIsFullyPopulated(t *testing.T) {
	for _, d := range dialects() {
		t.Run(string(d.dbms), func(t *testing.T) {
			if d.dbms == "" {
				t.Error("the entry claims no dbms value, so lookupDialect can never reach it")
			}
			if d.name == "" {
				t.Error("the entry has no display name, so its refusals cannot say who refused")
			}
			if d.check == nil {
				t.Error("the entry has no check")
			}
			if d.write == nil {
				t.Error("the entry has no writer")
			}
		})
	}
}

// TestDialectDBMSValuesAreDistinct makes lookupDialect's correctness
// independent of the table's order: with two entries claiming one dbms value
// the walk would return whichever came first, which is a fact about the
// literal rather than about the dialects.
func TestDialectDBMSValuesAreDistinct(t *testing.T) {
	// seen is looked up and assigned and never ranged over; the order of the
	// report comes from the slice walk.
	seen := make(map[model.DBMS]string)
	for _, d := range dialects() {
		if prior, taken := seen[d.dbms]; taken {
			t.Errorf("%s claims the dbms value %q, which %s already claims", d.name, d.dbms, prior)
			continue
		}
		seen[d.dbms] = d.name
	}
}

// TestLookupDialectIsExact pins the comparison. The schema's dbms enum is what
// makes an exact comparison safe, so being tolerant here would be accepting
// spellings no validated document can carry - and would make "Postgres" mean
// something in this package and nothing in every other.
func TestLookupDialectIsExact(t *testing.T) {
	for _, dbms := range []model.DBMS{model.DBMSPostgreSQL, model.DBMSMySQL} {
		if _, ok := lookupDialect(dbms); !ok {
			t.Errorf("lookupDialect does not resolve %q, which the table claims", dbms)
		}
	}
	for _, spelling := range []model.DBMS{"postgresql", "Postgres", "POSTGRESQL", " PostgreSQL", "mysql", "MYSQL", "My SQL", ""} {
		if _, ok := lookupDialect(spelling); ok {
			t.Errorf("lookupDialect resolved %q, which is not a dbms value the schema permits", spelling)
		}
	}
}

// TestDialectNamesListsEveryEntry keeps the refusal message honest: it is the
// one sentence in which a reader learns what jjf writes DDL for.
func TestDialectNamesListsEveryEntry(t *testing.T) {
	names := dialectNames()
	at := -1
	for _, d := range dialects() {
		i := strings.Index(names, d.name)
		if i < 0 {
			t.Errorf("dialectNames() = %q, which does not name %s", names, d.name)
			continue
		}
		if i <= at {
			t.Errorf("dialectNames() = %q, which does not name %s in table order", names, d.name)
		}
		at = i
	}
}

// TestUnsupportedDBMSValuesAreRefused spells the four out rather than deriving
// them, so that adding a dialect has to delete a line here deliberately. A
// derived list would silently agree with whatever the table said and would
// assert nothing.
//
// MariaDB heads the list on purpose. It is the value a reader will most expect
// to find supported, because it takes the same DDL MySQL does - and it is
// refused because it has no importer and no live-server leg, so it could only
// ship on golden files, which is what design/ddl-export.md's gate forbids.
func TestUnsupportedDBMSValuesAreRefused(t *testing.T) {
	for _, dbms := range []model.DBMS{
		model.DBMSMariaDB,
		model.DBMSSQLite,
		model.DBMSOracle,
		model.DBMSSQLServer,
	} {
		t.Run(string(dbms), func(t *testing.T) {
			if _, ok := lookupDialect(dbms); ok {
				t.Errorf("lookupDialect resolved %q; if a dialect for it has been added, this line is what has to be removed", dbms)
			}
		})
	}
}

// TestDialectValuesReadsAsASentence pins the other half of the message an
// author with no dbms gets. dialectNames is a list of what jjf can do;
// dialectValues is what to type, so it quotes each value as the JSON it is and
// joins the last two with "or" rather than a comma - a document may name one
// target, not both.
func TestDialectValuesReadsAsASentence(t *testing.T) {
	got := dialectValues()
	for _, d := range dialects() {
		if !strings.Contains(got, `"`+d.name+`"`) {
			t.Errorf("dialectValues() = %q, which does not offer %q as a JSON value", got, d.name)
		}
	}
	if len(dialects()) > 1 && !strings.Contains(got, " or ") {
		t.Errorf("dialectValues() = %q, which does not say that only one may be chosen", got)
	}
}
