package ddl

import (
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// TestMySQLEdgeFixtureCoversItsCases names, one by one, the awkward cases the
// MySQL edge golden is supposed to show, so that a regression reports which
// case broke rather than dumping a diff of the whole file. It is the sibling of
// TestEdgeFixtureCoversItsCases in export_test.go, deliberately written out
// again rather than shared: the two lists have almost no line in common,
// because almost every line is the thing the two dialects disagree about.
//
// Every string below was applied to a live MySQL 8.0 before it was written
// here. The fixture's own descriptions say which case each table is for.
func TestMySQLEdgeFixtureCoversItsCases(t *testing.T) {
	src := render(t, loadDoc(t, "mysql/edge.json"))

	tests := []struct {
		name string
		want string
	}{
		{"a table named after a reserved word, backtick quoted", "CREATE TABLE `order` ("},
		{"a column named after a reserved word", "    `user` BIGINT UNSIGNED NOT NULL"},
		{"an auto increment column, after NOT NULL", "`id` BIGINT NOT NULL AUTO_INCREMENT COMMENT"},
		{"a boolean as TINYINT(1), whose length alone survives", "`active` TINYINT(1) NOT NULL DEFAULT 1"},
		{"an unsigned type keeps its attribute after the parameters", "`total` DECIMAL(10,2) UNSIGNED NOT NULL"},
		{"an attribute on a type that takes no parameters", "`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT"},
		{"a precision alone", "`rate` DECIMAL(8) COMMENT"},
		{"a display width dropped from an integer", "`flags` INT COMMENT"},
		{"a postfix time precision", "`placed_at` DATETIME(3) NOT NULL"},
		{"a default matching that precision, copied verbatim", "DEFAULT CURRENT_TIMESTAMP(3)"},
		{"an unknown type keeping its parameter", "`score` FLOAT(24) COMMENT"},
		{"a column comment inline, at the end of the definition", "COMMENT '受注状態'"},
		{"a table comment after the closing parenthesis", ") COMMENT='受注\nA table named after a reserved word."},
		{"an apostrophe in a comment, doubled", "COMMENT 'ラベル''s'"},
		{"an apostrophe in a default, copied verbatim", "DEFAULT 'it''s'"},
		{"a backslash in a comment, doubled", `COMMENT 'C:\\tmp`},
		{"a logical name and a description joined by a real newline", "COMMENT '利用者ID\nA column named after a reserved word"},
		{"an unnamed primary key", "    PRIMARY KEY (`id`)"},
		{"an unnamed unique key", "    UNIQUE (`user`, `placed_at`)"},
		{"a named primary key, whose name MySQL will discard", "    CONSTRAINT `pk_user` PRIMARY KEY (`id`),"},
		{"a named unique key", "    CONSTRAINT `uq_user_email` UNIQUE (`email`)"},
		{"a table with no key, no index and no foreign key", "CREATE TABLE `audit` (\n    `at` DATETIME NOT NULL,\n    `who` VARCHAR(64) NOT NULL\n) COMMENT='監査';"},
		{"a non-unique index in its own phase", "CREATE INDEX `ix_order_status` ON `order` (`status`);"},
		{"a unique index in its own phase", "CREATE UNIQUE INDEX `ix_categories_name` ON `categories` (`name`);"},
		{
			"an unnamed foreign key, and the first half of a cycle",
			"ALTER TABLE `order` ADD FOREIGN KEY (`user`) REFERENCES `user` (`id`) ON UPDATE CASCADE ON DELETE RESTRICT;",
		},
		{
			"the second half of the cycle, needing no ordering between the two tables",
			"ALTER TABLE `user` ADD CONSTRAINT `fk_user_home_order` FOREIGN KEY (`home_order_id`) REFERENCES `order` (`id`) ON DELETE SET NULL;",
		},
		{
			"a self-referencing foreign key, and the fourth referential action",
			"ALTER TABLE `categories` ADD CONSTRAINT `fk_categories_parent` FOREIGN KEY (`parent_id`) REFERENCES `categories` (`id`) ON UPDATE NO ACTION;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(src, tt.want) {
				t.Errorf("the rendered edge fixture does not contain\n\t%s\ngot:\n%s", tt.want, src)
			}
		})
	}

	// A column whose logical name is just its physical name and which has no
	// description gets no comment, exactly as it gets no COMMENT ON in the
	// other dialect: that is the state the importer leaves for an object the
	// dump carried none for.
	if strings.Contains(src, "`note` VARCHAR(32) COMMENT") {
		t.Errorf("the output comments a column that says nothing about itself:\n%s", src)
	}
	// SET DEFAULT is not in this fixture. MySQL takes it and InnoDB never
	// performs it, so it belongs in the documented drift rather than in the
	// document whose whole job is to be applied to a server.
	if strings.Contains(src, "SET DEFAULT") {
		t.Errorf("the edge fixture uses SET DEFAULT, which MySQL records and never performs:\n%s", src)
	}
}

// TestMySQLHeaderMatchesPostgreSQL holds the two writers to one header. It is
// the build-artifact line a reader meets first, and a dialect that wrote its
// own would make the policy look like a property of one target rather than of
// jjf.
func TestMySQLHeaderMatchesPostgreSQL(t *testing.T) {
	pg := render(t, loadDoc(t, "minimal.json"))
	my := render(t, loadDoc(t, "mysql/minimal.json"))

	head := func(src string) string {
		lines := strings.SplitN(src, "\n", 3)
		if len(lines) < 3 {
			t.Fatalf("the output has fewer than two lines: %q", src)
		}
		return lines[0] + "\n" + lines[1]
	}
	if head(pg) != head(my) {
		t.Errorf("the two dialects open differently:\n--- PostgreSQL ---\n%s\n--- MySQL ---\n%s", head(pg), head(my))
	}
}

// TestMySQLHasNoCommentPhase is the fourth phase's absence asserted rather than
// left to a golden diff. MySQL has no COMMENT ON statement at all, so a script
// carrying one would not run, and a -- Comments section with nothing under it
// would be a section guard that had lost its guard.
func TestMySQLHasNoCommentPhase(t *testing.T) {
	src := render(t, loadDoc(t, "mysql/edge.json"))

	for _, absent := range []string{"COMMENT ON ", "-- Comments"} {
		if strings.Contains(src, absent) {
			t.Errorf("the MySQL output contains %q:\n%s", absent, src)
		}
	}
	// The comments did not vanish with the phase; they moved.
	for _, present := range []string{" COMMENT '", ") COMMENT='"} {
		if !strings.Contains(src, present) {
			t.Errorf("the MySQL output has no %q, so the comments were dropped rather than folded:\n%s", present, src)
		}
	}
}

// TestMySQLWritesNoPostgreSQLSpellings pins the things a shared helper would
// most plausibly leak into the wrong dialect: the other quoting, the other
// identity syntax, and a session setting neither writer may emit.
//
// The SET check looks for a statement rather than for the two words, because
// ON DELETE SET NULL is an ordinary referential action and matching it would
// make the test fail on the one MySQL script that is most obviously right.
func TestMySQLWritesNoPostgreSQLSpellings(t *testing.T) {
	for _, name := range myFixtures {
		t.Run(name, func(t *testing.T) {
			src := render(t, loadDoc(t, name))
			for _, absent := range []string{`"`, "GENERATED BY DEFAULT AS IDENTITY", "\nSET "} {
				if strings.Contains(src, absent) {
					t.Errorf("the MySQL output contains %q:\n%s", absent, src)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Quoting
// ---------------------------------------------------------------------------

// TestMySQLQuoteLiteralDoublesBackslashAndQuote is where the one-pass escaping
// is actually pinned. A two-pass implementation passes every case here except
// the fourth, which is why the fourth exists.
func TestMySQLQuoteLiteralDoublesBackslashAndQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"the empty string", "", "''"},
		{"ordinary text", "hello", "'hello'"},
		{"one apostrophe", "it's", "'it''s'"},
		{"one backslash", `C:\tmp`, `'C:\\tmp'`},
		{"a backslash immediately before an apostrophe", `a\'b`, `'a\\''b'`},
		{"a backslash immediately after an apostrophe", `a'\b`, `'a''\\b'`},
		{"two backslashes", `a\\b`, `'a\\\\b'`},
		{"a real newline, which the comment fold depends on", "受注\n説明", "'受注\n説明'"},
		{"Japanese text, every byte of which is >= 0x80", "顧客ID", "'顧客ID'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := myQuoteLiteral(tt.in); got != tt.want {
				t.Errorf("myQuoteLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMySQLQuoteIdentDoublesTheBacktick exercises the defensive half of the
// quoting. The schema's identifier pattern forbids a backtick, so nothing but
// this test can reach it - which is the argument text.go already makes about
// pgQuoteIdent, and the reason to keep the doubling rather than drop it as dead
// code.
func TestMySQLQuoteIdentDoublesTheBacktick(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"order", "`order`"},
		{"", "``"},
		{"we`ird", "`we``ird`"},
		// Two backticks become four, plus the pair around them. Written with
		// escapes because a Go raw string cannot hold a backtick at all, which
		// is itself a small reminder of why the doubling is worth testing.
		{"\x60\x60", "\x60\x60\x60\x60\x60\x60"},
	}
	for _, tt := range tests {
		if got := myQuoteIdent(tt.in); got != tt.want {
			t.Errorf("myQuoteIdent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

func TestMySQLTypeParamKind(t *testing.T) {
	tests := []struct {
		name string
		want paramKind
	}{
		{"VARCHAR", paramLength},
		{"CHAR", paramLength},
		{"BINARY", paramLength},
		{"VARBINARY", paramLength},
		{"BIT", paramLength},
		// The one integer that keeps a length, because tinyint(1) is how
		// MySQL stores a boolean and mysqldump writes it back.
		{"TINYINT", paramLength},
		{"DECIMAL", paramPrecisionScale},
		{"NUMERIC", paramPrecisionScale},
		{"DATETIME", paramTimePrecision},
		{"TIMESTAMP", paramTimePrecision},
		{"TIME", paramTimePrecision},
		{"INT", paramNone},
		{"BIGINT", paramNone},
		{"TEXT", paramNone},
		{"JSON", paramNone},
		// ENUM's parenthesis holds a value list rather than a number, and the
		// document has nowhere to keep one - so there is nothing to write and
		// the column is emitted bare. See M8 in design/ddl-export.md.
		{"ENUM", paramNone},
		{"SET", paramNone},
		// The attribute is myRenderType's business, and this function is
		// documented as receiving a name already stripped of one. Answering
		// these paramUnknown is what stops a later change stripping the suffix
		// twice.
		{"BIGINT UNSIGNED", paramUnknown},
		{"DECIMAL UNSIGNED", paramUnknown},
		// A type jjf does not know: reproduce whatever the document declared.
		{"GEOMETRYCOLLECTION", paramUnknown},
		{"NOT_A_TYPE", paramUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := myTypeParamKind(tt.name); got != tt.want {
				t.Errorf("myTypeParamKind(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

// TestMySQLRenderTypePutsParametersBeforeTheAttribute is the whole of M7 in one
// table. DECIMAL UNSIGNED is the case that matters: MySQL answers
// DECIMAL UNSIGNED(10,2) with a syntax error.
func TestMySQLRenderTypePutsParametersBeforeTheAttribute(t *testing.T) {
	tests := []struct {
		name string
		col  model.Column
		want string
	}{
		{
			name: "an attribute after a precision and a scale",
			col:  model.Column{Type: "DECIMAL UNSIGNED", Precision: intp(10), Scale: intp(2)},
			want: "DECIMAL(10,2) UNSIGNED",
		},
		{
			name: "an attribute on a type that takes no parameter",
			col:  model.Column{Type: "BIGINT UNSIGNED"},
			want: "BIGINT UNSIGNED",
		},
		{
			name: "both attributes, and a length the type cannot take",
			col:  model.Column{Type: "BIGINT UNSIGNED ZEROFILL", Length: intp(20)},
			want: "BIGINT UNSIGNED ZEROFILL",
		},
		{
			name: "the attribute as the document spelled it",
			col:  model.Column{Type: "bigint unsigned"},
			want: "bigint unsigned",
		},
		{
			name: "a length",
			col:  model.Column{Type: "VARCHAR", Length: intp(255)},
			want: "VARCHAR(255)",
		},
		{
			name: "a boolean, which keeps its length",
			col:  model.Column{Type: "TINYINT", Length: intp(1)},
			want: "TINYINT(1)",
		},
		{
			name: "a display width, which does not survive",
			col:  model.Column{Type: "INT", Length: intp(11)},
			want: "INT",
		},
		{
			name: "a postfix time precision",
			col:  model.Column{Type: "DATETIME", Precision: intp(3)},
			want: "DATETIME(3)",
		},
		{
			name: "a type jjf does not know, reproduced as the document states it",
			col:  model.Column{Type: "FLOAT", Length: intp(24)},
			want: "FLOAT(24)",
		},
		{
			// The guard against an empty base: a type whose whole name is an
			// attribute is a type called UNSIGNED, not a nameless one.
			name: "a type called after an attribute",
			col:  model.Column{Type: "UNSIGNED"},
			want: "UNSIGNED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := myRenderType(&tt.col); got != tt.want {
				t.Errorf("myRenderType(%+v) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}
