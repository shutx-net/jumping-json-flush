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
// TestAMultiWordTypeDoesNotSwallowTheRestOfTheTable is the PostgreSQL half of
// what #57 names, and it is the same shape as the MySQL one: parseTypeName
// stops after a word it does not know continues, the remaining words of the
// type fall to the attribute loop, and a skip that advances one token at a time
// walks onto the ")" of "(10)" - which the loop reads as the end of the column
// list. The rest of the table then disappears without a word.
//
// Every spelling below was accepted by PostgreSQL 16.13 before it was written
// here. "character varying" and "bit varying" already worked; these are the
// spellings of the same types that did not.
func TestAMultiWordTypeDoesNotSwallowTheRestOfTheTable(t *testing.T) {
	tests := []struct {
		name  string
		typ   string
		words string
		args  string
	}{
		{name: "national character varying", typ: "national character varying(10)", words: "national character varying", args: "10"},
		{name: "national character", typ: "national character(10)", words: "national character", args: "10"},
		{name: "national char", typ: "national char(10)", words: "national char", args: "10"},
		{name: "char varying", typ: "char varying(10)", words: "char varying", args: "10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
				"  j "+tt.typ+",\n"+
				"  k integer NOT NULL,\n"+
				"  PRIMARY KEY (k)\n);")
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
			tbl := dump.Tables[0]
			var names []string
			for _, c := range tbl.Columns {
				names = append(names, c.Name)
			}
			if !slices.Equal(names, []string{"j", "k"}) {
				t.Fatalf("columns got = %v, want [j k]", names)
			}
			if !tbl.Columns[1].NotNull {
				t.Errorf("column k got = %+v, want NOT NULL", tbl.Columns[1])
			}
			if len(tbl.Constraints) != 1 {
				t.Errorf("constraints got = %+v, want the primary key", tbl.Constraints)
			}
			typ := tbl.Columns[0].Type
			if got := strings.Join(typ.Words, " "); got != tt.words {
				t.Errorf("type words got = %q, want %q", got, tt.words)
			}
			if got := strings.Join(typ.Args, "|"); got != tt.args {
				t.Errorf("type args got = %q, want %q", got, tt.args)
			}
		})
	}
}

// TestADefaultMayCallAFunctionNamedByAnUnreservedWord is the second half of
// #57, and the one that reaches an ordinary pg_dump file.
//
// startsColumnConstraint claims in its own comment that every word it lists is
// reserved in PostgreSQL, so none can continue a DEFAULT expression. Asked,
// pg_get_keywords() disagrees about exactly two of the thirteen: "generated"
// and "no" are unreserved, so a function may be called either - and then
// pg_dump writes "DEFAULT public.no()" and the expression is cut at the word.
// What follows is cause A's swallowing, so the columns after it are lost too.
func TestADefaultMayCallAFunctionNamedByAnUnreservedWord(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "a function called no", expr: "public.no()"},
		{name: "a function called generated", expr: "public.generated()"},
		// Already correct today, because exprText only consults the stop
		// predicate at parenthesis depth zero. It is here so that a fix for
		// the two above cannot quietly break the case that worked.
		{name: "the same call inside parentheses", expr: "(1 + public.no())"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
				"  j integer DEFAULT "+tt.expr+",\n"+
				"  k integer NOT NULL,\n"+
				"  l text\n);")
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
			tbl := dump.Tables[0]
			var names []string
			for _, c := range tbl.Columns {
				names = append(names, c.Name)
			}
			if !slices.Equal(names, []string{"j", "k", "l"}) {
				t.Fatalf("columns got = %v, want [j k l]", names)
			}
			col := tbl.Columns[0]
			if !col.HasDefault || col.Default != tt.expr {
				t.Errorf("default got = %q (present = %v), want exactly %q", col.Default, col.HasDefault, tt.expr)
			}
			if !tbl.Columns[1].NotNull {
				t.Errorf("column k got = %+v, want NOT NULL", tbl.Columns[1])
			}
		})
	}
}

// TestNOINHERITAndGENERATEDStillEndADefault guards the other direction of the
// same fix: the two words earn their place in startsColumnConstraint through
// the clauses that really do follow a default, and gating them on what comes
// next must not stop those clauses from ending the expression.
func TestNOINHERITAndGENERATEDStillEndADefault(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (\n"+
		"  j integer DEFAULT 1 GENERATED BY DEFAULT AS IDENTITY,\n"+
		"  k integer NOT NULL\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	col := dump.Tables[0].Columns[0]
	if !col.HasDefault || col.Default != "1" {
		t.Errorf("default got = %q (present = %v), want exactly %q", col.Default, col.HasDefault, "1")
	}
	if !col.Identity {
		t.Errorf("column j got = %+v, want an identity column", col)
	}
}

// TestALikeElementDropsTheTable covers #53's LIKE case. PostgreSQL copies the
// definition of another table into this one, so the columns the table really
// has are not written in this statement at all.
//
// Measured on 16.13: "CREATE TABLE child (LIKE parent INCLUDING ALL)" gives
// child the columns id and note, copied from parent. What the parser produced
// instead was a table with one column called "like", of type PARENT - a column
// nothing in the database has, arrived at by reading a table element as a
// column definition because isConstraintStart did not know the word.
//
// Dropped rather than guessed, and the answer internal/importer/mysql already
// gives for the same SQL: the definition is not here to import, so inventing a
// table from what IS here describes something that does not exist.
func TestALikeElementDropsTheTable(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "LIKE as the only element",
			src:  "CREATE TABLE public.child (LIKE public.parent INCLUDING ALL);",
		},
		{
			// PostgreSQL allows it beside ordinary columns, which MySQL does
			// not. The table is still not one this statement describes.
			name: "LIKE beside a column of its own",
			src:  "CREATE TABLE public.child (LIKE public.parent, extra text);",
		},
		{
			name: "LIKE after a column of its own",
			src:  "CREATE TABLE public.child (extra text, LIKE public.parent);",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if msgs := messages(warnings); len(msgs) != 1 || !strings.Contains(msgs[0], "LIKE") {
				t.Fatalf("warnings got = %v, want exactly one about LIKE", msgs)
			}
			if len(dump.Tables) != 0 {
				t.Errorf("tables got = %+v, want none: the definition is not in this statement", dump.Tables)
			}
		})
	}
}

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

// TestMatchClausesAreConsumedWhateverTheySay is the case in this file with the
// worst failure mode behind it, and the row that matters is the one with no
// warning in it.
//
// parseReferentialActions is a loop whose default arm RETURNS, so a clause it
// fails to consume ends the loop and everything written after it is never read.
// MATCH SIMPLE is legal SQL and is what a hand-written foreign key spells out;
// if the arm that consumes `simple` stopped consuming it, the ON DELETE CASCADE
// that follows would be dropped in silence - the foreign key would still be
// imported, the document would still satisfy the schema, and a referential
// action would simply have gone missing. Every row therefore asserts OnDelete,
// including the rows that emit nothing.
//
// MATCH PARTIAL shares the same switch and is deliberately not a row here.
// PostgreSQL has never implemented it - the server answers "MATCH PARTIAL not
// yet implemented" - so no dump can carry one, and a test asserting that an
// unreachable message says what it says would only make the arm harder to
// remove.
func TestMatchClausesAreConsumedWhateverTheySay(t *testing.T) {
	tests := []struct {
		name        string
		match       string
		wantMessage string
	}{
		{
			name:        "MATCH FULL",
			match:       "MATCH FULL ",
			wantMessage: "foreign key t_fk on table public.t: MATCH FULL is not imported",
		},
		{
			// The silent one.
			name:  "MATCH SIMPLE",
			match: "MATCH SIMPLE ",
		},
		{
			name:  "no MATCH at all",
			match: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "ALTER TABLE ONLY public.t ADD CONSTRAINT t_fk "+
				"FOREIGN KEY (a) REFERENCES public.u(b) "+tt.match+"ON DELETE CASCADE;")
			if tt.wantMessage == "" {
				if len(warnings) != 0 {
					t.Errorf("warnings got = %v, want none", messages(warnings))
				}
			} else {
				if len(warnings) != 1 {
					t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
				}
				if !strings.Contains(warnings[0].Message, tt.wantMessage) {
					t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, tt.wantMessage)
				}
				if warnings[0].Line != 1 {
					t.Errorf("warning line got = %v, want 1", warnings[0].Line)
				}
			}
			if len(dump.Constraints) != 1 {
				t.Fatalf("constraints got = %+v, want 1", dump.Constraints)
			}
			if got := dump.Constraints[0].OnDelete; got != "CASCADE" {
				t.Errorf("on delete got = %q, want %q: the MATCH clause swallowed what followed it", got, "CASCADE")
			}
		})
	}
}

// TestAColumnListOnAReferentialActionIsReported covers the syntax PostgreSQL 15
// added: SET NULL and SET DEFAULT may name a subset of the key's columns. That
// narrows what the action does, a design document has no field for it, and
// pg_dump emits it for a key declared that way - so this is a form a real dump
// reaches, unlike most of the tolerance in this file.
//
// The assertion beyond the message is that the action itself and both column
// lists survive: dropping the parenthesised list must not take the ON DELETE
// with it.
func TestAColumnListOnAReferentialActionIsReported(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		wantMessage string
		wantAction  string
	}{
		{
			name:        "SET NULL",
			action:      "SET NULL (a)",
			wantMessage: "foreign key t_fk on table public.t: column list on SET NULL is not imported",
			wantAction:  "SET NULL",
		},
		{
			name:        "SET DEFAULT",
			action:      "SET DEFAULT (a)",
			wantMessage: "foreign key t_fk on table public.t: column list on SET DEFAULT is not imported",
			wantAction:  "SET DEFAULT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "ALTER TABLE ONLY public.t ADD CONSTRAINT t_fk "+
				"FOREIGN KEY (a, b) REFERENCES public.u(c, d) ON DELETE "+tt.action+";")
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
			}
			if !strings.Contains(warnings[0].Message, tt.wantMessage) {
				t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, tt.wantMessage)
			}
			if warnings[0].Line != 1 {
				t.Errorf("warning line got = %v, want 1", warnings[0].Line)
			}
			if len(dump.Constraints) != 1 {
				t.Fatalf("constraints got = %+v, want 1", dump.Constraints)
			}
			c := dump.Constraints[0]
			if c.OnDelete != tt.wantAction {
				t.Errorf("on delete got = %q, want %q", c.OnDelete, tt.wantAction)
			}
			if !slices.Equal(c.Columns, []string{"a", "b"}) || !slices.Equal(c.RefColumns, []string{"c", "d"}) {
				t.Errorf("columns got = %v -> %v, want [a b] -> [c d]", c.Columns, c.RefColumns)
			}
		})
	}
}

// TestIndexElementsThatAreNotPlainColumns holds parseCreateIndex's stated rule:
// an index over an expression is dropped WHOLE, because importing only the
// plain columns of "USING btree (a, lower(b))" would describe an index that
// does not exist and would silently claim a uniqueness or a lookup the database
// does not provide.
//
// The three inputs reach the decision by three different routes - an element
// that does not start with a name at all, a name followed by an operator, and a
// list mixing a plain column with a function call - and the third is the input
// the source comment itself names.
func TestIndexElementsThatAreNotPlainColumns(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "an element that opens with a parenthesis", src: "CREATE INDEX t_x ON public.t USING btree ((a || b));"},
		{name: "a column followed by an operator", src: "CREATE INDEX t_x ON public.t USING btree (a + 1);"},
		{name: "one plain column and one expression", src: "CREATE INDEX t_x ON public.t USING btree (a, lower(b));"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if len(dump.Indexes) != 0 {
				t.Errorf("indexes got = %+v, want none: an expression index is dropped whole", dump.Indexes)
			}
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
			}
			if want := "index t_x on table public.t: expression index is not imported"; !strings.Contains(warnings[0].Message, want) {
				t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
			}
		})
	}
}

// TestASCIsTheOnlyIndexColumnOptionThatSaysNothing states one claim about the
// whole group: every option written on an index column is dropped and the index
// itself is kept, and exactly one of those options is silent about it.
//
// ASC is the odd one out because it changes nothing - it restates PostgreSQL's
// default order - and its arm is therefore a no-op whose whole correctness is in
// what it does not do. Running that arm without asserting the silence would pass
// just as well if ASC began raising the warning, which would put a line on the
// standard error of every user whose dump spells the default out. The other four
// options each change what the index can answer, so losing them silently would
// be describing an index the database does not have.
func TestASCIsTheOnlyIndexColumnOptionThatSaysNothing(t *testing.T) {
	tests := []struct {
		name    string
		element string
		warns   bool
	}{
		{name: "ASC", element: "a ASC"},
		{name: "DESC", element: "a DESC", warns: true},
		{name: "NULLS LAST", element: "a NULLS LAST", warns: true},
		{name: "a collation", element: `a COLLATE pg_catalog."C"`, warns: true},
		{name: "an operator class", element: "a text_pattern_ops", warns: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE INDEX t_x ON public.t USING btree ("+tt.element+");")
			if tt.warns {
				if len(warnings) != 1 {
					t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
				}
				want := "index t_x on table public.t: column ordering options are not imported"
				if !strings.Contains(warnings[0].Message, want) {
					t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
				}
			} else if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
			// Warned about or not, the index is kept over the column it named:
			// the published contract is that the object survives and only the
			// clause is lost.
			if len(dump.Indexes) != 1 || !slices.Equal(dump.Indexes[0].Columns, []string{"a"}) {
				t.Errorf("indexes got = %+v, want one over [a]", dump.Indexes)
			}
		})
	}
}

// TestCreateIndexTailClausesThatChangeNothing covers what follows the column
// list. WITH carries storage parameters, which pg_dump writes for an index that
// has them and which say nothing about the design; TABLESPACE is not even
// recognised and is stepped over one token at a time, which is the tolerance
// that keeps an index from a newer PostgreSQL importable. Neither may cost the
// index or produce a line the user has to read - INCLUDE and WHERE, which do
// narrow what the index covers, are warned about and tested elsewhere.
func TestCreateIndexTailClausesThatChangeNothing(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "storage parameters", src: "CREATE INDEX t_x ON public.t USING btree (a) WITH (fillfactor='70');"},
		{name: "an unrecognised tail clause", src: "CREATE INDEX t_x ON public.t USING btree (a) TABLESPACE pg_default;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
			if len(dump.Indexes) != 1 || !slices.Equal(dump.Indexes[0].Columns, []string{"a"}) {
				t.Errorf("indexes got = %+v, want one over [a]", dump.Indexes)
			}
		})
	}
}

// TestAnIndexNeedNotBeNamedToParse records where the two phases of this importer
// divide. PostgreSQL allows CREATE INDEX with no name and generates one
// server-side; pg_dump always writes the generated name, so this is a form only
// hand-written SQL takes. The parser's job is to record what the dump said, so
// it keeps the index with an empty name and says nothing. Deciding that a
// document cannot hold a nameless index is the resolver's job, and build_test.go
// asserts that half.
// TestNullsNotDistinctOnAnIndexIsReported covers #54's fourth case, and it is
// a disagreement inside one package rather than a gap: the constraint path
// already warns about NULLS NOT DISTINCT, and the index path did not, so the
// same clause was reported or not depending on which spelling pg_dump chose.
//
// pg_dump 16.13 writes both, for the same table:
//
//	ALTER TABLE ... ADD CONSTRAINT uq UNIQUE NULLS NOT DISTINCT (email);
//	CREATE UNIQUE INDEX ux ON public.t USING btree (email) NULLS NOT DISTINCT;
//
// The clause changes what the index enforces - whether two NULLs collide - so
// an index recorded without it claims a uniqueness the database does not have.
func TestNullsNotDistinctOnAnIndexIsReported(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (email text);\n"+
		"CREATE UNIQUE INDEX ux_email ON public.t USING btree (email) NULLS NOT DISTINCT;")
	msgs := messages(warnings)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "NULLS NOT DISTINCT is not imported") {
		t.Fatalf("warnings got = %v, want exactly one about NULLS NOT DISTINCT", msgs)
	}
	// The index is still imported: the clause is what is dropped.
	if len(dump.Indexes) != 1 || dump.Indexes[0].Name != "ux_email" || !dump.Indexes[0].Unique {
		t.Errorf("indexes got = %+v, want the unique ux_email", dump.Indexes)
	}
}

// TestNullsDistinctOnAnIndexStaysSilent is the other side: NULLS DISTINCT is
// PostgreSQL's default, so writing it says nothing the document does not
// already say and there is nothing to report.
func TestNullsDistinctOnAnIndexStaysSilent(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE public.t (email text);\n"+
		"CREATE UNIQUE INDEX ux_email ON public.t USING btree (email) NULLS DISTINCT;")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	if len(dump.Indexes) != 1 || !dump.Indexes[0].Unique {
		t.Errorf("indexes got = %+v, want the unique ux_email", dump.Indexes)
	}
}

func TestAnIndexNeedNotBeNamedToParse(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE INDEX ON public.t USING btree (a);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	if len(dump.Indexes) != 1 {
		t.Fatalf("indexes got = %+v, want 1", dump.Indexes)
	}
	idx := dump.Indexes[0]
	if idx.Name != "" || idx.Table.Schema != "public" || idx.Table.Name != "t" ||
		!slices.Equal(idx.Columns, []string{"a"}) {
		t.Errorf("index got = %+v, want an unnamed index on public.t over [a]", idx)
	}
}

// TestAddColumnOnATableThisDumpNeverCreated is about what must NOT happen. A
// partial, truncated or concatenated dump can alter a table it never created,
// and the tempting reading - start a table here - would put a table in the
// document with one column and no CREATE TABLE behind it, describing something
// the database does not have. So the statement is reported and dropped, and the
// assertion that carries the meaning is that no table was invented.
//
// The second case is the control: with the CREATE TABLE present, the same ALTER
// is a different warning and the column lands at the end of the list.
func TestAddColumnOnATableThisDumpNeverCreated(t *testing.T) {
	dump, warnings := mustParse(t, "ALTER TABLE ONLY public.orphan ADD COLUMN b integer;")
	if len(dump.Tables) != 0 {
		t.Errorf("tables got = %+v, want none: no table may be invented from an ALTER", dump.Tables)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
	}
	want := "table public.orphan: ADD COLUMN names a table this dump never created; not imported"
	if !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}

	dump, warnings = mustParse(t, "CREATE TABLE public.orphan (a integer);\n"+
		"ALTER TABLE ONLY public.orphan ADD COLUMN b integer;")
	if len(warnings) != 1 {
		t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
	}
	if want := "ADD COLUMN is imported at the end of the column list"; !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}
	if len(dump.Tables) != 1 {
		t.Fatalf("tables got = %+v, want 1", dump.Tables)
	}
	var names []string
	for _, col := range dump.Tables[0].Columns {
		names = append(names, col.Name)
	}
	if want := []string{"a", "b"}; !slices.Equal(names, want) {
		t.Errorf("columns got = %v, want %v", names, want)
	}
}

// TestNamePartErrorsNameWhatWasWritten asserts the whole rendered error rather
// than a fragment of it, because these messages exist to be read: they quote the
// dotted name back at the user, which is how they say which line of a truncated
// or hand-edited dump went wrong. A Contains assertion would not notice the
// rejoining of the parts breaking.
func TestNamePartErrorsNameWhatWasWritten(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a comment on four name parts",
			src:  "COMMENT ON COLUMN public.t.a.b IS 'x';",
			want: "line 1: expected [schema.]table.column after COMMENT ON COLUMN, got public.t.a.b",
		},
		{
			name: "a comment on a bare name",
			src:  "COMMENT ON COLUMN t IS 'x';",
			want: "line 1: expected [schema.]table.column after COMMENT ON COLUMN, got t",
		},
		{
			name: "a comment with no IS",
			src:  "COMMENT ON TABLE public.t 'x';",
			want: `line 1: expected IS in COMMENT ON, got string "x"`,
		},
		{
			name: "a sequence owned by four name parts",
			src:  "ALTER SEQUENCE public.s OWNED BY public.t.a.b;",
			want: "line 1: expected [schema.]table.column after OWNED BY, got public.t.a.b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d diagList
			_, err := parse([]byte(tt.src), &d)
			if err == nil {
				t.Fatalf("parse(%q) returned no error, want %q", tt.src, tt.want)
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("parse(%q) error got = %q, want %q", tt.src, got, tt.want)
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
