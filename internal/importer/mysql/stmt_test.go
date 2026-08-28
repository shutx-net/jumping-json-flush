package mysql

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// wholeDump is a whole "mysqldump --no-data" file from MySQL 8.0.46, verbatim
// and unedited, down to the "Dump completed on" line. It is here rather than in
// testdata because a parser test whose input is a Go literal fails legibly: the
// bytes that broke are in the same file as the assertion about them.
//
// It carries, in one input, everything the parser has to survive at once: the
// banner, the /*!40101 SET ... */ preamble, a DROP TABLE and a pair of
// character_set_client wrappers before every table, three CREATE TABLE
// statements with an AUTO_INCREMENT primary key, a unique key, a plain key, an
// inline foreign key and a per-column and per-table COMMENT, a trigger wrapped
// in DELIMITER lines whose body holds semicolons of its own, and the closing
// SET statements.
//
// The captured dumps that a golden test can be run against arrive later, with
// their own generate.sh; this one exists so that the parser meets a real file
// before there is anything to build a document with.
const wholeDump = "" +
	"-- MySQL dump 10.13  Distrib 8.0.46, for Linux (x86_64)\n" +
	"--\n" +
	"-- Host: localhost    Database: jjf_probe5\n" +
	"-- ------------------------------------------------------\n" +
	"-- Server version\t8.0.46-0ubuntu0.24.04.3\n" +
	"\n" +
	"/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;\n" +
	"/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;\n" +
	"/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;\n" +
	"/*!50503 SET NAMES utf8mb4 */;\n" +
	"/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;\n" +
	"/*!40103 SET TIME_ZONE='+00:00' */;\n" +
	"/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;\n" +
	"/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;\n" +
	"/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;\n" +
	"/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;\n" +
	"\n" +
	"--\n" +
	"-- Table structure for table `audit`\n" +
	"--\n" +
	"\n" +
	"DROP TABLE IF EXISTS `audit`;\n" +
	"/*!40101 SET @saved_cs_client     = @@character_set_client */;\n" +
	"/*!50503 SET character_set_client = utf8mb4 */;\n" +
	"CREATE TABLE `audit` (\n" +
	"  `at` datetime NOT NULL,\n" +
	"  `who` varchar(64) NOT NULL\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;\n" +
	"/*!40101 SET character_set_client = @saved_cs_client */;\n" +
	"\n" +
	"--\n" +
	"-- Table structure for table `customers`\n" +
	"--\n" +
	"\n" +
	"DROP TABLE IF EXISTS `customers`;\n" +
	"/*!40101 SET @saved_cs_client     = @@character_set_client */;\n" +
	"/*!50503 SET character_set_client = utf8mb4 */;\n" +
	"CREATE TABLE `customers` (\n" +
	"  `id` bigint NOT NULL AUTO_INCREMENT COMMENT '顧客ID',\n" +
	"  `code` varchar(20) NOT NULL COMMENT '顧客コード',\n" +
	"  `name` varchar(100) NOT NULL,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  UNIQUE KEY `uq_customers_code` (`code`),\n" +
	"  KEY `ix_customers_name` (`name`)\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='顧客';\n" +
	"/*!40101 SET character_set_client = @saved_cs_client */;\n" +
	"\n" +
	"--\n" +
	"-- Table structure for table `orders`\n" +
	"--\n" +
	"\n" +
	"DROP TABLE IF EXISTS `orders`;\n" +
	"/*!40101 SET @saved_cs_client     = @@character_set_client */;\n" +
	"/*!50503 SET character_set_client = utf8mb4 */;\n" +
	"CREATE TABLE `orders` (\n" +
	"  `id` bigint NOT NULL AUTO_INCREMENT,\n" +
	"  `customer_id` bigint NOT NULL,\n" +
	"  `placed_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  KEY `ix_orders_customer` (`customer_id`),\n" +
	"  CONSTRAINT `fk_orders_customer` FOREIGN KEY (`customer_id`) REFERENCES `customers` (`id`) ON DELETE CASCADE\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;\n" +
	"/*!40101 SET character_set_client = @saved_cs_client */;\n" +
	"/*!50003 SET @saved_cs_client      = @@character_set_client */ ;\n" +
	"/*!50003 SET @saved_cs_results     = @@character_set_results */ ;\n" +
	"/*!50003 SET @saved_col_connection = @@collation_connection */ ;\n" +
	"/*!50003 SET character_set_client  = utf8mb4 */ ;\n" +
	"/*!50003 SET character_set_results = utf8mb4 */ ;\n" +
	"/*!50003 SET collation_connection  = utf8mb4_0900_ai_ci */ ;\n" +
	"/*!50003 SET @saved_sql_mode       = @@sql_mode */ ;\n" +
	"/*!50003 SET sql_mode              = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION' */ ;\n" +
	"DELIMITER ;;\n" +
	"/*!50003 CREATE*/ /*!50017 DEFINER=`root`@`localhost`*/ /*!50003 TRIGGER `trg_orders_bi` BEFORE INSERT ON `orders` FOR EACH ROW BEGIN\n" +
	"  IF NEW.placed_at IS NULL THEN\n" +
	"    SET NEW.placed_at = NOW();\n" +
	"  END IF;\n" +
	"END */;;\n" +
	"DELIMITER ;\n" +
	"/*!50003 SET sql_mode              = @saved_sql_mode */ ;\n" +
	"/*!50003 SET character_set_client  = @saved_cs_client */ ;\n" +
	"/*!50003 SET character_set_results = @saved_cs_results */ ;\n" +
	"/*!50003 SET collation_connection  = @saved_col_connection */ ;\n" +
	"/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;\n" +
	"\n" +
	"/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;\n" +
	"/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;\n" +
	"/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;\n" +
	"/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;\n" +
	"/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;\n" +
	"/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;\n" +
	"/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;\n" +
	"\n" +
	"-- Dump completed on 2026-08-26 15:22:23\n"

// TestWholeDumpParsesWithNoDiagnostics is the acceptance test of this phase:
// a real dump of a schema that says nothing jjf cannot hold produces exactly
// the tables it declares and not one warning. Every statement that is not one
// of those three tables - and there are thirty of them - is skipped in silence.
func TestWholeDumpParsesWithNoDiagnostics(t *testing.T) {
	dump, warnings := mustParse(t, wholeDump)

	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	names := make([]string, 0, len(dump.Tables))
	for _, table := range dump.Tables {
		names = append(names, table.Name.String())
	}
	// mysqldump orders tables by name, which is why audit comes first and why a
	// foreign key in orders names a table defined above it rather than below.
	if want := []string{"audit", "customers", "orders"}; !slices.Equal(names, want) {
		t.Errorf("tables got = %v, want %v", names, want)
	}
	if dump.Database != "jjf_probe5" {
		t.Errorf("database got = %q, want %q", dump.Database, "jjf_probe5")
	}
	if dump.ServerVersion != "8.0.46-0ubuntu0.24.04.3" || dump.ServerVersionLine != 5 {
		t.Errorf("server version got = (%q, %v), want (%q, %v)",
			dump.ServerVersion, dump.ServerVersionLine, "8.0.46-0ubuntu0.24.04.3", 5)
	}
	// Everything a dump says about a table is inside its CREATE TABLE, so
	// nothing was contributed by an ALTER TABLE or a standalone CREATE INDEX.
	if len(dump.Constraints) != 0 || len(dump.Indexes) != 0 {
		t.Errorf("statement-level constraints and indexes got = (%v, %v), want (0, 0)",
			len(dump.Constraints), len(dump.Indexes))
	}
}

// TestTriggerBlockIsSkipped states from the parser's side what
// TestLexTriggerBlockIsOneStatement states from the lexer's: the trigger in
// wholeDump contributes nothing and says nothing. A trigger is not a design
// fact, so a warning about it would be noise in every dump taken with the
// default options.
func TestTriggerBlockIsSkipped(t *testing.T) {
	if !strings.Contains(wholeDump, "DELIMITER ;;") {
		t.Fatal("wholeDump no longer contains a trigger block, so this test asserts nothing")
	}
	dump, warnings := mustParse(t, wholeDump)
	if len(dump.Tables) != 3 {
		t.Errorf("tables got = %v, want 3", len(dump.Tables))
	}
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
}

// TestParseCreateTableFromMysqldump reads one table out of wholeDump and checks
// it field for field, which is the only way to know that the columns, the keys
// and the comments all arrived and arrived in order.
func TestParseCreateTableFromMysqldump(t *testing.T) {
	dump, _ := mustParse(t, wholeDump)
	table := dump.table(qname{Name: "customers"})
	if table == nil {
		t.Fatal("table customers not found")
	}
	if table.Line != 38 {
		t.Errorf("table line got = %v, want %v", table.Line, 38)
	}
	if table.Engine != "innodb" {
		t.Errorf("engine got = %q, want %q", table.Engine, "innodb")
	}
	if table.Comment != "顧客" {
		t.Errorf("table comment got = %q, want %q", table.Comment, "顧客")
	}

	cols := []struct {
		name    string
		typ     string
		notNull bool
		auto    bool
		comment string
	}{
		{name: "id", typ: "bigint", notNull: true, auto: true, comment: "顧客ID"},
		{name: "code", typ: "varchar(20)", notNull: true, comment: "顧客コード"},
		{name: "name", typ: "varchar(100)", notNull: true},
	}
	if len(table.Columns) != len(cols) {
		t.Fatalf("columns got = %v, want %v", len(table.Columns), len(cols))
	}
	for i, want := range cols {
		t.Run(want.name, func(t *testing.T) {
			col := table.Columns[i]
			if col.Name != want.name {
				t.Fatalf("column %d name got = %v, want %v", i, col.Name, want.name)
			}
			if got := col.Type.String(); got != want.typ {
				t.Errorf("type got = %v, want %v", got, want.typ)
			}
			if col.NotNull != want.notNull {
				t.Errorf("not null got = %v, want %v", col.NotNull, want.notNull)
			}
			if col.AutoIncrement != want.auto {
				t.Errorf("auto increment got = %v, want %v", col.AutoIncrement, want.auto)
			}
			if col.Comment != want.comment {
				t.Errorf("comment got = %q, want %q", col.Comment, want.comment)
			}
			if col.HasDefault {
				t.Errorf("has default got = true, want false")
			}
		})
	}

	// The primary key and the unique key are constraints; the plain KEY is an
	// index. All three were written inside the CREATE TABLE, in this order.
	if len(table.Constraints) != 2 {
		t.Fatalf("constraints got = %v, want 2", len(table.Constraints))
	}
	if pk := table.Constraints[0]; pk.Kind != constraintPrimaryKey ||
		pk.Name != "" || !slices.Equal(pk.Columns, []string{"id"}) {
		t.Errorf("primary key got = %+v, want an unnamed key over id", pk)
	}
	if uq := table.Constraints[1]; uq.Kind != constraintUnique ||
		uq.Name != "uq_customers_code" || !slices.Equal(uq.Columns, []string{"code"}) {
		t.Errorf("unique key got = %+v, want uq_customers_code over code", uq)
	}
	if len(table.Indexes) != 1 {
		t.Fatalf("indexes got = %v, want 1", len(table.Indexes))
	}
	if ix := table.Indexes[0]; ix.Name != "ix_customers_name" || ix.Unique ||
		!slices.Equal(ix.Columns, []string{"name"}) || !ix.Table.same(table.Name) {
		t.Errorf("index got = %+v, want ix_customers_name over name", ix)
	}

	// The foreign key of the table below names a table defined ABOVE it, which
	// is the forward reference build.go exists to resolve - here in the easy
	// direction, because mysqldump sorts by name and orders comes second.
	orders := dump.table(qname{Name: "orders"})
	if orders == nil {
		t.Fatal("table orders not found")
	}
	fk := orders.Constraints[len(orders.Constraints)-1]
	if fk.Kind != constraintForeign || fk.Name != "fk_orders_customer" ||
		!slices.Equal(fk.Columns, []string{"customer_id"}) ||
		fk.RefTable.Name != "customers" || !slices.Equal(fk.RefColumns, []string{"id"}) ||
		fk.OnDelete != "CASCADE" || fk.OnUpdate != "" {
		t.Errorf("foreign key got = %+v, want fk_orders_customer to customers(id) ON DELETE CASCADE", fk)
	}
}

// TestElementsAreInSourceOrder pins the invariant the whole package rests on.
// Nothing sorts anything here and nothing may: the order of a document's
// columns, keys and indexes is the order the dump wrote them, and a Go map
// ranged over anywhere on this path would take that away.
func TestElementsAreInSourceOrder(t *testing.T) {
	dump, _ := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `z` int NOT NULL,\n"+
		"  `m` int NOT NULL,\n"+
		"  `a` int NOT NULL,\n"+
		"  PRIMARY KEY (`z`),\n"+
		"  UNIQUE KEY `uq_b` (`m`),\n"+
		"  UNIQUE KEY `uq_a` (`a`),\n"+
		"  KEY `ix_z` (`z`),\n"+
		"  KEY `ix_a` (`a`)\n"+
		") ENGINE=InnoDB;")
	table := dump.Tables[0]

	var names []string
	for _, col := range table.Columns {
		names = append(names, col.Name)
	}
	if want := []string{"z", "m", "a"}; !slices.Equal(names, want) {
		t.Errorf("columns got = %v, want %v", names, want)
	}
	var keys []string
	for _, c := range table.Constraints {
		keys = append(keys, c.Name)
	}
	if want := []string{"", "uq_b", "uq_a"}; !slices.Equal(keys, want) {
		t.Errorf("constraints got = %v, want %v", keys, want)
	}
	var indexes []string
	for _, ix := range table.Indexes {
		indexes = append(indexes, ix.Name)
	}
	if want := []string{"ix_z", "ix_a"}; !slices.Equal(indexes, want) {
		t.Errorf("indexes got = %v, want %v", indexes, want)
	}
}

func TestParseColumnTypes(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		words    string
		args     string
		unsigned bool
		zerofill bool
	}{
		{name: "an integer", src: "int", words: "int"},
		{name: "an unsigned integer", src: "int unsigned", words: "int", unsigned: true},
		{name: "a big integer", src: "bigint", words: "bigint"},
		{name: "a boolean, which MySQL stores as a one-digit tinyint", src: "tinyint(1)", words: "tinyint", args: "1"},
		{name: "a variable character type", src: "varchar(255)", words: "varchar", args: "255"},
		{name: "a fixed character type", src: "char(2)", words: "char", args: "2"},
		{name: "a decimal with a scale", src: "decimal(10,2)", words: "decimal", args: "10|2"},
		{name: "an attribute after the arguments", src: "decimal(10,2) unsigned", words: "decimal", args: "10|2", unsigned: true},
		{name: "both attributes after the arguments", src: "tinyint(3) unsigned zerofill", words: "tinyint", args: "3", unsigned: true, zerofill: true},
		{name: "a fractional seconds precision", src: "datetime(3)", words: "datetime", args: "3"},
		{name: "a timestamp with no precision", src: "timestamp", words: "timestamp"},
		{name: "a text type", src: "text", words: "text"},
		{name: "a longer text type", src: "longtext", words: "longtext"},
		{name: "a JSON type", src: "json", words: "json"},
		{name: "a value list", src: "enum('a','b')", words: "enum", args: "a|b"},
		{name: "a value list holding a comma and a doubled quote", src: "enum('b,c','d''e')", words: "enum", args: "b,c|d'e"},
		{name: "a set", src: "set('x','y')", words: "set", args: "x|y"},
		{name: "a double", src: "double", words: "double"},
		{name: "the two-word spelling of a double", src: "double precision", words: "double precision"},
		{name: "a binary type", src: "binary(16)", words: "binary", args: "16"},
		{name: "a variable binary type", src: "varbinary(255)", words: "varbinary", args: "255"},
		{name: "a bit type", src: "bit(1)", words: "bit", args: "1"},
		{name: "a year", src: "year", words: "year"},
		{name: "a geometry type", src: "geometry", words: "geometry"},
		{name: "a character set and a collation after the type", src: "varchar(10) CHARACTER SET latin1 COLLATE latin1_bin", words: "varchar", args: "10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `t` (\n  `c` "+tt.src+"\n);")
			if len(warnings) != 0 {
				t.Errorf("warnings got = %v, want none", messages(warnings))
			}
			typ := dump.Tables[0].Columns[0].Type
			if got := strings.Join(typ.Words, " "); got != tt.words {
				t.Errorf("words got = %q, want %q", got, tt.words)
			}
			// The arguments are joined with a character no MySQL type argument
			// can hold, so that a value list containing a comma stays legible.
			if got := strings.Join(typ.Args, "|"); got != tt.args {
				t.Errorf("args got = %q, want %q", got, tt.args)
			}
			if typ.Unsigned != tt.unsigned {
				t.Errorf("unsigned got = %v, want %v", typ.Unsigned, tt.unsigned)
			}
			if typ.Zerofill != tt.zerofill {
				t.Errorf("zerofill got = %v, want %v", typ.Zerofill, tt.zerofill)
			}
		})
	}
}

// TestAMultiWordTypeDoesNotSwallowTheRestOfTheTable is the MySQL half of what
// #57 names. A type whose parentheses hang off its SECOND word is the shape
// that breaks: parseTypeName stops after the first word, the attribute loop
// meets "varchar" and then "(", and a skip that advances one token at a time
// walks onto the ")" of "(5)" - which the loop reads as the end of the column
// list. Everything after it is then eaten by parseTableOptions in silence.
//
// Every spelling below was accepted by MySQL 8.0.46 before it was written here.
// The assertion is deliberately about the whole table rather than the type: a
// column, a key and a NOT NULL disappearing is the defect, and the type is what
// caused it.
func TestAMultiWordTypeDoesNotSwallowTheRestOfTheTable(t *testing.T) {
	tests := []struct {
		name  string
		typ   string
		words string
		args  string
	}{
		{name: "NATIONAL VARCHAR", typ: "national varchar(5)", words: "national varchar", args: "5"},
		{name: "NATIONAL CHAR", typ: "national char(5)", words: "national char", args: "5"},
		{name: "NATIONAL CHARACTER", typ: "national character(5)", words: "national character", args: "5"},
		{name: "CHARACTER VARYING", typ: "character varying(5)", words: "character varying", args: "5"},
		{name: "CHAR VARYING", typ: "char varying(5)", words: "char varying", args: "5"},
		{name: "NCHAR VARYING", typ: "nchar varying(5)", words: "nchar varying", args: "5"},
		{name: "NATIONAL CHARACTER VARYING", typ: "national character varying(5)", words: "national character varying", args: "5"},
		// The two that carry no parentheses at all: they cost the table
		// nothing today, and they are here so that the words are read whole
		// rather than leaving a column of type LONG.
		{name: "LONG VARCHAR", typ: "long varchar", words: "long varchar"},
		{name: "LONG VARBINARY", typ: "long varbinary", words: "long varbinary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
				"  `j` "+tt.typ+",\n"+
				"  `k` int NOT NULL,\n"+
				"  KEY `ix_k` (`k`)\n);")
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
			if len(tbl.Indexes) != 1 || tbl.Indexes[0].Name != "ix_k" {
				t.Errorf("indexes got = %+v, want one called ix_k", tbl.Indexes)
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

// TestAnUnknownAttributeKeepsItsParenthesesToItself is the same defect without
// the type: whatever an unrecognised attribute carries in parentheses has to be
// skipped whole.
//
// The attribute here is invented, which is the point. The default branch of the
// attribute loop exists so that a dump from a newer MySQL still parses, and an
// attribute jjf has never heard of is exactly what it will meet - so its ")"
// must not be able to end the column list.
func TestAnUnknownAttributeKeepsItsParenthesesToItself(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `j` int somethingnew(3),\n"+
		"  `k` int NOT NULL\n);")
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
}

// TestParseInlineComments covers the whole of what MySQL has instead of a
// COMMENT ON statement: a column says its comment in its own definition and a
// table says its own in the options after the closing parenthesis.
func TestParseInlineComments(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `a` int NOT NULL COMMENT 'it''s here',\n"+
		"  `b` int NOT NULL COMMENT 'two\\nlines'\n"+
		") ENGINE=InnoDB COMMENT='table''s own';")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	if got, want := table.Columns[0].Comment, "it's here"; got != want {
		t.Errorf("column comment got = %q, want %q", got, want)
	}
	// mysqldump writes a newline inside a comment as a backslash escape, which
	// is the reason the lexer resolves them at all.
	if got, want := table.Columns[1].Comment, "two\nlines"; got != want {
		t.Errorf("column comment got = %q, want %q", got, want)
	}
	if got, want := table.Comment, "table's own"; got != want {
		t.Errorf("table comment got = %q, want %q", got, want)
	}
}

// TestTableOptionsAreSkippedSilently is the test that stops a well-meaning
// later change from warning about every dropped option: mysqldump writes four
// or five of these on every table, so one warning each would put a hundred
// warnings in front of the ones that matter in a twenty-table schema.
func TestTableOptionsAreSkippedSilently(t *testing.T) {
	_, warnings := mustParse(t, "CREATE TABLE `t` (\n  `a` int NOT NULL\n"+
		") ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4"+
		" COLLATE=utf8mb4_0900_ai_ci ROW_FORMAT=DYNAMIC STATS_PERSISTENT=1"+
		" KEY_BLOCK_SIZE=8 CHECKSUM=1 MAX_ROWS=1000;")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
}

func TestNonInnoDBEngineWarns(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n  `a` int NOT NULL\n) ENGINE=MyISAM;")
	if len(dump.Tables) != 1 || dump.Tables[0].Engine != "myisam" {
		t.Fatalf("engine got = %+v, want myisam recorded on one table", dump.Tables)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "myisam") {
		t.Fatalf("warnings got = %v, want one naming the engine", messages(warnings))
	}
	if warnings[0].Line != 3 {
		t.Errorf("warning line got = %v, want %v", warnings[0].Line, 3)
	}
}

// TestPartitioningWarns also pins that the columns survive. A partitioned table
// is not the table the document would describe, but its columns are still true,
// so dropping the whole table would lose more than it saved.
func TestPartitioningWarns(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `parts` (\n"+
		"  `id` int NOT NULL,\n"+
		"  `created` date NOT NULL,\n"+
		"  PRIMARY KEY (`id`,`created`)\n"+
		") ENGINE=InnoDB\n"+
		"/*!50100 PARTITION BY RANGE (year(`created`))\n"+
		"(PARTITION p0 VALUES LESS THAN (2020) ENGINE = InnoDB,\n"+
		" PARTITION p1 VALUES LESS THAN MAXVALUE ENGINE = InnoDB) */;")
	if len(dump.Tables) != 1 || len(dump.Tables[0].Columns) != 2 {
		t.Fatalf("tables got = %+v, want one table with two columns", dump.Tables)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "partitioning") {
		t.Fatalf("warnings got = %v, want one about partitioning", messages(warnings))
	}
	if warnings[0].Line != 6 {
		t.Errorf("warning line got = %v, want %v", warnings[0].Line, 6)
	}
}

func TestCheckConstraintIsWarnedAndDropped(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "written as a table element, as mysqldump writes it",
			src: "CREATE TABLE `t` (\n  `qty` int NOT NULL,\n" +
				"  CONSTRAINT `chk_qty` CHECK ((`qty` > 0))\n);",
		},
		{
			name: "written on the column",
			src:  "CREATE TABLE `t` (\n  `qty` int NOT NULL CHECK ((`qty` > 0)) NOT ENFORCED\n);",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			table := dump.Tables[0]
			if len(table.Constraints) != 0 {
				t.Errorf("constraints got = %+v, want none", table.Constraints)
			}
			if len(table.Columns) != 1 {
				t.Errorf("columns got = %v, want 1", len(table.Columns))
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "check constraint") {
				t.Fatalf("warnings got = %v, want one about a check constraint", messages(warnings))
			}
		})
	}
}

// TestAColumnCHECKReadsNotEnforcedWholeOrNotAtAll is the table of what MySQL
// 8.0 accepts after a column CHECK, and of the one thing that separates the
// rows: whether the column is still NOT NULL.
//
// NOT ENFORCED is ONE optional phrase, so it has to be read whole or not at
// all. Read as two independent words, the NOT of a following NOT NULL is taken
// for its head; the ENFORCED that never arrives leaves the NULL behind, the
// explicit-NULL arm swallows it, and the column comes out nullable with no
// warning anywhere. That is the whole defect, and the rows below reach it
// through three different surrounding texts.
//
// Bare ENFORCED is legal SQL and has a row of its own, because reading the
// phrase whole must not cost it. A lone NOT after a CHECK is a syntax error, so
// a NOT there can only begin NOT ENFORCED or NOT NULL - which is why trying the
// phrase whole and abandoning it leaves no third possibility unhandled.
//
// Every row warns about the check constraint, so the warning count cannot tell
// a right answer from a wrong one. It is asserted all the same: it is what says
// the column went through the CHECK arm at all, and without it a row could pin
// the right nullability for a reason that has nothing to do with this arm. The
// default is asserted for the same kind of reason - it is what says the arm
// consumes the phrase and no more, which a fix that reached for skipElement
// would fail.
func TestAColumnCHECKReadsNotEnforcedWholeOrNotAtAll(t *testing.T) {
	tests := []struct {
		name        string
		column      string
		wantNotNull bool
		wantDefault string
	}{
		{
			name:        "NOT NULL after the CHECK",
			column:      "`a` int CHECK ((`a` > 0)) NOT NULL",
			wantNotNull: true,
		},
		{
			name:        "ENFORCED and then NOT NULL",
			column:      "`a` int CHECK ((`a` > 0)) ENFORCED NOT NULL",
			wantNotNull: true,
		},
		{
			name:        "NOT ENFORCED and then NOT NULL",
			column:      "`a` int CHECK ((`a` > 0)) NOT ENFORCED NOT NULL",
			wantNotNull: true,
		},
		{
			name:        "NOT NULL before the CHECK",
			column:      "`a` int NOT NULL CHECK ((`a` > 0))",
			wantNotNull: true,
		},
		{
			name:        "a CONSTRAINT symbol in front of the CHECK",
			column:      "`a` int CONSTRAINT `sym` CHECK ((`a` > 0)) NOT NULL",
			wantNotNull: true,
		},
		{
			name:        "NOT NULL and a DEFAULT after the CHECK",
			column:      "`a` int CHECK ((`a` > 0)) NOT NULL DEFAULT 5",
			wantNotNull: true,
			wantDefault: "5",
		},
		{
			name:   "NOT ENFORCED and nothing after it",
			column: "`a` int CHECK ((`a` > 0)) NOT ENFORCED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `t` (\n  "+tt.column+"\n);")
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1 about the check constraint", messages(warnings))
			}
			cols := dump.Tables[0].Columns
			if len(cols) != 1 {
				t.Fatalf("columns got = %+v, want 1", cols)
			}
			if cols[0].NotNull != tt.wantNotNull {
				t.Errorf("NOT NULL got = %v, want %v", cols[0].NotNull, tt.wantNotNull)
			}
			if cols[0].HasDefault != (tt.wantDefault != "") || cols[0].Default != tt.wantDefault {
				t.Errorf("default got = %q (present = %v), want %q", cols[0].Default, cols[0].HasDefault, tt.wantDefault)
			}
		})
	}
}

// TestFulltextKeyIsWarnedAndDropped and its spatial sibling assert the same
// thing about two different keys: neither is an index over the columns the
// document would name, so importing either as a plain index would describe
// something that does not exist.
func TestFulltextKeyIsWarnedAndDropped(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `body` text NOT NULL,\n  FULLTEXT KEY `ft_body` (`body`)\n) ENGINE=InnoDB;")
	if len(dump.Tables[0].Indexes) != 0 {
		t.Errorf("indexes got = %+v, want none", dump.Tables[0].Indexes)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "full-text index") {
		t.Fatalf("warnings got = %v, want one about a full-text index", messages(warnings))
	}
	if !strings.Contains(warnings[0].Message, "ft_body") {
		t.Errorf("warning got = %q, want it to name the index", warnings[0].Message)
	}
}

func TestSpatialKeyIsWarnedAndDropped(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `pt` point NOT NULL /*!80003 SRID 4326 */,\n  SPATIAL KEY `sp_pt` (`pt`)\n) ENGINE=InnoDB;")
	if len(dump.Tables[0].Indexes) != 0 {
		t.Errorf("indexes got = %+v, want none", dump.Tables[0].Indexes)
	}
	// The SRID attribute arrives unwrapped from its executable comment and is a
	// storage detail, so it is stepped over without a word of its own.
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "spatial index") {
		t.Fatalf("warnings got = %v, want one about a spatial index", messages(warnings))
	}
}

// TestPrefixedIndexIsImportedAndReported is the other half of the pair: a
// prefix length narrows an index that still covers the column the document
// names, so it is kept and the narrowing is reported. Dropping it would discard
// every index on a TEXT column in every real MySQL schema.
func TestPrefixedIndexIsImportedAndReported(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `body` text,\n  KEY `ix_body` (`body`(255))\n) ENGINE=InnoDB;")
	idx := dump.Tables[0].Indexes
	if len(idx) != 1 || !slices.Equal(idx[0].Columns, []string{"body"}) {
		t.Fatalf("indexes got = %+v, want one over body", idx)
	}
	if len(warnings) != 1 ||
		!strings.Contains(warnings[0].Message, "ix_body") ||
		!strings.Contains(warnings[0].Message, `"body"`) {
		t.Fatalf("warnings got = %v, want one naming the index and the column", messages(warnings))
	}
}

// TestExpressionIndexIsDroppedWhole is the case the prefix test is contrasted
// with: a functional index covers something the document cannot name at all.
func TestExpressionIndexIsDroppedWhole(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `name` varchar(64),\n  KEY `ix_expr` ((lower(`name`)))\n) ENGINE=InnoDB;")
	if len(dump.Tables[0].Indexes) != 0 {
		t.Errorf("indexes got = %+v, want none", dump.Tables[0].Indexes)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "expression index") {
		t.Fatalf("warnings got = %v, want one about an expression index", messages(warnings))
	}
}

func TestGeneratedColumnIsImportedAsAnOrdinaryColumn(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `total` int NOT NULL,\n"+
		"  `gen` int GENERATED ALWAYS AS ((`total` + 1)) STORED,\n"+
		"  `virt` int GENERATED ALWAYS AS ((`total` + 2)) VIRTUAL\n) ENGINE=InnoDB;")
	table := dump.Tables[0]
	if len(table.Columns) != 3 {
		t.Fatalf("columns got = %v, want 3", len(table.Columns))
	}
	for _, i := range []int{1, 2} {
		if !table.Columns[i].Generated {
			t.Errorf("column %s generated got = false, want true", table.Columns[i].Name)
		}
		if got := table.Columns[i].Type.String(); got != "int" {
			t.Errorf("column %s type got = %q, want %q", table.Columns[i].Name, got, "int")
		}
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings got = %v, want two", messages(warnings))
	}
}

// TestOnUpdateCurrentTimestampSetsTheFlagAndNotTheDefault is the test that
// stops a well-meaning later change from folding the rule into the default
// text: the exporter copies a default out verbatim, so a default holding
// "CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP" would make the round trip
// emit a script MySQL refuses.
func TestOnUpdateCurrentTimestampSetsTheFlagAndNotTheDefault(t *testing.T) {
	dump, _ := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n"+
		"  `touched_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)\n"+
		") ENGINE=InnoDB;")
	want := []string{"CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP(6)"}
	for i, col := range dump.Tables[0].Columns {
		if !col.OnUpdateCurrentTimestamp {
			t.Errorf("column %s on update flag got = false, want true", col.Name)
		}
		if col.Default != want[i] {
			t.Errorf("column %s default got = %q, want %q", col.Name, col.Default, want[i])
		}
	}
}

func TestAlterTableAddForeignKey(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `orders` (\n  `id` bigint NOT NULL,\n  `user` bigint NOT NULL\n);\n"+
		"ALTER TABLE `orders` ADD CONSTRAINT `fk_orders_user` FOREIGN KEY (`user`)"+
		" REFERENCES `users` (`id`) ON UPDATE CASCADE ON DELETE SET NULL;\n"+
		"ALTER TABLE `orders` ADD FOREIGN KEY (`user`) REFERENCES `users` (`id`) ON DELETE SET DEFAULT;\n"+
		"ALTER TABLE `orders` ADD KEY `ix_orders_user` (`user`);\n"+
		"ALTER TABLE `orders` ENGINE=InnoDB, ALTER INDEX `ix_orders_user` INVISIBLE;\n")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	// A constraint added by ALTER TABLE stays on the dump rather than on the
	// table, because at parse time the table it names may not have been read.
	if len(dump.Tables[0].Constraints) != 0 {
		t.Errorf("table constraints got = %+v, want none", dump.Tables[0].Constraints)
	}
	if len(dump.Constraints) != 2 {
		t.Fatalf("constraints got = %+v, want 2", dump.Constraints)
	}
	first := dump.Constraints[0]
	if first.Name != "fk_orders_user" || first.Table.Name != "orders" ||
		first.RefTable.Name != "users" || first.OnUpdate != "CASCADE" || first.OnDelete != "SET NULL" {
		t.Errorf("first foreign key got = %+v, want fk_orders_user to users", first)
	}
	// SET DEFAULT is recorded like any other action: MySQL 8.0.46 accepts it,
	// stores it and dumps it back, and InnoDB simply never carries it out.
	if second := dump.Constraints[1]; second.Name != "" || second.OnDelete != "SET DEFAULT" {
		t.Errorf("second foreign key got = %+v, want an unnamed key with ON DELETE SET DEFAULT", second)
	}
	if len(dump.Indexes) != 1 || dump.Indexes[0].Name != "ix_orders_user" ||
		!dump.Indexes[0].Table.same(qname{Name: "orders"}) {
		t.Errorf("indexes got = %+v, want ix_orders_user on orders", dump.Indexes)
	}
}

func TestAlterTableAddColumnIsReported(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n  `a` int NOT NULL\n);\n"+
		"ALTER TABLE `t` ADD COLUMN `b` int NOT NULL DEFAULT 0 AFTER `a`;\n"+
		"ALTER TABLE `absent` ADD COLUMN `c` int;\n")
	cols := dump.Tables[0].Columns
	if len(cols) != 2 || cols[1].Name != "b" || cols[1].Default != "0" || !cols[1].NotNull {
		t.Fatalf("columns got = %+v, want a and b with b defaulting to 0", cols)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings got = %v, want two", messages(warnings))
	}
	if !strings.Contains(warnings[0].Message, "end of the column list") {
		t.Errorf("first warning got = %q, want it to say where the column lands", warnings[0].Message)
	}
	if !strings.Contains(warnings[1].Message, "never created") {
		t.Errorf("second warning got = %q, want it to say the table is unknown", warnings[1].Message)
	}
}

// TestStandaloneCreateIndex covers the form jjf's own MySQL writer emits.
// mysqldump never writes one - it puts every index inside the CREATE TABLE that
// owns it - so a script this tool wrote would otherwise be unreadable by it.
func TestStandaloneCreateIndex(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n  `a` int NOT NULL,\n  `b` int NOT NULL\n);\n"+
		"CREATE INDEX `ix_a` ON `t` (`a`);\n"+
		"CREATE UNIQUE INDEX `ix_b` ON `t` (`b`);\n"+
		"CREATE INDEX `ix_using` USING BTREE ON `t` (`a`);\n"+
		"CREATE FULLTEXT INDEX `ft_a` ON `t` (`a`);\n"+
		"CREATE SPATIAL INDEX `sp_a` ON `t` (`a`);\n"+
		"CREATE INDEX `ix_expr` ON `t` (((`a` + 1)));\n")
	if len(dump.Indexes) != 3 {
		t.Fatalf("indexes got = %+v, want 3", dump.Indexes)
	}
	if dump.Indexes[0].Unique || !dump.Indexes[1].Unique {
		t.Errorf("uniqueness got = (%v, %v), want (false, true)", dump.Indexes[0].Unique, dump.Indexes[1].Unique)
	}
	if !dump.Indexes[0].Table.same(qname{Name: "t"}) {
		t.Errorf("table got = %v, want t", dump.Indexes[0].Table)
	}
	// The last three statements each name something that is not an index over
	// the columns the document would write down, and each is dropped with its
	// own sentence. The full-text and spatial forms answer a MATCH ... AGAINST
	// and an R-tree query respectively, and the third indexes an expression;
	// importing any of them as a plain index would describe something the
	// database does not have.
	want := []string{
		"line 8: index ft_a on table t: full-text index is not imported",
		"line 9: index sp_a on table t: spatial index is not imported",
		"line 10: index ix_expr on table t: expression index is not imported",
	}
	if got := messages(warnings); !slices.Equal(got, want) {
		t.Fatalf("warnings got = %v, want %v", got, want)
	}
}

// TestSetPreambleAndLockTablesAreSkipped states the rule D8 rests on: what jjf
// maps is enumerated and everything else is skipped in silence. LOCK TABLES is
// here even though "mysqldump --no-data" does not write it - it appears only
// around the rows of a dump taken with data - because a concatenated or
// hand-made file is a legitimate input.
func TestSetPreambleAndLockTablesAreSkipped(t *testing.T) {
	_, warnings := mustParse(t, "/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;\n"+
		"/*!50503 SET NAMES utf8mb4 */;\n"+
		"/*!40103 SET TIME_ZONE='+00:00' */;\n"+
		"CREATE DATABASE /*!32312 IF NOT EXISTS*/ `shop` /*!40100 DEFAULT CHARACTER SET utf8mb4 */"+
		" /*!80016 DEFAULT ENCRYPTION='N' */;\n"+
		"USE `shop`;\n"+
		"DROP TABLE IF EXISTS `t`;\n"+
		"LOCK TABLES `t` WRITE;\n"+
		"UNLOCK TABLES;\n"+
		"CREATE TABLE `t` (\n  `a` int NOT NULL\n);\n"+
		"/*!50001 CREATE ALGORITHM=UNDEFINED */\n"+
		"/*!50013 DEFINER=`root`@`localhost` SQL SECURITY DEFINER */\n"+
		"/*!50001 VIEW `v` AS select `t`.`a` AS `a` from `t` */;\n"+
		"GRANT SELECT ON `shop`.* TO `reader`@`%`;\n"+
		"/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;\n")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
}

// TestUnknownColumnOptionIsSkippedNotRejected is what keeps a dump from a newer
// MySQL importable: the attribute list is long and still growing, so an
// attribute this parser has never heard of is stepped over rather than refused.
//
// The unknown attributes are written BEFORE the DEFAULT on purpose. An
// attribute after one would be swallowed into the default text, because
// startsColumnAttribute is the single list that says where a default ends and
// an unknown word is by definition not in it. That is the bounded cost of being
// tolerant, and it is stated here so that the next reader does not discover it
// through a corrupted default.
func TestUnknownColumnOptionIsSkippedNotRejected(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `a` int FUTURE_OPTION 7 NOT NULL DEFAULT 0,\n"+
		"  `b` int ANOTHER_ONE NOT NULL STORAGE DISK COLUMN_FORMAT FIXED INVISIBLE\n) ENGINE=InnoDB;")
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
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
}

// TestInlineColumnConstraintsBecomeTableConstraints covers the spellings a
// hand-made file uses and mysqldump never does, so that build.go has one shape
// to resolve rather than two.
//
// REFERENCES is not among them, and the fourth column is here to say so in the
// same place as the three that are: MySQL creates a key for an inline PRIMARY
// KEY, UNIQUE and KEY, and creates nothing at all for an inline REFERENCES.
// TestAnInlineREFERENCESIsWarnedAndDropped has the measurement.
func TestInlineColumnConstraintsBecomeTableConstraints(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `id` int NOT NULL PRIMARY KEY,\n"+
		"  `code` varchar(8) NOT NULL UNIQUE KEY,\n"+
		"  `alt` int KEY,\n"+
		"  `owner` int REFERENCES `users` (`id`) ON DELETE RESTRICT\n);")
	if msgs := messages(warnings); len(msgs) != 1 ||
		!strings.Contains(msgs[0], "inline REFERENCES is not imported") {
		t.Errorf("warnings got = %v, want the one about the inline REFERENCES", msgs)
	}
	table := dump.Tables[0]
	if len(table.Columns) != 4 {
		t.Fatalf("columns got = %v, want 4", len(table.Columns))
	}
	kinds := []constraintKind{constraintPrimaryKey, constraintUnique, constraintPrimaryKey}
	if len(table.Constraints) != len(kinds) {
		t.Fatalf("constraints got = %+v, want %v", table.Constraints, len(kinds))
	}
	for i, want := range kinds {
		if got := table.Constraints[i].Kind; got != want {
			t.Errorf("constraint %d kind got = %v, want %v", i, got, want)
		}
	}
}

// TestAnInlineREFERENCESIsWarnedAndDropped pins what MySQL really does with a
// REFERENCES clause written on a column: it parses the clause and then ignores
// it. Asked on 8.0.46, SHOW CREATE TABLE carries no FOREIGN KEY line, and
// information_schema.REFERENTIAL_CONSTRAINTS has no row - with no warning from
// the server either, and the same answer under MyISAM as under InnoDB. The
// table-level FOREIGN KEY spelling in the same database does create one, which
// is what makes the silence about this one a fact about the clause rather than
// about the engine.
//
// Importing it as a foreign key therefore put a constraint in the document that
// does not exist in the database. The document is what the diagram, the
// workbook and "jjf export ddl" all describe, so the invention travelled to
// every one of them, and nothing said so.
//
// Dropped rather than refused, and warned about rather than dropped in silence:
// this is a clause of a statement jjf maps, which is exactly the case
// "jjf import -h" promises a warning for.
func TestAnInlineREFERENCESIsWarnedAndDropped(t *testing.T) {
	tests := []struct {
		name   string
		column string
	}{
		{name: "the bare clause", column: "`pid` int REFERENCES `parent` (`id`)"},
		{name: "with a referential action", column: "`pid` int REFERENCES `parent` (`id`) ON DELETE CASCADE"},
		// One warning for the whole clause and not one per part of it: the
		// clause is gone, so MATCH FULL is not a second thing to report.
		{name: "with every sub-clause it may carry", column: "`pid` int REFERENCES `parent` (`id`) MATCH FULL ON DELETE CASCADE ON UPDATE SET NULL"},
		{name: "no column list", column: "`pid` int REFERENCES `parent`"},
		{name: "an attribute after the clause", column: "`pid` int REFERENCES `parent` (`id`) NOT NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `child` (\n"+
				"  `id` int NOT NULL,\n"+
				"  "+tt.column+",\n"+
				"  PRIMARY KEY (`id`)\n);")
			table := dump.Tables[0]
			for _, c := range table.Constraints {
				if c.Kind == constraintForeign {
					t.Errorf("constraint got = %+v, want no foreign key: MySQL creates none for an inline REFERENCES", c)
				}
			}
			if msgs := messages(warnings); len(msgs) != 1 ||
				!strings.Contains(msgs[0], "inline REFERENCES is not imported") {
				t.Errorf("warnings got = %v, want exactly one about the inline REFERENCES", msgs)
			}
			// The clause is dropped, not the rest of the column definition:
			// both columns and the primary key are still read.
			var names []string
			for _, c := range table.Columns {
				names = append(names, c.Name)
			}
			if !slices.Equal(names, []string{"id", "pid"}) {
				t.Errorf("columns got = %v, want [id pid]", names)
			}
			if len(table.Constraints) != 1 || table.Constraints[0].Kind != constraintPrimaryKey {
				t.Errorf("constraints got = %+v, want the primary key alone", table.Constraints)
			}
		})
	}
}

// TestATableLevelForeignKeyIsStillImported is the other half of the claim: the
// spelling MySQL does honour keeps working, so the drop above is about the
// inline clause and not about foreign keys.
func TestATableLevelForeignKeyIsStillImported(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `child` (\n"+
		"  `id` int NOT NULL,\n"+
		"  `pid` int DEFAULT NULL,\n"+
		"  PRIMARY KEY (`id`),\n"+
		"  CONSTRAINT `fk_child_parent` FOREIGN KEY (`pid`) REFERENCES `parent` (`id`) ON DELETE CASCADE\n);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	if len(table.Constraints) != 2 {
		t.Fatalf("constraints got = %+v, want the primary key and the foreign key", table.Constraints)
	}
	fk := table.Constraints[1]
	if fk.Kind != constraintForeign || fk.Name != "fk_child_parent" ||
		fk.RefTable.Name != "parent" || !slices.Equal(fk.RefColumns, []string{"id"}) ||
		fk.OnDelete != "CASCADE" {
		t.Errorf("foreign key got = %+v, want fk_child_parent to parent(id)", fk)
	}
}

// TestReservedWordsInBackticksAreColumnsAndTables is why isConstraintStart can
// be a bare word list with no lookahead escape hatch: all nine of its words are
// reserved in MySQL, so a column called one of them is quoted, and a quoted
// name never reaches wordAt.
func TestReservedWordsInBackticksAreColumnsAndTables(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `order` (\n"+
		"  `key` varchar(10) NOT NULL,\n"+
		"  `index` int DEFAULT NULL,\n"+
		"  `check` int DEFAULT NULL,\n"+
		"  `primary` int DEFAULT NULL,\n"+
		"  PRIMARY KEY (`key`)\n) ENGINE=InnoDB;")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	table := dump.Tables[0]
	if table.Name.Name != "order" {
		t.Errorf("table name got = %q, want %q", table.Name.Name, "order")
	}
	var names []string
	for _, col := range table.Columns {
		names = append(names, col.Name)
	}
	if want := []string{"key", "index", "check", "primary"}; !slices.Equal(names, want) {
		t.Errorf("columns got = %v, want %v", names, want)
	}
	if len(table.Constraints) != 1 || table.Constraints[0].Kind != constraintPrimaryKey {
		t.Errorf("constraints got = %+v, want one primary key", table.Constraints)
	}
}

// TestTableWithoutItsOwnDefinitionIsReported covers the two CREATE TABLE forms
// whose columns are stated somewhere this statement does not reach.
func TestTableWithoutItsOwnDefinitionIsReported(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "LIKE", src: "CREATE TABLE `copy` LIKE `orig`;", want: "LIKE"},
		{name: "parenthesised LIKE", src: "CREATE TABLE `copy` (LIKE `orig`);", want: "LIKE"},
		{name: "a query", src: "CREATE TABLE `copy` AS SELECT * FROM `orig`;", want: "query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, tt.src)
			if len(dump.Tables) != 0 {
				t.Errorf("tables got = %+v, want none", dump.Tables)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0].Message, tt.want) {
				t.Fatalf("warnings got = %v, want one mentioning %q", messages(warnings), tt.want)
			}
		})
	}
}

func TestBrokenStatementReportsItsLine(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "a column with no type",
			src:      "CREATE TABLE `t` (\n  `a` int,\n  `b`\n);",
			wantLine: 4,
			wantMsg:  "expected a type name",
		},
		{
			name:     "a foreign key with no REFERENCES",
			src:      "CREATE TABLE `t` (\n  `a` int,\n  FOREIGN KEY (`a`) `other` (`id`)\n);",
			wantLine: 3,
			wantMsg:  "expected REFERENCES",
		},
		{
			name:     "an unclosed table definition",
			src:      "CREATE TABLE `t` (\n  `a` int\n;",
			wantLine: 3,
			wantMsg:  `expected "," or ")" in table definition`,
		},
		{
			name:     "an unknown referential action",
			src:      "CREATE TABLE `t` (\n  `a` int,\n  FOREIGN KEY (`a`) REFERENCES `o` (`id`) ON DELETE MAYBE\n);",
			wantLine: 3,
			wantMsg:  "expected a referential action",
		},
		{
			name:     "an index over nothing",
			src:      "CREATE TABLE `t` (\n  `a` int,\n  KEY `ix` (7)\n);",
			wantLine: 3,
			wantMsg:  "expected an index column",
		},
		{
			name:     "a comment that is not a string",
			src:      "CREATE TABLE `t` (\n  `a` int COMMENT 7\n);",
			wantLine: 2,
			wantMsg:  "expected a string after COMMENT",
		},
		{
			name:     "an ALTER TABLE naming nothing",
			src:      "CREATE TABLE `t` (\n  `a` int\n);\nALTER TABLE ;",
			wantLine: 4,
			wantMsg:  "expected an identifier",
		},
		{
			name:     "a CREATE INDEX with no ON",
			src:      "CREATE INDEX `ix` `t` (`a`);",
			wantLine: 1,
			wantMsg:  "expected ON in CREATE INDEX",
		},
		{
			// ENGINE is the one table option this parser reads rather than
			// steps over, so it is the one that can fail. The message names
			// what was expected, which is what makes it useful on a file that
			// was cut short.
			name:     "a table option with no value",
			src:      "CREATE TABLE `t` (`a` int) ENGINE=;",
			wantLine: 1,
			wantMsg:  "expected an engine name after ENGINE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d diagList
			_, err := parse([]byte(tt.src), &d)
			if err == nil {
				t.Fatalf("parse(%q) returned no error, want %q", tt.src, tt.wantMsg)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error type got = %T, want *syntaxError", err)
			}
			if !strings.Contains(se.Msg, tt.wantMsg) {
				t.Errorf("message got = %q, want it to contain %q", se.Msg, tt.wantMsg)
			}
			if se.Line != tt.wantLine {
				t.Errorf("line got = %v, want %v", se.Line, tt.wantLine)
			}
		})
	}
}

// TestAColumnCharacterSetAndCollationAreConsumed covers the one column
// attribute in this file that mysqldump really does write for a schema the
// captures do not contain. All nine COLLATEs in the committed dumps are the
// TABLE option COLLATE=utf8mb4_0900_ai_ci, which is skipped as a table option
// somewhere else entirely; the COLUMN form appears whenever a column's
// character set differs from its table's, which is ordinary in a schema that
// has grown over several years.
//
// A design document has nowhere to put a collation, so the attribute is
// consumed and forgotten. What the test asserts is that consuming it costs
// nothing else: the collation NAME must be eaten too, or it would fall to the
// default arm and be stepped over one token at a time - which usually survives
// and occasionally does not.
func TestAColumnCharacterSetAndCollationAreConsumed(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `a` varchar(20) CHARACTER SET utf8mb3 COLLATE utf8mb3_bin NOT NULL,\n"+
		"  `b` varchar(20) COLLATE utf8mb4_bin DEFAULT 'x',\n"+
		"  `c` varchar(20) CHARSET latin1 NOT NULL\n) ENGINE=InnoDB;")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	cols := dump.Tables[0].Columns
	if len(cols) != 3 {
		t.Fatalf("columns got = %+v, want 3", cols)
	}
	if got := cols[0].Type.String(); got != "varchar(20)" || !cols[0].NotNull {
		t.Errorf("first column got = %+v, want a NOT NULL varchar(20)", cols[0])
	}
	if cols[1].Default != "'x'" {
		t.Errorf("second column default got = %q, want %q", cols[1].Default, "'x'")
	}
	if !cols[2].NotNull {
		t.Errorf("third column got = %+v, want NOT NULL", cols[2])
	}
}

// TestADefaultEndsWhereACharacterSetBegins is the twin of the PostgreSQL
// package's TestADefaultEndsWhereCOLLATEBegins and pins a coupling rather than
// an arm. Two lists have to agree: parseColumnDefinition consumes COLLATE,
// CHARACTER SET and CHARSET as attributes, and startsColumnAttribute names all
// three among the words that end a DEFAULT expression. If either moved alone
// the document's default would read `'x' COLLATE utf8mb4_bin`, and
// internal/export/ddl copies a default out verbatim, so the round trip would
// emit a script MySQL rejects - with no warning anywhere along the way.
func TestADefaultEndsWhereACharacterSetBegins(t *testing.T) {
	tests := []struct {
		name      string
		attribute string
	}{
		{name: "COLLATE", attribute: "COLLATE utf8mb4_bin"},
		{name: "CHARACTER SET", attribute: "CHARACTER SET utf8mb4"},
		{name: "CHARSET", attribute: "CHARSET utf8mb4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
				"  `a` varchar(20) DEFAULT 'x' "+tt.attribute+" NOT NULL\n);")
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
		})
	}
}

// TestAColumnCONSTRAINTSymbolIsConsumed covers the arm whose comment explains
// why the symbol is thrown away: MySQL allows a name here only in front of a
// column CHECK, and the CHECK itself is dropped, so the name has nothing left
// to belong to. The assertion that carries the weight is about the SECOND
// column - a symbol that was not consumed would leave the parser reading the
// rest of the column list from the wrong place.
//
// NOT NULL is written AFTER the CHECK here, and that is not a stylistic
// choice. That order was once the broken one: the arm ended with two
// independent accepts for NOT and ENFORCED, so the NOT of a following NOT NULL
// was taken for the start of NOT ENFORCED, the NULL left behind was read as an
// explicit NULL, and the column came out nullable with no warning. The whole
// table of what may follow a column CHECK is
// TestAColumnCHECKReadsNotEnforcedWholeOrNotAtAll; the order is kept here so
// that a symbol test cannot go on passing on the easy one.
func TestAColumnCONSTRAINTSymbolIsConsumed(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `a` int CONSTRAINT `sym` CHECK (`a` > 0) NOT NULL,\n"+
		"  `b` int NOT NULL\n);")
	if len(warnings) != 1 {
		t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
	}
	if want := "check constraint is not imported"; !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}
	if warnings[0].Line != 2 {
		t.Errorf("warning line got = %v, want 2", warnings[0].Line)
	}
	table := dump.Tables[0]
	if len(table.Columns) != 2 || !table.Columns[0].NotNull || !table.Columns[1].NotNull {
		t.Fatalf("columns got = %+v, want two NOT NULL columns", table.Columns)
	}
	if len(table.Constraints) != 0 {
		t.Errorf("constraints got = %+v, want none", table.Constraints)
	}
}

// TestAnExplicitNULLIsTheDefaultAlready is the twin of the PostgreSQL test of
// the same name and is worth the same little: the arm consumes a word the
// default arm would consume anyway. What it asserts is that a tool spelling out
// the nullability jjf assumes cannot end up setting NOT NULL.
func TestAnExplicitNULLIsTheDefaultAlready(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (`a` int NULL, `b` int NOT NULL);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	cols := dump.Tables[0].Columns
	if len(cols) != 2 {
		t.Fatalf("columns got = %+v, want 2", cols)
	}
	if cols[0].NotNull {
		t.Error("an explicit NULL set NotNull on the column, want it left false")
	}
	if !cols[1].NotNull {
		t.Error("NOT NULL got = false, want true")
	}
}

// TestMatchClausesAreConsumedWhateverTheySay is the same table as the
// PostgreSQL package's test of the same name, transposed - and this time it is
// MySQL that is catching up: the PostgreSQL MATCH FULL row has existed since
// that importer was written and none of these did, which is worth knowing
// before assuming the MySQL importer is uniformly the better tested of the two.
//
// The row that matters carries no warning. parseReferentialActions is a loop
// whose default arm returns, so a MATCH spelling it failed to consume would end
// the loop and silently drop the ON DELETE written after it. MySQL parses MATCH
// and then ignores it, and InnoDB does not store it, so mysqldump never writes
// one - these arms exist for hand-written SQL, which is exactly where all three
// spellings turn up.
func TestMatchClausesAreConsumedWhateverTheySay(t *testing.T) {
	tests := []struct {
		name        string
		match       string
		wantMessage string
	}{
		{
			name:        "MATCH FULL",
			match:       "MATCH FULL ",
			wantMessage: "foreign key t_fk on table t: MATCH FULL is not imported",
		},
		{
			name:        "MATCH PARTIAL",
			match:       "MATCH PARTIAL ",
			wantMessage: "foreign key t_fk on table t: MATCH PARTIAL is not imported",
		},
		{
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
			dump, warnings := mustParse(t, "ALTER TABLE `t` ADD CONSTRAINT `t_fk` "+
				"FOREIGN KEY (`a`) REFERENCES `u` (`b`) "+tt.match+"ON DELETE CASCADE;")
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

// TestNoActionIsARecognisedReferentialAction covers the one action of the five
// that no captured dump carries: NO ACTION is MySQL's default and mysqldump
// leaves it out. jjf's own DDL writer states it, though, and so does a
// hand-written script, so a parser that did not recognise it would fail on a
// file this tool wrote.
func TestNoActionIsARecognisedReferentialAction(t *testing.T) {
	dump, warnings := mustParse(t, "ALTER TABLE `t` ADD CONSTRAINT `t_fk` "+
		"FOREIGN KEY (`a`) REFERENCES `u` (`b`) ON DELETE NO ACTION ON UPDATE NO ACTION;")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	if len(dump.Constraints) != 1 {
		t.Fatalf("constraints got = %+v, want 1", dump.Constraints)
	}
	c := dump.Constraints[0]
	if c.OnDelete != "NO ACTION" || c.OnUpdate != "NO ACTION" {
		t.Errorf("actions got = (%q, %q), want both %q", c.OnDelete, c.OnUpdate, "NO ACTION")
	}
}

// TestAnExpressionKeyPartDropsTheWholeKey is the counterpart of
// TestExpressionIndexIsDroppedWhole and of TestPrefixedIndexIsImportedAndReported,
// and the three together state the rule: a narrowing that still covers the
// named column is kept and reported, and anything the document cannot name at
// all is dropped - but a KEY is dropped WHOLE where an index may be narrowed,
// because a key that claimed uniqueness over the wrong columns would be a false
// statement about the database, and a foreign key elsewhere may be resting on
// it.
//
// The assertion that says this is the absence of a unique key over [a] alone.
// A version that narrowed the key instead of dropping it would produce exactly
// that, and a test asserting only the warning would not notice. mysqldump emits
// functional key parts from MySQL 8.0.13 on, doubly parenthesised as below.
func TestAnExpressionKeyPartDropsTheWholeKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantMessage string
	}{
		{
			name:        "a unique key",
			key:         "UNIQUE KEY `uq` (`a`,((`b` + 1)))",
			wantMessage: "unique key uq on table t: expression key part is not imported; the key is dropped",
		},
		{
			name:        "a primary key",
			key:         "PRIMARY KEY ((`a` + 1))",
			wantMessage: "primary key on table t: expression key part is not imported; the key is dropped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
				"  `a` int NOT NULL,\n"+
				"  `b` int NOT NULL,\n"+
				"  "+tt.key+",\n"+
				"  KEY `ix` (`a`)\n) ENGINE=InnoDB;")
			if len(warnings) != 1 {
				t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
			}
			if !strings.Contains(warnings[0].Message, tt.wantMessage) {
				t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, tt.wantMessage)
			}
			if warnings[0].Line != 4 {
				t.Errorf("warning line got = %v, want 4", warnings[0].Line)
			}
			table := dump.Tables[0]
			if len(table.Constraints) != 0 {
				t.Errorf("constraints got = %+v, want none: the key is dropped, not narrowed", table.Constraints)
			}
			// The neighbour, declared after the dropped key: the rest of the
			// element list must still be read.
			if len(table.Indexes) != 1 || table.Indexes[0].Name != "ix" {
				t.Errorf("indexes got = %+v, want ix over [a]", table.Indexes)
			}
		})
	}
}

// TestAColumnListFollowedByAQueryKeepsItsColumns is a different statement from
// TestTableWithoutItsOwnDefinitionIsReported's "a query" case, and the two are
// easy to mistake for one. That one is CREATE TABLE t AS SELECT, which states
// no columns at all, so the table is dropped. This one states its columns and
// then fills them from a query: the columns written down are true and stay
// imported, only the query is lost. Two forms, two messages, two outcomes.
func TestAColumnListFollowedByAQueryKeepsItsColumns(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TABLE `t` (\n"+
		"  `a` int NOT NULL\n) AS SELECT `a` FROM `u`;")
	if len(warnings) != 1 {
		t.Fatalf("warnings got = %v, want exactly 1", messages(warnings))
	}
	if want := "table t: the query a table is defined from is not imported"; !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}
	if len(dump.Tables) != 1 {
		t.Fatalf("tables got = %+v, want 1", dump.Tables)
	}
	cols := dump.Tables[0].Columns
	if len(cols) != 1 || cols[0].Name != "a" || !cols[0].NotNull {
		t.Errorf("columns got = %+v, want one NOT NULL column a", cols)
	}
}

// TestATemporaryTableIsATable covers two loops that are one behaviour: classify
// steps over the words between CREATE and TABLE to decide what the statement
// is, and parseCreateTable steps over the same words to read it. They have to
// agree, and one input is the honest way to say so - if only one of them
// skipped TEMPORARY, this statement would either be classified as something
// else and skipped in silence, or be classified right and then fail to parse.
func TestATemporaryTableIsATable(t *testing.T) {
	dump, warnings := mustParse(t, "CREATE TEMPORARY TABLE `t` (`a` int);")
	if len(warnings) != 0 {
		t.Errorf("warnings got = %v, want none", messages(warnings))
	}
	if len(dump.Tables) != 1 || dump.Tables[0].Name.Name != "t" {
		t.Fatalf("tables got = %+v, want one table t", dump.Tables)
	}
}
