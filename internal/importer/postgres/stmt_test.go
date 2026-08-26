package postgres

import (
	"slices"
	"strings"
	"testing"
)

// This file holds the statement-level tests for the column definitions and
// table constraints of a CREATE TABLE, next to their MySQL counterparts in
// internal/importer/mysql/stmt_test.go. Every input is a Go string literal for
// the reason that file states: a parser test whose input is a literal fails
// legibly, because the bytes that broke are in the same file as the assertion
// about them.
//
// What almost every case here has in common is worth saying once. pg_dump does
// not write a column-level PRIMARY KEY, UNIQUE or REFERENCES, does not write an
// explicit NULL, and does not write COLLATE for the schemas the captures under
// testdata/dump were taken from - it writes every key as its own ALTER TABLE
// ONLY ... ADD CONSTRAINT. These forms are here because the parser accepts them
// on purpose, so that hand-written SQL, a hand-edited dump and output from a
// tool that is not pg_dump still import. That tolerance is a promise the parser
// makes to input no fixture can contain, which is exactly the kind of promise
// only a named test can hold it to.

// messages renders diagnostics as their strings, which is what a test asserts
// on: the line and the sentence together are the whole of what a reader gets.
// Copied from the MySQL package's parse_test.go so that the two importers' test
// files read alike.
func messages(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

// TestInlineColumnConstraintsBecomeTableConstraints pins the folding that lets
// build.go resolve one shape instead of two: a key written on a column arrives
// in t.Constraints looking exactly like a key written as a table element.
//
// The failure this guards against is silent. If the PRIMARY KEY arm stopped
// folding, jjf would emit a document with no primary key at all, that document
// would still satisfy the JSON Schema, and not one warning would be printed.
//
// The fourth column is the point of the last assertion. `pending` survives from
// one option to the next within a column definition, and the three arms that
// consume it clear it by hand; an arm added later that forgot to would name the
// next unnamed constraint after the previous one's CONSTRAINT clause, and
// nothing else in the tree would notice.
func TestInlineColumnConstraintsBecomeTableConstraints(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  id integer CONSTRAINT t_pk PRIMARY KEY,\n"+
		"  code text CONSTRAINT t_code_uq UNIQUE,\n"+
		"  owner integer CONSTRAINT t_owner_fk REFERENCES public.u(id) ON DELETE CASCADE,\n"+
		"  alt text UNIQUE\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	if len(dump.Tables) != 1 {
		t.Fatalf("tables got = %v, want 1", len(dump.Tables))
	}
	table := dump.Tables[0]

	var names []string
	for _, col := range table.Columns {
		names = append(names, col.Name)
	}
	if want := []string{"id", "code", "owner", "alt"}; !slices.Equal(names, want) {
		t.Errorf("columns got = %v, want %v", names, want)
	}

	kinds := []constraintKind{constraintPrimaryKey, constraintUnique, constraintForeign, constraintUnique}
	if len(table.Constraints) != len(kinds) {
		t.Fatalf("constraints got = %+v, want %v", table.Constraints, len(kinds))
	}
	for i, want := range kinds {
		if got := table.Constraints[i].Kind; got != want {
			t.Errorf("constraint %d kind got = %v, want %v", i, got, want)
		}
	}

	if pk := table.Constraints[0]; pk.Name != "t_pk" || !slices.Equal(pk.Columns, []string{"id"}) ||
		pk.Line != 2 || pk.NameLine != 2 {
		t.Errorf("primary key got = %+v, want t_pk over id on line 2", pk)
	}
	if uq := table.Constraints[1]; uq.Name != "t_code_uq" || !slices.Equal(uq.Columns, []string{"code"}) || uq.Line != 3 {
		t.Errorf("unique key got = %+v, want t_code_uq over code on line 3", uq)
	}
	if fk := table.Constraints[2]; fk.Name != "t_owner_fk" || !slices.Equal(fk.Columns, []string{"owner"}) ||
		fk.RefTable.Schema != "public" || fk.RefTable.Name != "u" ||
		!slices.Equal(fk.RefColumns, []string{"id"}) || fk.OnDelete != "CASCADE" || fk.OnUpdate != "" {
		t.Errorf("foreign key got = %+v, want t_owner_fk (owner) -> public.u(id) ON DELETE CASCADE", fk)
	}
	if alt := table.Constraints[3]; alt.Name != "" {
		t.Errorf("the unnamed unique key got name %q, want it empty: it must not inherit the previous CONSTRAINT clause", alt.Name)
	}
}

// TestAnExplicitNULLIsTheDefaultAlready is worth less than its neighbours and
// says so. The arm it covers consumes a word the default arm would consume
// anyway, so what is asserted is not that the arm is load-bearing but that the
// tolerance holds: a tool that spells out the nullability jjf assumes must not
// end up setting NOT NULL, and the word must not be mistaken for the start of
// something else.
func TestAnExplicitNULLIsTheDefaultAlready(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  a integer NULL,\n"+
		"  b integer NOT NULL\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	if len(table.Columns) != 2 {
		t.Fatalf("columns got = %+v, want 2", table.Columns)
	}
	if table.Columns[0].NotNull {
		t.Error("an explicit NULL set NotNull on the column, want it left false")
	}
	if !table.Columns[1].NotNull {
		t.Error("NOT NULL got = false, want true")
	}
	if len(table.Constraints) != 0 {
		t.Errorf("constraints got = %+v, want none", table.Constraints)
	}
}

// TestAnUnrecognisedColumnOptionIsSkippedNotRejected holds stmt.go's claim that
// skipping an unknown column option rather than refusing it is what keeps a
// dump from a newer PostgreSQL working. Nothing else in the tree states it, and
// a future maintainer tightening the parser would find no test in the way.
//
// The unknown options are written BEFORE the DEFAULT on purpose, and that is
// the bounded cost of the tolerance: an unknown word is stepped over one token
// at a time, so an unknown option written after a DEFAULT would be swallowed
// into the default text - startsColumnConstraint is the single list that says
// where a default ends, and an unknown word is by definition not in it. An
// unknown option taking a parenthesised argument would likewise be walked into
// rather than over.
func TestAnUnrecognisedColumnOptionIsSkippedNotRejected(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  a integer FUTURE_OPTION 7 NOT NULL DEFAULT 0,\n"+
		"  b integer ANOTHER_ONE NOT NULL\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	cols := dump.Tables[0].Columns
	if len(cols) != 2 {
		t.Fatalf("columns got = %+v, want 2", cols)
	}
	if !cols[0].NotNull || cols[0].Default != "0" {
		t.Errorf("first column got = %+v, want NOT NULL with a default of 0", cols[0])
	}
	if !cols[1].NotNull {
		t.Errorf("second column got = %+v, want NOT NULL", cols[1])
	}
}

// TestACollatedColumnKeepsItsTypeAndNullability covers the one form in this
// file that pg_dump does emit, for a schema the captures happen not to have: a
// column whose collation differs from the database default comes back as
// `col text COLLATE pg_catalog."C"`. The collation itself has nowhere to go in
// a design document, so the assertion is that dropping it costs the column
// nothing else - the qualified name whose second part is a quoted identifier is
// consumed whole, and the NOT NULL after it is still read.
func TestACollatedColumnKeepsItsTypeAndNullability(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  a text COLLATE pg_catalog.\"C\" NOT NULL\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	cols := dump.Tables[0].Columns
	if len(cols) != 1 {
		t.Fatalf("columns got = %+v, want 1", cols)
	}
	if got := strings.Join(cols[0].Type.Words, " "); got != "text" {
		t.Errorf("type got = %q, want %q", got, "text")
	}
	if !cols[0].NotNull {
		t.Errorf("column got = %+v, want NOT NULL", cols[0])
	}
}

// TestADefaultEndsWhereCOLLATEBegins pins a coupling rather than an arm, and it
// is the case in this file with the longest reach.
//
// Two places have to agree about COLLATE: parseColumnDefinition consumes it as
// a column option, and startsColumnConstraint lists `collate` among the words
// that end a DEFAULT expression. If either moved without the other, the
// document would carry a default of `'x' COLLATE pg_catalog."C"` - and
// internal/export/ddl copies a default into its output verbatim, so the round
// trip would then emit a script PostgreSQL refuses. Nothing would warn.
func TestADefaultEndsWhereCOLLATEBegins(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  a text DEFAULT 'x' COLLATE pg_catalog.\"C\" NOT NULL\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	col := dump.Tables[0].Columns[0]
	if !col.HasDefault || col.Default != "'x'" {
		t.Errorf("default got = %q (present = %v), want exactly %q", col.Default, col.HasDefault, "'x'")
	}
	if !col.NotNull {
		t.Errorf("column got = %+v, want NOT NULL", col)
	}
}

// TestColumnOptionsTheFormatCannotHold covers the column and table options that
// a design document has no field for. The published contract for all of them is
// the same and is stated in docs/usage.md: one line on standard error naming the
// dump line, and the surrounding table or constraint still imported. Each row
// therefore asserts the line exactly, the distinctive clause of the message, and
// what survived - a message with nothing behind it is the half of the contract
// that is easy to keep by accident.
//
// One diagnostic that belongs to this group is deliberately absent. The
// standalone `NO INHERIT` column option at stmt.go:287-288 is reachable only
// from SQL PostgreSQL itself rejects: its grammar allows NO INHERIT after a
// CHECK, and the check arm already consumes it there. Pinning a message no
// input a user could hold in a file can reach would only make it harder to
// delete, so it is left unpinned on purpose.
func TestColumnOptionsTheFormatCannotHold(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// wantMessage empty means the input must produce no warning at all.
		wantMessage string
		wantLine    int
		check       func(t *testing.T, table pgTable)
	}{
		{
			name:        "deferrable",
			src:         "CREATE TABLE public.t (\n  a integer REFERENCES public.u(id) DEFERRABLE INITIALLY DEFERRED\n);",
			wantMessage: "column public.t.a: deferrable constraint is imported as an ordinary constraint",
			wantLine:    2,
			check:       oneForeignKeyToU,
		},
		{
			// NOT DEFERRABLE restates PostgreSQL's default and changes nothing
			// about the import, which makes it the arm most likely to be
			// dropped by someone tidying the case list. This row is the only
			// thing that would notice.
			name:        "not deferrable",
			src:         "CREATE TABLE public.t (\n  a integer REFERENCES public.u(id) NOT DEFERRABLE\n);",
			wantMessage: "column public.t.a: deferrable constraint is imported as an ordinary constraint",
			wantLine:    2,
			check:       oneForeignKeyToU,
		},
		{
			// pg_dump 15 and later writes this for a unique constraint declared
			// that way, so a real dump reaches it.
			name:        "column level NULLS NOT DISTINCT",
			src:         "CREATE TABLE public.t (\n  a integer UNIQUE NULLS NOT DISTINCT\n);",
			wantMessage: "column public.t.a: NULLS NOT DISTINCT is not imported",
			wantLine:    2,
			check:       oneUniqueKeyOverA,
		},
		{
			// The table-constraint sibling of the row above. The pair is what
			// shows the two sites say the same sentence; either alone would let
			// them drift.
			name:        "table level NULLS NOT DISTINCT",
			src:         "CREATE TABLE public.t (\n  a integer,\n  CONSTRAINT t_a_uq UNIQUE NULLS NOT DISTINCT (a)\n);",
			wantMessage: "constraint t_a_uq on table public.t: NULLS NOT DISTINCT is not imported",
			wantLine:    3,
			check: func(t *testing.T, table pgTable) {
				oneUniqueKeyOverA(t, table)
				if got := table.Constraints[0].Name; got != "t_a_uq" {
					t.Errorf("constraint name got = %q, want %q", got, "t_a_uq")
				}
			},
		},
		{
			// The negative half, and the reason the two NULLS rows are in the
			// same table. NULLS DISTINCT is PostgreSQL's own default and is
			// accepted without a word; a change that warned about both
			// spellings would be caught here and nowhere else.
			name:  "NULLS DISTINCT is silent",
			src:   "CREATE TABLE public.t (\n  a integer UNIQUE NULLS DISTINCT\n);",
			check: oneUniqueKeyOverA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if tt.wantMessage == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", messages(warnings))
				}
			} else {
				// Exactly one, even where the input carries two words that
				// could each raise it: the deferrable rows are what pins
				// parseColumnDefinition's warnedDeferrable flag.
				if len(warnings) != 1 {
					t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
				}
				if !strings.Contains(warnings[0].Message, tt.wantMessage) {
					t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, tt.wantMessage)
				}
				if warnings[0].Line != tt.wantLine {
					t.Errorf("warning line got = %v, want %v", warnings[0].Line, tt.wantLine)
				}
			}
			if len(dump.Tables) != 1 {
				t.Fatalf("tables got = %v, want 1", len(dump.Tables))
			}
			tt.check(t, dump.Tables[0])
		})
	}
}

// oneForeignKeyToU asserts that column a survived with the foreign key written
// on it intact, which is the half of the tier-2 contract a warning alone does
// not show.
func oneForeignKeyToU(t *testing.T, table pgTable) {
	t.Helper()
	if len(table.Columns) != 1 || table.Columns[0].Name != "a" {
		t.Fatalf("columns got = %+v, want one column a", table.Columns)
	}
	if len(table.Constraints) != 1 {
		t.Fatalf("constraints got = %+v, want 1", table.Constraints)
	}
	fk := table.Constraints[0]
	if fk.Kind != constraintForeign || fk.RefTable.Name != "u" || !slices.Equal(fk.RefColumns, []string{"id"}) {
		t.Errorf("constraint got = %+v, want a foreign key to public.u(id)", fk)
	}
}

// oneUniqueKeyOverA asserts the unique key the NULLS clause was written on is
// still there over the column it named.
func oneUniqueKeyOverA(t *testing.T, table pgTable) {
	t.Helper()
	if len(table.Constraints) != 1 {
		t.Fatalf("constraints got = %+v, want 1", table.Constraints)
	}
	c := table.Constraints[0]
	if c.Kind != constraintUnique || !slices.Equal(c.Columns, []string{"a"}) {
		t.Errorf("constraint got = %+v, want a unique key over a", c)
	}
}

// TestAColumnCalledExcludeIsAColumn and its sibling below are one claim in two
// halves, and only the two together pin it. isConstraintStart can tell a table
// constraint from a column definition by its first word alone for every word
// but one, because the rest are reserved and a column named one of them would
// have to be quoted - and a quoted name never reaches wordAt. EXCLUDE is not
// reserved, so it is decided by what follows it instead.
//
// The guarantee is exactly this narrow: a column called exclude parses as a
// column unless its very next token is USING or an opening parenthesis, which
// for a column definition would mean a type called `using` or an immediately
// parenthesised type. Neither is legal, so nothing legal is lost.
func TestAColumnCalledExcludeIsAColumn(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  exclude integer NOT NULL,\n"+
		"  b text\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	var names []string
	for _, col := range table.Columns {
		names = append(names, col.Name)
	}
	if want := []string{"exclude", "b"}; !slices.Equal(names, want) {
		t.Fatalf("columns got = %v, want %v", names, want)
	}
	if !table.Columns[0].NotNull {
		t.Errorf("column exclude got = %+v, want NOT NULL", table.Columns[0])
	}
	if len(table.Constraints) != 0 {
		t.Errorf("constraints got = %+v, want none: exclude was a column here", table.Constraints)
	}
}

// TestAnUnnamedExclusionConstraintIsWarnedAndDropped is the other half. The
// named form - ADD CONSTRAINT t_ex EXCLUDE ... - enters isConstraintStart
// through the `constraint` case and is already covered; these two spellings are
// the ones the lookahead itself has to catch, and a test of the named form
// alone would still pass with `case "exclude": return true`, which is the bug
// the lookahead exists to prevent.
//
// The second sub-case drops USING, which is legal - the access method defaults
// - and reaches the lookahead's other alternative, the opening parenthesis.
func TestAnUnnamedExclusionConstraintIsWarnedAndDropped(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "with an access method", src: "ALTER TABLE ONLY public.t ADD EXCLUDE USING gist (a WITH =);"},
		{name: "without one", src: "ALTER TABLE ONLY public.t ADD EXCLUDE (a WITH =);"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
			}
			if want := "constraint on table public.t: exclusion constraint is not imported"; !strings.Contains(warnings[0].Message, want) {
				t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
			}
			if warnings[0].Line != 1 {
				t.Errorf("warning line got = %v, want 1", warnings[0].Line)
			}
			// Dropped, not recorded under an empty name: a constraint with no
			// name and no columns reaching build.go would be reported a second
			// time there, about a thing the user was already told about.
			if len(dump.Constraints) != 0 {
				t.Errorf("constraints got = %+v, want none", dump.Constraints)
			}
		})
	}
}

// TestTypeArgumentsAreReadWhole pins the depth tracking in typeArgs: a
// parenthesis inside an argument must not be mistaken for the end of the
// argument list. The input is one PostgreSQL would reject, and it is here
// because of what happens AFTER the list is read - the argument text is handed
// to argInt, which cannot make a number of it, drops the precision with a
// warning and leaves the column standing. A parser that ended the list at the
// inner parenthesis would instead fail the whole statement.
//
// The joined-with-spaces argument text is what typeArgs really produces; it is
// asserted rather than tidied because that text is what the warning quotes back
// to the user.
func TestTypeArgumentsAreReadWhole(t *testing.T) {
	typ := parseType(t, "numeric((10),2)")
	if want := []string{"( 10 )", "2"}; !slices.Equal(typ.Args, want) {
		t.Errorf("type arguments got = %q, want %q", typ.Args, want)
	}

	var d diagList
	ct, err := normalizeType(typ, "users.c", &d)
	if err != nil {
		t.Fatalf("normalizeType returned error %v, want no error", err)
	}
	if ct.Name != "NUMERIC" {
		t.Errorf("type name got = %q, want %q", ct.Name, "NUMERIC")
	}
	if ct.Precision != nil || ct.Scale != nil {
		t.Errorf("precision/scale got = %v/%v, want both dropped", ct.Precision, ct.Scale)
	}
	if warnings := messages(d.all()); len(warnings) != 1 ||
		!strings.Contains(warnings[0], `precision "( 10 )" is not a number`) {
		t.Errorf("warnings got = %v, want one naming the unreadable precision", warnings)
	}
}

// TestDiagnosticsRenderTheirLine is the twin of the MySQL package's test of the
// same name. Both importers publish the same contract on their Diagnostic: a
// zero line means the warning is about the dump as a whole and prints without a
// line prefix.
//
// No importer calls warnf with a zero line today. The branch is a promise the
// type makes in its own comment - "Pass 0 when no line applies" - to whoever
// writes the next resolve-time warning that has no place in the file to point
// at, and this test is the only thing holding it.
//
// The nil half is a separate promise: all() on an empty list returns nil rather
// than an empty slice, which is what lets a caller treat `warnings == nil` as
// "this dump imported cleanly".
func TestDiagnosticsRenderTheirLine(t *testing.T) {
	var d diagList
	d.warnf(12, "table %s: partitioning is not imported", "parts")
	d.warnf(0, "this dump names no database")

	want := []string{
		"line 12: table parts: partitioning is not imported",
		"this dump names no database",
	}
	if got := messages(d.all()); !slices.Equal(got, want) {
		t.Errorf("diagnostics got = %v, want %v", got, want)
	}
	if got := (&diagList{}).all(); got != nil {
		t.Errorf("an empty diagList rendered %v, want nil", got)
	}
}

// TestConstraintNameLineIsWhereTheNameWasWritten reads a column definition
// spread over four lines, which is the only shape where the constraint's line
// and the statement's line differ. These numbers reach the user as the
// `shop.sql:4: warning:` prefix of a resolve-time complaint, so being wrong
// about them costs a reader the ability to find the clause being complained
// about.
//
// An inline constraint is reported at the keyword that CREATED it - line 4,
// where PRIMARY KEY is written - rather than at the CONSTRAINT clause that
// named it on line 3, because parseColumnDefinition re-reads the current line
// at the top of every option. A table constraint differs here: parseTableConstraint
// sets NameLine to the line the name itself is on. The two are inconsistent,
// which matters only for a name that cannot be written to a document, and this
// test records which is which rather than arguing for a change.
func TestConstraintNameLineIsWhereTheNameWasWritten(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  id integer\n"+
		"    CONSTRAINT t_pk\n"+
		"    PRIMARY KEY\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	if len(table.Constraints) != 1 {
		t.Fatalf("constraints got = %+v, want 1", table.Constraints)
	}
	c := table.Constraints[0]
	if c.Name != "t_pk" || c.Line != 4 || c.NameLine != 4 {
		t.Errorf("constraint got = %+v, want t_pk with Line and NameLine 4", c)
	}
	if table.Columns[0].Line != 2 {
		t.Errorf("column line got = %v, want 2", table.Columns[0].Line)
	}
}
