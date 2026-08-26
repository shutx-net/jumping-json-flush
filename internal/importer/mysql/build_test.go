package mysql

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// testOptions are the options every test here imports with: the defaults plus a
// source name, so that the database name never has to be spelled out.
func testOptions() Options {
	opt := DefaultOptions()
	opt.Source = "shop.sql"
	return opt
}

// mustImport imports src and fails the test if the import did not succeed.
func mustImport(t *testing.T, src string) (*model.Document, []Diagnostic) {
	t.Helper()
	doc, warnings, err := Import([]byte(src), testOptions())
	if err != nil {
		t.Fatalf("Import returned error %v, want no error", err)
	}
	return doc, warnings
}

// importError imports src and fails the test unless the import failed.
func importError(t *testing.T, src string) error {
	t.Helper()
	_, _, err := Import([]byte(src), testOptions())
	if err == nil {
		t.Fatal("Import returned no error, want one")
	}
	return err
}

// table returns the imported table of that name, or fails the test.
func table(t *testing.T, doc *model.Document, name string) *model.Table {
	t.Helper()
	for i := range doc.Tables {
		if doc.Tables[i].Name == name {
			return &doc.Tables[i]
		}
	}
	t.Fatalf("the document has no table %q", name)
	return nil
}

// column returns the imported column, or fails the test.
func column(t *testing.T, tbl *model.Table, name string) *model.Column {
	t.Helper()
	for i := range tbl.Columns {
		if tbl.Columns[i].Name == name {
			return &tbl.Columns[i]
		}
	}
	t.Fatalf("table %s has no column %q", tbl.Name, name)
	return nil
}

// hasWarning reports whether any diagnostic contains want.
func hasWarning(warnings []Diagnostic, want string) bool {
	for _, w := range warnings {
		if strings.Contains(w.Message, want) {
			return true
		}
	}
	return false
}

// showWarnings renders the diagnostics one per line for a failure message.
func showWarnings(warnings []Diagnostic) string {
	var b strings.Builder
	for _, w := range warnings {
		b.WriteString("\n  ")
		b.WriteString(w.String())
	}
	if b.Len() == 0 {
		return " (none)"
	}
	return b.String()
}

// dumpSource is a dump in the order mysqldump really writes one: tables in
// alphabetical order, every key, index, foreign key and comment written inside
// the CREATE TABLE that owns it. orders comes before users, so its foreign key
// names a table the parser has not read yet - which is the whole reason build.go
// exists in a package whose parser could otherwise have done the work.
const dumpSource = "-- MySQL dump 10.13  Distrib 8.0.46, for Linux (x86_64)\n" +
	"--\n" +
	"-- Host: localhost    Database: shop\n" +
	"-- ------------------------------------------------------\n" +
	"-- Server version\t8.0.46-0ubuntu0.24.04.3\n" +
	"\n" +
	"/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;\n" +
	"\n" +
	"DROP TABLE IF EXISTS `orders`;\n" +
	"CREATE TABLE `orders` (\n" +
	"  `id` bigint NOT NULL AUTO_INCREMENT,\n" +
	"  `user_id` int NOT NULL,\n" +
	"  `total` decimal(10,2) DEFAULT NULL,\n" +
	"  `note` text COMMENT '備考\\n自由記述。',\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  KEY `ix_orders_user` (`user_id`),\n" +
	"  CONSTRAINT `fk_orders_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE RESTRICT\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='注文';\n" +
	"\n" +
	"DROP TABLE IF EXISTS `users`;\n" +
	"CREATE TABLE `users` (\n" +
	"  `id` int NOT NULL AUTO_INCREMENT,\n" +
	"  `email` varchar(255) NOT NULL COMMENT 'メールアドレス',\n" +
	"  `created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  UNIQUE KEY `uq_users_email` (`email`)\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ユーザー\\nサービスを使う人。';\n"

func TestImportTables(t *testing.T) {
	doc, warnings := mustImport(t, dumpSource)
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}

	if doc.Database.DBMS != model.DBMSMySQL {
		t.Errorf("dbms got = %v, want %v", doc.Database.DBMS, model.DBMSMySQL)
	}
	if doc.Database.Name != "shop" {
		t.Errorf("database name got = %v, want shop", doc.Database.Name)
	}
	if doc.FormatVersion != model.CurrentFormatVersion {
		t.Errorf("format version got = %v, want %v", doc.FormatVersion, model.CurrentFormatVersion)
	}

	// Source order, which for a real dump is alphabetical and is deliberately
	// not re-sorted into anything else.
	var names []string
	for _, tbl := range doc.Tables {
		names = append(names, tbl.Name)
	}
	if want := []string{"orders", "users"}; !equalStrings(names, want) {
		t.Errorf("tables got = %v, want %v", names, want)
	}

	orders := table(t, doc, "orders")
	if orders.PrimaryKey == nil || !equalStrings(orders.PrimaryKey.Columns, []string{"id"}) {
		t.Errorf("orders primary key got = %v, want id", orders.PrimaryKey)
	}
	if !column(t, orders, "id").AutoIncrement {
		t.Error("orders.id auto increment got = false, want true")
	}
	if len(orders.Indexes) != 1 || orders.Indexes[0].Name != "ix_orders_user" {
		t.Errorf("orders indexes got = %v, want one called ix_orders_user", orders.Indexes)
	}
	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("orders foreign keys got = %v, want exactly 1", orders.ForeignKeys)
	}
	fk := orders.ForeignKeys[0]
	if fk.Name != "fk_orders_user" || fk.References.Table != "users" ||
		!equalStrings(fk.References.Columns, []string{"id"}) {
		t.Errorf("orders foreign key got = %+v, want fk_orders_user referencing users.id", fk)
	}
	if fk.OnDelete != model.ActionCascade || fk.OnUpdate != model.ActionRestrict {
		t.Errorf("referential actions got = %v / %v, want CASCADE / RESTRICT", fk.OnDelete, fk.OnUpdate)
	}

	users := table(t, doc, "users")
	if len(users.UniqueKeys) != 1 || users.UniqueKeys[0].Name != "uq_users_email" {
		t.Errorf("users unique keys got = %v, want one called uq_users_email", users.UniqueKeys)
	}
	if got := column(t, users, "created_at"); got.Default == nil || *got.Default != "CURRENT_TIMESTAMP(3)" {
		t.Errorf("users.created_at default got = %v, want CURRENT_TIMESTAMP(3)", got.Default)
	}
}

// equalStrings compares two string slices. slices.Equal would do, and this says
// the same thing without an import that only one assertion needs.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestForeignKeyToATableDefinedLater is the case mysqldump's alphabetical order
// makes routine rather than exotic: orders is written before users, so the
// foreign key in it names a table that does not exist yet at parse time.
func TestForeignKeyToATableDefinedLater(t *testing.T) {
	doc, warnings := mustImport(t, dumpSource)
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
	orders := table(t, doc, "orders")
	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("orders foreign keys got = %v, want exactly 1", orders.ForeignKeys)
	}
	if got := orders.ForeignKeys[0].References.Table; got != "users" {
		t.Errorf("referenced table got = %v, want users", got)
	}
}

// TestForeignKeyWithNoColumnListResolvesAgainstThePrimaryKey covers the second
// half of the same problem: the primary key the reference resolves against also
// belongs to a table written further down.
func TestForeignKeyWithNoColumnListResolvesAgainstThePrimaryKey(t *testing.T) {
	src := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `user_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  CONSTRAINT `fk_orders_user` FOREIGN KEY (`user_id`) REFERENCES `users`\n" +
		");\n" +
		"CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
	orders := table(t, doc, "orders")
	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("orders foreign keys got = %v, want exactly 1", orders.ForeignKeys)
	}
	if got := orders.ForeignKeys[0].References.Columns; !equalStrings(got, []string{"id"}) {
		t.Errorf("referenced columns got = %v, want id", got)
	}
}

func TestForeignKeyNamingAnUnimportedTableIsWarnedAndDropped(t *testing.T) {
	src := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `user_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  CONSTRAINT `fk_orders_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	orders := table(t, doc, "orders")
	if len(orders.ForeignKeys) != 0 {
		t.Errorf("orders foreign keys got = %v, want none", orders.ForeignKeys)
	}
	if !hasWarning(warnings, "references table users, which was not imported") {
		t.Errorf("warnings got =%s, want one naming the missing table", showWarnings(warnings))
	}
}

// TestPrimaryKeyForcesItsColumnsNotNull holds the importer to what MySQL does
// rather than to what the dump wrote. A document whose primary key column is
// nullable describes a database nobody can create, and internal/check reports
// exactly that.
func TestPrimaryKeyForcesItsColumnsNotNull(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` int,\n" +
		"  `b` int,\n" +
		"  PRIMARY KEY (`a`,`b`)\n" +
		");\n"

	doc, _ := mustImport(t, src)
	tbl := table(t, doc, "t")
	for _, name := range []string{"a", "b"} {
		if column(t, tbl, name).Nullable {
			t.Errorf("t.%s nullable got = true, want false", name)
		}
	}
}

// TestUnrepresentableConstraintNameIsDroppedButTheConstraintSurvives and
// TestUnrepresentableIndexNameIsAnError are the two halves of one asymmetry, and
// the asymmetry is the JSON Schema's: a constraint name is optional and an
// index name is required, so the same unusable name costs a name in one case
// and the whole import in the other.
func TestUnrepresentableConstraintNameIsDroppedButTheConstraintSurvives(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` int NOT NULL,\n" +
		"  `b` int NOT NULL,\n" +
		"  PRIMARY KEY (`a`),\n" +
		"  UNIQUE KEY `uq-b` (`b`)\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	tbl := table(t, doc, "t")
	if len(tbl.UniqueKeys) != 1 {
		t.Fatalf("unique keys got = %v, want exactly 1", tbl.UniqueKeys)
	}
	if tbl.UniqueKeys[0].Name != "" {
		t.Errorf("unique key name got = %q, want it dropped", tbl.UniqueKeys[0].Name)
	}
	if !equalStrings(tbl.UniqueKeys[0].Columns, []string{"b"}) {
		t.Errorf("unique key columns got = %v, want b", tbl.UniqueKeys[0].Columns)
	}
	if !hasWarning(warnings, "imported without a name") {
		t.Errorf("warnings got =%s, want one about the dropped name", showWarnings(warnings))
	}
}

func TestUnrepresentableIndexNameIsAnError(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` int NOT NULL,\n" +
		"  KEY `ix-a` (`a`)\n" +
		");\n"

	err := importError(t, src)
	if want := `index name "ix-a" cannot be represented`; !strings.Contains(err.Error(), want) {
		t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
	}
}

// TestIdentifierThatCannotBeRepresentedStopsTheImport pins the rule that a name
// is never quietly rewritten. MySQL makes this ordinary rather than exotic: a
// backtick-quoted identifier may hold almost any character, so `order-items` is
// a table name a real database has.
func TestIdentifierThatCannotBeRepresentedStopsTheImport(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{
			name:    "a table name",
			src:     "CREATE TABLE `order-items` (`a` int NOT NULL);\n",
			wantMsg: `table name "order-items" cannot be represented`,
		},
		{
			name:    "a column name",
			src:     "CREATE TABLE `t` (`a b` int NOT NULL);\n",
			wantMsg: `column name "a b" cannot be represented`,
		},
		{
			name:    "a name that starts with a digit",
			src:     "CREATE TABLE `2024_sales` (`a` int NOT NULL);\n",
			wantMsg: `table name "2024_sales" cannot be represented`,
		},
		{
			// The schema's length limit, which the rows above do not reach.
			// MySQL allows 64 characters, so this arrives from a hand-written
			// file rather than a server - but the limit is quoted in the
			// message, and one that nothing tests can drift away from the
			// schema it claims to implement.
			name:    "an over-long table name",
			src:     "CREATE TABLE `" + strings.Repeat("a", maxIdentifierLength+1) + "` (`a` int NOT NULL);\n",
			wantMsg: "cannot be represented",
		},
		{
			// A type whose name the format cannot hold stops the import in the
			// same way a column name does. The three messages this reaches are
			// tested at the type layer with nothing showing that one of them
			// reaches a user through Import.
			name:    "a type name",
			src:     "CREATE TABLE `t` (`a` `my-type` NOT NULL);\n",
			wantMsg: "cannot be written to a design document",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := importError(t, tt.src)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("message got = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Errorf("error type got = %T, want it to wrap *syntaxError", err)
			}
		})
	}
}

func TestCommentBecomesLogicalNameAndDescription(t *testing.T) {
	doc, _ := mustImport(t, dumpSource)

	users := table(t, doc, "users")
	if users.LogicalName != "ユーザー" || users.Description != "サービスを使う人。" {
		t.Errorf("users logical name / description got = %q / %q, want ユーザー / サービスを使う人。",
			users.LogicalName, users.Description)
	}
	note := column(t, table(t, doc, "orders"), "note")
	if note.LogicalName != "備考" || note.Description != "自由記述。" {
		t.Errorf("orders.note logical name / description got = %q / %q, want 備考 / 自由記述。",
			note.LogicalName, note.Description)
	}
}

func TestCommentWithNoNewlineIsAllLogicalName(t *testing.T) {
	doc, _ := mustImport(t, dumpSource)
	email := column(t, table(t, doc, "users"), "email")
	if email.LogicalName != "メールアドレス" {
		t.Errorf("users.email logical name got = %q, want メールアドレス", email.LogicalName)
	}
	if email.Description != "" {
		t.Errorf("users.email description got = %q, want it empty", email.Description)
	}
}

func TestObjectWithNoCommentIsNamedAfterItself(t *testing.T) {
	doc, _ := mustImport(t, dumpSource)
	orders := table(t, doc, "orders")
	total := column(t, orders, "total")
	if total.LogicalName != "total" {
		t.Errorf("orders.total logical name got = %q, want total", total.LogicalName)
	}
	if total.Description != "" {
		t.Errorf("orders.total description got = %q, want it empty", total.Description)
	}
}

func TestOverlongDescriptionIsTruncatedAndOverlongDefaultIsDropped(t *testing.T) {
	long := strings.Repeat("あ", maxDescriptionLength+10)
	src := "CREATE TABLE `t` (\n" +
		"  `a` int NOT NULL COMMENT '名前\\n" + long + "',\n" +
		"  `b` varchar(400) DEFAULT '" + strings.Repeat("x", maxDefaultLength+10) + "'\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	tbl := table(t, doc, "t")

	a := column(t, tbl, "a")
	if got := len([]rune(a.Description)); got != maxDescriptionLength {
		t.Errorf("t.a description got = %d runes, want %d", got, maxDescriptionLength)
	}
	if !hasWarning(warnings, "description is longer than") {
		t.Errorf("warnings got =%s, want one about the description", showWarnings(warnings))
	}

	if b := column(t, tbl, "b"); b.Default != nil {
		t.Errorf("t.b default got = %q, want it dropped", *b.Default)
	}
	if !hasWarning(warnings, "default expression is longer than") {
		t.Errorf("warnings got =%s, want one about the default", showWarnings(warnings))
	}
}

// TestOverlongLogicalNameBecomesTheDescription covers the other half of
// describeComment: a first line that long is prose, so it is used as the
// description and the object falls back to being named after itself.
func TestOverlongLogicalNameBecomesTheDescription(t *testing.T) {
	long := strings.Repeat("あ", maxLogicalNameLength+1)
	src := "CREATE TABLE `t` (`a` int NOT NULL COMMENT '" + long + "');\n"

	doc, warnings := mustImport(t, src)
	a := column(t, table(t, doc, "t"), "a")
	if a.LogicalName != "a" {
		t.Errorf("t.a logical name got = %q, want the physical name", a.LogicalName)
	}
	if a.Description != long {
		t.Errorf("t.a description got = %d runes, want the whole comment", len([]rune(a.Description)))
	}
	if !hasWarning(warnings, "used as the description") {
		t.Errorf("warnings got =%s, want one about the first line", showWarnings(warnings))
	}
}

// TestDatabaseName walks the four places a name can come from and the one case
// where there is nothing to use. MySQL supplies two of them itself, which is
// where this differs from the PostgreSQL importer: a dump names the database it
// was taken from in its banner and again in a USE statement.
func TestDatabaseName(t *testing.T) {
	const table = "CREATE TABLE `t` (`a` int NOT NULL);\n"

	tests := []struct {
		name   string
		src    string
		option string
		source string
		want   string
	}{
		{
			name:   "from the header banner",
			src:    "-- Host: localhost    Database: shop\n" + table,
			source: "dump.sql",
			want:   "shop",
		},
		{
			name:   "from a USE statement, which beats the banner",
			src:    "-- Host: localhost    Database: shop\nUSE `archive`;\n" + table,
			source: "dump.sql",
			want:   "archive",
		},
		{
			name:   "from the option, which beats both",
			src:    "-- Host: localhost    Database: shop\nUSE `archive`;\n" + table,
			option: "chosen",
			source: "dump.sql",
			want:   "chosen",
		},
		{
			name:   "from the file name when the dump says nothing",
			src:    table,
			source: "warehouse.sql",
			want:   "warehouse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _, err := Import([]byte(tt.src), Options{Database: tt.option, Source: tt.source})
			if err != nil {
				t.Fatalf("Import returned error %v, want no error", err)
			}
			if doc.Database.Name != tt.want {
				t.Errorf("database name got = %v, want %v", doc.Database.Name, tt.want)
			}
		})
	}

	t.Run("nothing to use at all", func(t *testing.T) {
		_, _, err := Import([]byte(table), Options{})
		if err == nil {
			t.Fatal("Import returned no error, want one")
		}
		if want := "pass -database <name>"; !strings.Contains(err.Error(), want) {
			t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
		}
	})

	t.Run("a file name that is not an identifier", func(t *testing.T) {
		_, _, err := Import([]byte(table), Options{Source: "db-dump.sql"})
		if err == nil {
			t.Fatal("Import returned no error, want one")
		}
		if want := `database name "db-dump" cannot be represented`; !strings.Contains(err.Error(), want) {
			t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
		}
	})
}

func TestNormalizeDefaultCollapsesWhitespaceButNotInsideALiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "a function call keeps its shape", in: "(uuid())", want: "(uuid())"},
		{name: "a bit literal is not split", in: "b'10101010'", want: "b'10101010'"},
		{name: "arithmetic keeps single spaces", in: "((1 + 2))", want: "((1 + 2))"},
		{name: "a run of spaces collapses", in: "1    +    2", want: "1 + 2"},
		{name: "a newline collapses", in: "1 +\n    2", want: "1 + 2"},
		{name: "spacing inside a literal survives", in: "'a  b'", want: "'a  b'"},
		{name: "a backslash escape survives", in: `'C:\\tmp'`, want: `'C:\\tmp'`},
		{name: "a comment in a gap disappears", in: "1 /* two */ + 2", want: "1 + 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDefault(tt.in); got != tt.want {
				t.Errorf("normalizeDefault(%q) got = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestDefaultNullOnANullableColumnIsNotImported covers the one default this
// importer drops on sight. MySQL writes DEFAULT NULL for every nullable column
// it was not given a default for, so recording it would put a default on nearly
// every column of every imported document, each restating what nullable already
// says.
func TestDefaultNullOnANullableColumnIsNotImported(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` int DEFAULT NULL,\n" +
		"  `b` int NOT NULL DEFAULT '0',\n" +
		"  `c` int NOT NULL DEFAULT NULL\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	tbl := table(t, doc, "t")

	if got := column(t, tbl, "a"); got.Default != nil {
		t.Errorf("t.a default got = %q, want it dropped", *got.Default)
	}
	if got := column(t, tbl, "b"); got.Default == nil || *got.Default != "'0'" {
		t.Errorf("t.b default got = %v, want '0'", got.Default)
	}
	// A NOT NULL column with DEFAULT NULL is a contradiction MySQL will not
	// store, so it can only come from a hand-written file. The document says
	// what the file said and "jjf validate" is where that is reported.
	if got := column(t, tbl, "c"); got.Default == nil || *got.Default != "NULL" {
		t.Errorf("t.c default got = %v, want NULL kept verbatim", got.Default)
	}
	// Dropping it is silent on purpose: a warning per nullable column would
	// bury the warnings that matter.
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
}

// TestOnUpdateCurrentTimestampIsReported covers the one column attribute that
// is neither a type, a default nor a constraint, and that the design format has
// nowhere to put.
func TestOnUpdateCurrentTimestampIsReported(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	a := column(t, table(t, doc, "t"), "a")
	if a.Default == nil || *a.Default != "CURRENT_TIMESTAMP" {
		t.Errorf("t.a default got = %v, want CURRENT_TIMESTAMP alone", a.Default)
	}
	if !hasWarning(warnings, "ON UPDATE CURRENT_TIMESTAMP is not represented") {
		t.Errorf("warnings got =%s, want one about the update rule", showWarnings(warnings))
	}
}

// TestTheIndexBackingAForeignKeyIsNotImportedTwice is the one place the
// importer declines to record something a dump plainly said, and the reason is
// the design format: MySQL keeps index names per table and foreign key names
// per schema, so InnoDB happily names the index it auto-creates after the
// constraint - while a jjf document keeps both in one namespace per table, and
// jjf itself would then refuse to export the document it had just written.
func TestTheIndexBackingAForeignKeyIsNotImportedTwice(t *testing.T) {
	src := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `user_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  KEY `fk_orders_user` (`user_id`),\n" +
		"  CONSTRAINT `fk_orders_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)\n" +
		");\n" +
		"CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	orders := table(t, doc, "orders")
	if len(orders.Indexes) != 0 {
		t.Errorf("orders indexes got = %v, want none", orders.Indexes)
	}
	if len(orders.ForeignKeys) != 1 {
		t.Errorf("orders foreign keys got = %v, want exactly 1", orders.ForeignKeys)
	}
	if !hasWarning(warnings, "has the name of a foreign key of the same table") {
		t.Errorf("warnings got =%s, want one naming the backing index", showWarnings(warnings))
	}
}

// TestAnIndexOverAForeignKeysColumnsUnderItsOwnNameIsKept is the other side of
// the same rule, and it is what stops it from growing into "suppress any index
// that duplicates a foreign key". An index the designer named is a fact the
// designer stated, it collides with nothing, and it is imported.
func TestAnIndexOverAForeignKeysColumnsUnderItsOwnNameIsKept(t *testing.T) {
	src := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `user_id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`),\n" +
		"  KEY `ix_orders_user` (`user_id`),\n" +
		"  CONSTRAINT `fk_orders_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)\n" +
		");\n" +
		"CREATE TABLE `users` (\n" +
		"  `id` int NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		");\n"

	doc, warnings := mustImport(t, src)
	orders := table(t, doc, "orders")
	if len(orders.Indexes) != 1 || orders.Indexes[0].Name != "ix_orders_user" {
		t.Errorf("orders indexes got = %v, want one called ix_orders_user", orders.Indexes)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
}

// TestTablesOfOneNameFromSeveralDatabasesAreAnError covers what a
// "mysqldump --databases a b" produces when both databases hold a table of one
// name: a document has room for one of them, and choosing either would describe
// a schema nobody has.
func TestTablesOfOneNameFromSeveralDatabasesAreAnError(t *testing.T) {
	src := "USE `a`;\n" +
		"CREATE TABLE `t` (`x` int NOT NULL);\n" +
		"USE `b`;\n" +
		"CREATE TABLE `t` (`y` int NOT NULL);\n"

	err := importError(t, src)
	if want := `table "t" is defined twice`; !strings.Contains(err.Error(), want) {
		t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
	}
}

// TestADatabaseQualificationIsReportedAndDropped covers the shape only a
// hand-written or concatenated file produces: mysqldump writes bare names, and
// a document has room for exactly one database.
func TestADatabaseQualificationIsReportedAndDropped(t *testing.T) {
	src := "-- Host: localhost    Database: shop\n" +
		"CREATE TABLE `archive`.`orders` (`id` int NOT NULL, PRIMARY KEY (`id`));\n"

	doc, warnings := mustImport(t, src)
	if got := table(t, doc, "orders"); got.Name != "orders" {
		t.Errorf("table name got = %v, want orders", got.Name)
	}
	if !hasWarning(warnings, `database qualification "archive" is not represented`) {
		t.Errorf("warnings got =%s, want one about the qualification", showWarnings(warnings))
	}

	// The matching case says nothing, because there is nothing to say.
	quiet := "-- Host: localhost    Database: shop\n" +
		"CREATE TABLE `shop`.`orders` (`id` int NOT NULL, PRIMARY KEY (`id`));\n"
	if _, warnings := mustImport(t, quiet); len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
}

func TestADumpWithNoTablesIsAnError(t *testing.T) {
	src := "-- Host: localhost    Database: shop\n" +
		"SET @a = 1;\n" +
		"DROP TABLE IF EXISTS `t`;\n"

	err := importError(t, src)
	if want := "no tables found in the dump"; !strings.Contains(err.Error(), want) {
		t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
	}
}

// TestImportWarnsAboutAnUnsupportedServerVersion covers the banner check, and
// the distribution suffix the version carries in every packaged build.
func TestImportWarnsAboutAnUnsupportedServerVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		warn    bool
	}{
		{name: "the supported major, with a distribution suffix", version: "8.0.46-0ubuntu0.24.04.3"},
		{name: "the supported major, bare", version: "8.4.0"},
		{name: "an older major", version: "5.7.44", warn: true},
		{name: "a newer major", version: "9.1.0", warn: true},
		{name: "a MariaDB banner", version: "10.11.6-MariaDB", warn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "-- Server version\t" + tt.version + "\n" +
				"-- Host: localhost    Database: shop\n" +
				"CREATE TABLE `t` (`a` int NOT NULL);\n"
			_, warnings := mustImport(t, src)
			got := hasWarning(warnings, "jjf supports")
			if got != tt.warn {
				t.Errorf("version warning got = %v, want %v (warnings:%s)", got, tt.warn, showWarnings(warnings))
			}
		})
	}

	t.Run("a dump with no banner is not warned about", func(t *testing.T) {
		_, warnings := mustImport(t, "CREATE TABLE `t` (`a` int NOT NULL);\n")
		if len(warnings) != 0 {
			t.Errorf("warnings got =%s, want none", showWarnings(warnings))
		}
	})
}

// TestSupportedMajorsReadsAsASentence keeps the one-major case from printing
// "jjf supports 8 to 8", which reads as a mistake rather than as a fact.
func TestSupportedMajorsReadsAsASentence(t *testing.T) {
	got := supportedMajors()
	if minSupportedMajor == maxSupportedMajor {
		if strings.Contains(got, " to ") {
			t.Errorf("supportedMajors got = %q, want a single number", got)
		}
		return
	}
	if !strings.Contains(got, " to ") {
		t.Errorf("supportedMajors got = %q, want a range", got)
	}
}

// resolvableTarget is a usable target for the failing foreign keys below: a
// table that exists and carries a primary key. Every case in this group has to
// fail for the ONE reason it names, and applyForeignKey's guards shadow each
// other - an unresolvable local column returns before the target is looked at
// all - so a case whose fixture tripped an earlier guard would pass while
// proving something else entirely.
const resolvableTarget = "CREATE TABLE `u` (\n" +
	"  `c` int NOT NULL,\n" +
	"  `d` int NOT NULL,\n" +
	"  PRIMARY KEY (`c`)\n" +
	");\n"

// mismatchedForeignKey names two columns and references one.
const mismatchedForeignKey = resolvableTarget +
	"CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL,\n" +
	"  `b` int NOT NULL,\n" +
	"  KEY `ix` (`b`),\n" +
	"  CONSTRAINT `t_fk` FOREIGN KEY (`a`,`b`) REFERENCES `u` (`c`)\n" +
	");"

// TestAForeignKeyWhoseColumnCountsDisagree covers the one guard in either
// importer that has no counterpart on the other side, and it is the only thing
// standing between a self-contradictory foreign key and a document that looks
// fine.
//
// The JSON Schema constrains a foreign key's `columns` and its
// `references.columns` separately and never relates the two lengths, so a key
// naming two columns and referencing one would VALIDATE. It would then draw a
// line in the ER diagram between column sets of different sizes and generate
// DDL no database accepts. internal/check relates the two, but only for a
// document that already exists - it says nothing about what an importer may
// build. So this guard is the whole defence, and nothing had ever run it.
func TestAForeignKeyWhoseColumnCountsDisagree(t *testing.T) {
	doc, warnings := mustImport(t, mismatchedForeignKey)
	if len(warnings) != 1 {
		t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
	}
	want := "table t: foreign key t_fk names 2 column(s) but references 1; not imported"
	if got := warnings[0].Message; got != want {
		t.Errorf("warning got = %q, want %q", got, want)
	}
	tbl := table(t, doc, "t")
	if len(tbl.ForeignKeys) != 0 {
		t.Errorf("foreign keys got = %+v, want none", tbl.ForeignKeys)
	}
	// The neighbours: the table keeps both columns and the index declared
	// beside the dropped key.
	if len(tbl.Columns) != 2 {
		t.Errorf("columns got = %+v, want 2", tbl.Columns)
	}
	if len(tbl.Indexes) != 1 || tbl.Indexes[0].Name != "ix" {
		t.Errorf("indexes got = %+v, want ix", tbl.Indexes)
	}
}

// noPrimaryKeyTarget references a table that has no primary key to resolve
// against.
const noPrimaryKeyTarget = "CREATE TABLE `u` (\n" +
	"  `c` int NOT NULL\n" +
	");\n" +
	"CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL,\n" +
	"  UNIQUE KEY `uq` (`a`),\n" +
	"  CONSTRAINT `t_fk` FOREIGN KEY (`a`) REFERENCES `u`\n" +
	");"

// TestAForeignKeyWithNoColumnListAgainstATargetWithNoPrimaryKey is the failing
// half of TestForeignKeyWithNoColumnListResolvesAgainstThePrimaryKey. REFERENCES
// with no column list means "the primary key of that table", and when there is
// none there is nothing to write down - guessing a column would state a
// relationship the dump never claimed.
func TestAForeignKeyWithNoColumnListAgainstATargetWithNoPrimaryKey(t *testing.T) {
	doc, warnings := mustImport(t, noPrimaryKeyTarget)
	if len(warnings) != 1 {
		t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
	}
	want := "table t: foreign key t_fk omits the referenced columns and u has no primary key; not imported"
	if got := warnings[0].Message; got != want {
		t.Errorf("warning got = %q, want %q", got, want)
	}
	tbl := table(t, doc, "t")
	if len(tbl.ForeignKeys) != 0 {
		t.Errorf("foreign keys got = %+v, want none", tbl.ForeignKeys)
	}
	if len(tbl.UniqueKeys) != 1 {
		t.Errorf("unique keys got = %+v, want the one declared beside the dropped key", tbl.UniqueKeys)
	}
	if table(t, doc, "u") == nil {
		t.Error("table u is missing")
	}
}

// TestForeignKeysThatCannotBeResolved is the same table as the PostgreSQL
// package's test of the same name. The two resolvers are separate files
// implementing one rule, and keeping the tests the same shape is what lets a
// reviewer diff them.
func TestForeignKeysThatCannotBeResolved(t *testing.T) {
	tests := []struct {
		name        string
		constraint  string
		wantMessage string
	}{
		{
			// This row and the next reach one guard: resolvable answers no for
			// two different reasons and the message cannot tell them apart.
			// Both are kept because a change that stopped catching repeats
			// would still pass the first.
			name:        "a local column the table does not have",
			constraint:  "FOREIGN KEY (`nosuch`) REFERENCES `u` (`c`)",
			wantMessage: `table t: foreign key t_fk names unknown or repeated column "nosuch"; not imported`,
		},
		{
			name:        "the same local column twice",
			constraint:  "FOREIGN KEY (`a`,`a`) REFERENCES `u` (`c`,`d`)",
			wantMessage: `table t: foreign key t_fk names unknown or repeated column "a"; not imported`,
		},
		{
			name:        "a referenced column the target does not have",
			constraint:  "FOREIGN KEY (`a`) REFERENCES `u` (`nosuch`)",
			wantMessage: `table t: foreign key t_fk references unknown or repeated column u."nosuch"; not imported`,
		},
		{
			name:        "the same referenced column twice",
			constraint:  "FOREIGN KEY (`a`,`b`) REFERENCES `u` (`c`,`c`)",
			wantMessage: `table t: foreign key t_fk references unknown or repeated column u."c"; not imported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, resolvableTarget+
				"CREATE TABLE `t` (\n"+
				"  `a` int NOT NULL,\n"+
				"  `b` int NOT NULL,\n"+
				"  UNIQUE KEY `uq` (`b`),\n"+
				"  CONSTRAINT `t_fk` "+tt.constraint+"\n"+
				");")
			if len(warnings) != 1 {
				t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
			}
			if got := warnings[0].Message; got != tt.wantMessage {
				t.Errorf("warning got = %q, want %q", got, tt.wantMessage)
			}
			tbl := table(t, doc, "t")
			if len(tbl.ForeignKeys) != 0 {
				t.Errorf("foreign keys got = %+v, want none", tbl.ForeignKeys)
			}
			// The neighbour, declared in the same element list: each failure
			// returns from applyForeignKey alone, and a change that returned one
			// frame higher would take this unique key with it in silence.
			if len(tbl.UniqueKeys) != 1 || tbl.UniqueKeys[0].Name != "uq" {
				t.Errorf("unique keys got = %+v, want uq", tbl.UniqueKeys)
			}
		})
	}
}

// twoPrimaryKeys states a primary key twice, which MySQL rejects and a file
// assembled by hand does not.
const twoPrimaryKeys = "CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL,\n" +
	"  `b` int NOT NULL,\n" +
	"  PRIMARY KEY (`a`),\n" +
	"  PRIMARY KEY (`b`)\n" +
	");"

// TestASecondPrimaryKeyIsNotImported asserts which key survived, not merely
// that one did. The message says which key was dropped and not which was kept,
// so the tie-break - the first one wins - is decided in the code and stated
// nowhere else, and it decides what the document says about the table.
func TestASecondPrimaryKeyIsNotImported(t *testing.T) {
	doc, warnings := mustImport(t, twoPrimaryKeys)
	if len(warnings) != 1 {
		t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
	}
	if want := "table t: a second primary key is not imported"; warnings[0].Message != want {
		t.Errorf("warning got = %q, want %q", warnings[0].Message, want)
	}
	if warnings[0].Line != 5 {
		t.Errorf("warning line got = %v, want 5", warnings[0].Line)
	}
	tbl := table(t, doc, "t")
	if tbl.PrimaryKey == nil || !slices.Equal(tbl.PrimaryKey.Columns, []string{"a"}) {
		t.Fatalf("primary key got = %+v, want the first one, over a", tbl.PrimaryKey)
	}
	// b is not null because its own definition said so, not because of the key
	// that was dropped - which is what makes the two facts distinguishable here.
	if column(t, tbl, "b").Nullable {
		t.Error("column b nullable got = true, want false")
	}
}

// TestAKeyNamingAColumnTheTableDoesNotHave covers the guard both key kinds share.
func TestAKeyNamingAColumnTheTableDoesNotHave(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantMessage string
	}{
		{
			name:        "a primary key over an unknown column",
			key:         "PRIMARY KEY (`nosuch`)",
			wantMessage: `table t: primary key names unknown or repeated column "nosuch"; not imported`,
		},
		{
			name:        "a unique key over an unknown column",
			key:         "UNIQUE KEY `uq` (`nosuch`)",
			wantMessage: `table t: unique key names unknown or repeated column "nosuch"; not imported`,
		},
		{
			name:        "a primary key naming one column twice",
			key:         "PRIMARY KEY (`a`,`a`)",
			wantMessage: `table t: primary key names unknown or repeated column "a"; not imported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, warnings := mustImport(t, "CREATE TABLE `t` (\n"+
				"  `a` int NOT NULL,\n"+
				"  `b` int NOT NULL,\n"+
				"  "+tt.key+",\n"+
				"  KEY `ix` (`b`)\n"+
				");")
			if len(warnings) != 1 {
				t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
			}
			if got := warnings[0].Message; got != tt.wantMessage {
				t.Errorf("warning got = %q, want %q", got, tt.wantMessage)
			}
			tbl := table(t, doc, "t")
			if tbl.PrimaryKey != nil || len(tbl.UniqueKeys) != 0 {
				t.Errorf("keys got = %+v / %+v, want none", tbl.PrimaryKey, tbl.UniqueKeys)
			}
			if len(tbl.Indexes) != 1 || tbl.Indexes[0].Name != "ix" {
				t.Errorf("indexes got = %+v, want ix", tbl.Indexes)
			}
		})
	}
}

// tableWithNoColumns declares an empty table beside a real one. Two tables are
// needed rather than one: a dump in which no table survives is an error rather
// than a warning, so a lone empty table would exercise a different decision.
//
// MySQL rejects a table with no columns, so mysqldump cannot emit this; the
// parser tolerates it on purpose, and this is what the resolver then does with
// it.
const tableWithNoColumns = "CREATE TABLE `empty` (\n" +
	");\n" +
	"CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL,\n" +
	"  PRIMARY KEY (`a`)\n" +
	");\n" +
	"ALTER TABLE `empty` ADD PRIMARY KEY (`a`);\n" +
	"ALTER TABLE `empty` ADD CONSTRAINT `empty_fk` FOREIGN KEY (`a`) REFERENCES `t` (`a`);\n" +
	"ALTER TABLE `empty` ADD KEY `empty_ix` (`a`);"

// TestATableWithNoColumnsIsReportedAndSkipped also states what the importer
// says about the statements that follow the table it skipped. It says nothing:
// the table has been reported once, and repeating the complaint for each of its
// keys and indexes would bury the line that matters under lines that add none.
// The single-warning assertion is where that decision is written down.
func TestATableWithNoColumnsIsReportedAndSkipped(t *testing.T) {
	doc, warnings := mustImport(t, tableWithNoColumns)
	if len(warnings) != 1 {
		t.Fatalf("warnings got =%s, want exactly 1", showWarnings(warnings))
	}
	if want := "table empty has no columns; not imported"; warnings[0].Message != want {
		t.Errorf("warning got = %q, want %q", warnings[0].Message, want)
	}
	if warnings[0].Line != 1 {
		t.Errorf("warning line got = %v, want 1", warnings[0].Line)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "t" {
		t.Fatalf("tables got = %+v, want only t", doc.Tables)
	}
}

// indexesThatCannotBeImported declares three indexes that cannot reach the
// document and, deliberately last, one that can.
//
// The unnamed KEY is the one that needs explaining to anyone who knows MySQL:
// the server names an unnamed key after its first column before the table is
// ever dumped, so mysqldump cannot write one. It arrives from a hand-written
// file, and the document has nowhere to put it because the schema requires an
// index name.
const indexesThatCannotBeImported = "CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL,\n" +
	"  `b` int NOT NULL,\n" +
	"  KEY (`a`),\n" +
	"  KEY `ix_x` (`nosuch`),\n" +
	"  KEY `ix_y` (`a`,`a`),\n" +
	"  KEY `ix_ab` (`a`,`b`)\n" +
	");"

// TestIndexesThatCannotBeImportedAreReportedOneByOne asserts three warnings in
// source order and then the thing the warnings do not say: the good index
// declared after all of them arrived. applyIndexes continues past each failure,
// and a change turning one of those continues into a return would drop every
// index after the first bad one without a word.
func TestIndexesThatCannotBeImportedAreReportedOneByOne(t *testing.T) {
	doc, warnings := mustImport(t, indexesThatCannotBeImported)
	want := []string{
		"line 4: table t: an index without a name cannot be imported",
		`line 5: table t: index ix_x names unknown or repeated column "nosuch"; not imported`,
		`line 6: table t: index ix_y names unknown or repeated column "a"; not imported`,
	}
	if got := messages(warnings); !slices.Equal(got, want) {
		t.Fatalf("warnings got = %v, want %v", got, want)
	}
	tbl := table(t, doc, "t")
	if len(tbl.Indexes) != 1 || tbl.Indexes[0].Name != "ix_ab" ||
		!slices.Equal(tbl.Indexes[0].Columns, []string{"a", "b"}) {
		t.Errorf("indexes got = %+v, want ix_ab over [a b]", tbl.Indexes)
	}
}

// noActionForeignKey states both referential actions as NO ACTION.
const noActionForeignKey = resolvableTarget +
	"CREATE TABLE `t` (\n" +
	"  `a` int NOT NULL\n" +
	");\n" +
	"ALTER TABLE `t` ADD CONSTRAINT `t_fk` FOREIGN KEY (`a`) REFERENCES `u` (`c`)" +
	" ON DELETE NO ACTION ON UPDATE NO ACTION;"

// TestNoActionReachesTheDocument is the resolve half of the parser case in
// stmt_test.go, and a unit test is the only place it can be held. NO ACTION is
// both systems' default, so a round trip through a real server loses it - the
// server stores nothing and mysqldump writes nothing - which means no captured
// fixture can ever carry one. jjf reads it and never writes it back.
func TestNoActionReachesTheDocument(t *testing.T) {
	doc, warnings := mustImport(t, noActionForeignKey)
	if len(warnings) != 0 {
		t.Errorf("warnings got =%s, want none", showWarnings(warnings))
	}
	fks := table(t, doc, "t").ForeignKeys
	if len(fks) != 1 {
		t.Fatalf("foreign keys got = %+v, want 1", fks)
	}
	if fks[0].OnDelete != model.ActionNoAction || fks[0].OnUpdate != model.ActionNoAction {
		t.Errorf("actions got = (%q, %q), want both %q", fks[0].OnDelete, fks[0].OnUpdate, model.ActionNoAction)
	}
}

// TestTheFirstDefinitionOfARepeatedColumnWins is the twin of the PostgreSQL
// test of the same name: a tie-break stated in a comment and decided nowhere
// else. MySQL rejects a table naming a column twice, so this arrives only from a
// file assembled or edited by hand - but when it does, choosing the LAST
// definition would give the column a type the first half of the file
// contradicts.
func TestTheFirstDefinitionOfARepeatedColumnWins(t *testing.T) {
	doc, _ := mustImport(t, "CREATE TABLE `t` (\n"+
		"  `a` int NOT NULL,\n"+
		"  `a` varchar(10)\n"+
		");")
	cols := table(t, doc, "t").Columns
	if len(cols) != 2 {
		t.Fatalf("columns got = %+v, want both definitions kept as written", cols)
	}
	if cols[0].Type != "INTEGER" || cols[0].Nullable {
		t.Errorf("first column got = %+v, want a NOT NULL INTEGER", cols[0])
	}
}

// mustEncode renders a document for a failure message.
func mustEncode(t *testing.T, doc *model.Document) []byte {
	t.Helper()
	raw, err := model.Encode(doc)
	if err != nil {
		t.Fatalf("Encode returned error %v, want no error", err)
	}
	return raw
}

// importedSources lists every dump this package's tests import successfully.
//
// The PostgreSQL package has had this pair since it was written; this one had
// only its committed fixtures held to the schema, so the hand-written sources
// in these files - every one of which is a document this importer can produce -
// were never checked against it. cmd/jjf/import.go states the invariant they
// are being held to: a document jjf itself produced and jjf itself would then
// reject is the worst thing the command could leave behind.
func importedSources() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"mysqldump order", dumpSource},
		{"a whole dump", wholeDump},
		{"a mismatched foreign key", mismatchedForeignKey},
		{"a target with no primary key", noPrimaryKeyTarget},
		{"two primary keys", twoPrimaryKeys},
		{"a table with no columns", tableWithNoColumns},
		{"indexes that cannot be imported", indexesThatCannotBeImported},
		{"on delete no action", noActionForeignKey},
	}
}

// TestImportProducesAValidDocument runs every source above through the JSON
// Schema, which is the one invariant the whole importer exists to preserve. It
// matters most for the sources that deliberately make the importer DROP
// something: a table left with no indexes is fine, a table left with no columns
// is not, and the schema is what knows the difference.
func TestImportProducesAValidDocument(t *testing.T) {
	validator, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator returned error %v, want no error", err)
	}

	for _, tt := range importedSources() {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := mustImport(t, tt.src)
			raw := mustEncode(t, doc)
			if err := validator.Validate(tt.name, raw); err != nil {
				var ide *schema.InvalidDocumentError
				if errors.As(err, &ide) {
					var report strings.Builder
					ide.WriteReport(&report)
					t.Fatalf("the imported document does not conform to the schema:\n%s\n%s", report.String(), raw)
				}
				t.Fatalf("validating the imported document returned error %v, want no error", err)
			}
		})
	}
}
