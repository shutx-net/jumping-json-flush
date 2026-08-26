package mysql

import (
	"errors"
	"strings"
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
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
