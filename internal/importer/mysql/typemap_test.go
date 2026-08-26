package mysql

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// parseType runs the real path from SQL text to a myType, so that these tests
// exercise the parser's idea of a type rather than a hand-built struct. That
// matters more here than it does for the PostgreSQL sibling: MySQL writes its
// attributes AFTER the parentheses, so "decimal(10,2) unsigned" only reaches
// normalizeType correctly if the parser split it correctly first.
func parseType(t *testing.T, sql string) myType {
	t.Helper()
	toks, err := lex([]byte(sql))
	if err != nil {
		t.Fatalf("lex(%q) returned error %v, want no error", sql, err)
	}
	p := &parser{toks: toks[:len(toks)-1]} // drop the eof sentinel
	typ, err := p.parseTypeName()
	if err != nil {
		t.Fatalf("parseTypeName(%q) returned error %v, want no error", sql, err)
	}
	return typ
}

// normalize is the whole phase under test: SQL text in, columnType out.
func normalize(t *testing.T, sql string) (columnType, []Diagnostic) {
	t.Helper()
	var d diagList
	ct, err := normalizeType(parseType(t, sql), "orders.c", &d)
	if err != nil {
		t.Fatalf("normalizeType(%q) returned error %v, want no error", sql, err)
	}
	return ct, d.all()
}

// eqp compares an optional integer attribute, keeping absent and zero apart.
func eqp(got, want *int) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

// show renders an optional integer for a failure message.
func show(v *int) string {
	if v == nil {
		return "absent"
	}
	return strconv.Itoa(*v)
}

// TestCanonicalTypeName walks every spelling the switch lists, in the spelling
// mysqldump writes it: lower case, with the parameters MySQL puts back.
func TestCanonicalTypeName(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		want      string
		length    *int
		precision *int
		scale     *int
	}{
		{name: "varchar with a length", sql: "varchar(255)", want: "VARCHAR", length: intp(255)},
		{name: "char with a length", sql: "char(36)", want: "CHAR", length: intp(36)},
		{name: "character is a synonym for char", sql: "character(1)", want: "CHAR", length: intp(1)},
		{name: "binary", sql: "binary(16)", want: "BINARY", length: intp(16)},
		{name: "varbinary", sql: "varbinary(255)", want: "VARBINARY", length: intp(255)},
		{name: "bit", sql: "bit(8)", want: "BIT", length: intp(8)},
		{name: "decimal with precision and scale", sql: "decimal(10,2)", want: "DECIMAL", precision: intp(10), scale: intp(2)},
		{name: "decimal with a precision alone", sql: "decimal(10)", want: "DECIMAL", precision: intp(10)},
		{name: "decimal with a zero scale", sql: "decimal(10,0)", want: "DECIMAL", precision: intp(10), scale: intp(0)},
		{name: "numeric is a synonym for decimal", sql: "numeric(5,3)", want: "DECIMAL", precision: intp(5), scale: intp(3)},
		{name: "dec is a synonym for decimal", sql: "dec(5,3)", want: "DECIMAL", precision: intp(5), scale: intp(3)},
		{name: "fixed is a synonym for decimal", sql: "fixed(5,3)", want: "DECIMAL", precision: intp(5), scale: intp(3)},
		{name: "datetime with a precision", sql: "datetime(3)", want: "DATETIME", precision: intp(3)},
		{name: "datetime bare", sql: "datetime", want: "DATETIME"},
		{name: "timestamp with a precision", sql: "timestamp(6)", want: "TIMESTAMP", precision: intp(6)},
		{name: "time with a precision", sql: "time(3)", want: "TIME", precision: intp(3)},
		{name: "int", sql: "int", want: "INTEGER"},
		{name: "integer", sql: "integer", want: "INTEGER"},
		{name: "tinyint", sql: "tinyint", want: "TINYINT"},
		{name: "smallint", sql: "smallint", want: "SMALLINT"},
		{name: "mediumint", sql: "mediumint", want: "MEDIUMINT"},
		{name: "bigint", sql: "bigint", want: "BIGINT"},
		{name: "float", sql: "float", want: "FLOAT"},
		{name: "double", sql: "double", want: "DOUBLE"},
		{name: "double precision", sql: "double precision", want: "DOUBLE"},
		{name: "real", sql: "real", want: "DOUBLE"},
		{name: "bool", sql: "bool", want: "BOOLEAN"},
		{name: "boolean", sql: "boolean", want: "BOOLEAN"},
		{name: "date", sql: "date", want: "DATE"},
		{name: "year", sql: "year", want: "YEAR"},
		{name: "json", sql: "json", want: "JSON"},
		{name: "tinytext", sql: "tinytext", want: "TINYTEXT"},
		{name: "text", sql: "text", want: "TEXT"},
		{name: "mediumtext", sql: "mediumtext", want: "MEDIUMTEXT"},
		{name: "longtext", sql: "longtext", want: "LONGTEXT"},
		{name: "tinyblob", sql: "tinyblob", want: "TINYBLOB"},
		{name: "blob", sql: "blob", want: "BLOB"},
		{name: "mediumblob", sql: "mediumblob", want: "MEDIUMBLOB"},
		{name: "longblob", sql: "longblob", want: "LONGBLOB"},
		{name: "geometry is not listed and is upper-cased", sql: "geometry", want: "GEOMETRY"},
		{name: "point is not listed and is upper-cased", sql: "point", want: "POINT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := normalize(t, tt.sql)
			if got.Name != tt.want {
				t.Errorf("normalizeType(%q) name got = %v, want %v", tt.sql, got.Name, tt.want)
			}
			if !eqp(got.Length, tt.length) {
				t.Errorf("normalizeType(%q) length got = %v, want %v", tt.sql, show(got.Length), show(tt.length))
			}
			if !eqp(got.Precision, tt.precision) {
				t.Errorf("normalizeType(%q) precision got = %v, want %v", tt.sql, show(got.Precision), show(tt.precision))
			}
			if !eqp(got.Scale, tt.scale) {
				t.Errorf("normalizeType(%q) scale got = %v, want %v", tt.sql, show(got.Scale), show(tt.scale))
			}
			// The schema's dependentRequired rule: a scale may never appear
			// without a precision.
			if got.Scale != nil && got.Precision == nil {
				t.Errorf("normalizeType(%q) produced a scale without a precision", tt.sql)
			}
		})
	}
}

// TestUnsignedStaysInTheName pins the decision that an attribute is part of the
// type NAME. Dropping it would describe a column holding a different range of
// values, and internal/export/ddl/mysql.go's myAttributeSuffix splits exactly
// these spellings back off, so the two have to agree.
func TestUnsignedStaysInTheName(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		want      string
		length    *int
		precision *int
		scale     *int
	}{
		{name: "bigint unsigned", sql: "bigint unsigned", want: "BIGINT UNSIGNED"},
		{name: "decimal keeps its parameters under the attribute", sql: "decimal(10,2) unsigned",
			want: "DECIMAL UNSIGNED", precision: intp(10), scale: intp(2)},
		{name: "int unsigned zerofill", sql: "int unsigned zerofill", want: "INTEGER UNSIGNED ZEROFILL"},
		{name: "zerofill alone", sql: "int zerofill", want: "INTEGER ZEROFILL"},
		{name: "the display width zerofill forces back is still dropped", sql: "tinyint(3) unsigned zerofill",
			want: "TINYINT UNSIGNED ZEROFILL", length: intp(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := normalize(t, tt.sql)
			if got.Name != tt.want {
				t.Errorf("normalizeType(%q) name got = %v, want %v", tt.sql, got.Name, tt.want)
			}
			if !eqp(got.Length, tt.length) {
				t.Errorf("normalizeType(%q) length got = %v, want %v", tt.sql, show(got.Length), show(tt.length))
			}
			if !eqp(got.Precision, tt.precision) {
				t.Errorf("normalizeType(%q) precision got = %v, want %v", tt.sql, show(got.Precision), show(tt.precision))
			}
			if !eqp(got.Scale, tt.scale) {
				t.Errorf("normalizeType(%q) scale got = %v, want %v", tt.sql, show(got.Scale), show(tt.scale))
			}
			// An attribute is not a loss, so nothing is said about it.
			if len(warnings) != 0 {
				t.Errorf("normalizeType(%q) warnings got = %v, want none", tt.sql, warnings)
			}
		})
	}
}

// TestTinyintOneKeepsItsLength is the test that stops a well-meaning later
// change from recognising tinyint(1) as BOOLEAN. MySQL stores a BOOLEAN as
// tinyint(1) and mysqldump writes tinyint(1) back, so a document saying BOOLEAN
// would name a type the dump did not - and the round trip would never reach a
// fixed point, because exporting BOOLEAN produces a database that dumps
// tinyint(1) again.
func TestTinyintOneKeepsItsLength(t *testing.T) {
	got, warnings := normalize(t, "tinyint(1)")
	if got.Name != "TINYINT" {
		t.Errorf("normalizeType(\"tinyint(1)\") name got = %v, want TINYINT", got.Name)
	}
	if !eqp(got.Length, intp(1)) {
		t.Errorf("normalizeType(\"tinyint(1)\") length got = %v, want 1", show(got.Length))
	}
	if len(warnings) != 0 {
		t.Errorf("normalizeType(\"tinyint(1)\") warnings got = %v, want none", warnings)
	}

	// The other direction: a dump that spells BOOLEAN out keeps the word it
	// used, rather than being rewritten to TINYINT.
	spelled, _ := normalize(t, "boolean")
	if spelled.Name != "BOOLEAN" {
		t.Errorf("normalizeType(\"boolean\") name got = %v, want BOOLEAN", spelled.Name)
	}
}

// TestEnumAndSetDropTheirValuesWithAWarning covers the one MySQL parenthesis
// that holds authored content. The type survives, because every ENUM column
// that ever reaches this importer is in the same state - including one jjf
// itself produced - so refusing it would refuse jjf's own output.
func TestEnumAndSetDropTheirValuesWithAWarning(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "enum", sql: "enum('pending','paid','shipped')", want: "ENUM"},
		{name: "set", sql: "set('a','b')", want: "SET"},
		{name: "a value list holding a comma", sql: "enum('a,b','c')", want: "ENUM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := normalize(t, tt.sql)
			if got.Name != tt.want {
				t.Errorf("normalizeType(%q) name got = %v, want %v", tt.sql, got.Name, tt.want)
			}
			if got.Length != nil || got.Precision != nil || got.Scale != nil {
				t.Errorf("normalizeType(%q) recorded a numeric attribute for a value list", tt.sql)
			}
			if len(warnings) != 1 {
				t.Fatalf("normalizeType(%q) warnings got = %v, want exactly 1", tt.sql, warnings)
			}
			if want := "the value list of type"; !strings.Contains(warnings[0].Message, want) {
				t.Errorf("normalizeType(%q) warning got = %q, want it to contain %q",
					tt.sql, warnings[0].Message, want)
			}
		})
	}
}

// TestDatetimePrecisionIsAPrecisionAndNotALength is the MySQL face of the one
// sentence its PostgreSQL sibling calls most likely to save a reader an hour:
// varchar(3) is three characters and datetime(3) is three fractional-second
// digits, and recording either as the other describes a different column.
func TestDatetimePrecisionIsAPrecisionAndNotALength(t *testing.T) {
	for _, sql := range []string{"datetime(3)", "timestamp(3)", "time(3)"} {
		t.Run(sql, func(t *testing.T) {
			got, _ := normalize(t, sql)
			if got.Length != nil {
				t.Errorf("normalizeType(%q) length got = %v, want it absent", sql, show(got.Length))
			}
			if !eqp(got.Precision, intp(3)) {
				t.Errorf("normalizeType(%q) precision got = %v, want 3", sql, show(got.Precision))
			}
		})
	}
	length, _ := normalize(t, "varchar(3)")
	if !eqp(length.Length, intp(3)) || length.Precision != nil {
		t.Errorf("normalizeType(\"varchar(3)\") got length %v precision %v, want 3 and absent",
			show(length.Length), show(length.Precision))
	}
}

// TestScaleWithoutPrecisionIsDropped covers the schema's dependentRequired rule
// from the one direction that can reach it: an unreadable precision drops the
// scale with it, because a scale alone is a document the schema rejects.
func TestScaleWithoutPrecisionIsDropped(t *testing.T) {
	got, warnings := normalize(t, "decimal(0,2)")
	if got.Precision != nil {
		t.Errorf("precision got = %v, want it absent", show(got.Precision))
	}
	if got.Scale != nil {
		t.Errorf("scale got = %v, want it dropped with the precision", show(got.Scale))
	}
	if len(warnings) == 0 {
		t.Error("warnings got = none, want one about the precision")
	}
}

func TestUnknownTypeIsUpperCased(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{sql: "geometry", want: "GEOMETRY"},
		{sql: "multipolygon", want: "MULTIPOLYGON"},
		{sql: "geomcollection", want: "GEOMCOLLECTION"},
		{sql: "vector", want: "VECTOR"},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			got, warnings := normalize(t, tt.sql)
			if got.Name != tt.want {
				t.Errorf("normalizeType(%q) name got = %v, want %v", tt.sql, got.Name, tt.want)
			}
			if len(warnings) != 0 {
				t.Errorf("normalizeType(%q) warnings got = %v, want none", tt.sql, warnings)
			}
		})
	}

	// A type jjf does not know still loses its parameters, and says so: the
	// document has nowhere to record what geometry(Point,4326) meant.
	got, warnings := normalize(t, "geometry(9)")
	if got.Name != "GEOMETRY" {
		t.Errorf("normalizeType(\"geometry(9)\") name got = %v, want GEOMETRY", got.Name)
	}
	if len(warnings) != 1 {
		t.Fatalf("normalizeType(\"geometry(9)\") warnings got = %v, want exactly 1", warnings)
	}
}

// TestDisplayWidthIsNotRepresented covers the parameter MySQL itself is
// removing: mysqldump has not written int(11) since 8.0.17, so a width can only
// arrive from an older dump or a hand-written file, and writing it back would
// emit a construct the server has deprecated.
func TestDisplayWidthIsNotRepresented(t *testing.T) {
	got, warnings := normalize(t, "int(11)")
	if got.Name != "INTEGER" {
		t.Errorf("normalizeType(\"int(11)\") name got = %v, want INTEGER", got.Name)
	}
	if got.Length != nil {
		t.Errorf("normalizeType(\"int(11)\") length got = %v, want it absent", show(got.Length))
	}
	if len(warnings) != 1 {
		t.Fatalf("normalizeType(\"int(11)\") warnings got = %v, want exactly 1", warnings)
	}
	if want := "parameters of type int are not represented"; !strings.Contains(warnings[0].Message, want) {
		t.Errorf("warning got = %q, want it to contain %q", warnings[0].Message, want)
	}
}

func TestNormalizeTypeWarnings(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantName    string
		wantMessage string
	}{
		{
			name:        "a non-numeric length",
			sql:         "varchar(many)",
			wantName:    "VARCHAR",
			wantMessage: `length "many" is not a number`,
		},
		{
			name:        "a non-positive length",
			sql:         "varchar(0)",
			wantName:    "VARCHAR",
			wantMessage: "length 0 is out of range",
		},
		{
			name:        "an extra parameter on a length type",
			sql:         "varchar(10,2)",
			wantName:    "VARCHAR",
			wantMessage: "extra parameters of type varchar are not represented",
		},
		{
			name:        "an extra parameter on a time type",
			sql:         "datetime(3,1)",
			wantName:    "DATETIME",
			wantMessage: "extra parameters of type datetime are not represented",
		},
		{
			name:        "a third parameter on decimal",
			sql:         "decimal(10,2,1)",
			wantName:    "DECIMAL",
			wantMessage: "extra parameters of type decimal are not represented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := normalize(t, tt.sql)
			if got.Name != tt.wantName {
				t.Errorf("normalizeType(%q) name got = %v, want %v", tt.sql, got.Name, tt.wantName)
			}
			if len(warnings) != 1 {
				t.Fatalf("normalizeType(%q) warnings got = %v, want exactly 1", tt.sql, warnings)
			}
			if !strings.Contains(warnings[0].Message, tt.wantMessage) {
				t.Errorf("normalizeType(%q) warning got = %q, want it to contain %q",
					tt.sql, warnings[0].Message, tt.wantMessage)
			}
		})
	}
}

func TestTypeNameTooLongIsAnError(t *testing.T) {
	sql := "`" + strings.Repeat("a", maxTypeNameLength+6) + "`"
	var d diagList
	_, err := normalizeType(parseType(t, sql), "orders.c", &d)
	if err == nil {
		t.Fatal("normalizeType returned no error, want one")
	}
	if want := "is longer than 64 characters"; !strings.Contains(err.Error(), want) {
		t.Errorf("message got = %q, want it to contain %q", err.Error(), want)
	}
}

// TestTypeNameThatCannotMatchThePatternIsAnError covers the case a truncation
// would hide. isSchemaTypeName is checked inside normalizeType, naming the
// column and the dump line, so that the schema validation cmd/jjf runs over the
// finished document is a net rather than the diagnosis.
func TestTypeNameThatCannotMatchThePatternIsAnError(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{
			name:    "a hyphen",
			sql:     "`my-type`",
			wantMsg: "orders.c: type my-type cannot be written to a design document",
		},
		{
			name:    "a name that is not ASCII",
			sql:     "`ユーザー種別`",
			wantMsg: "cannot be written to a design document",
		},
		{
			name:    "a name starting with a digit",
			sql:     "`2d`",
			wantMsg: "cannot be written to a design document",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d diagList
			_, err := normalizeType(parseType(t, tt.sql), "orders.c", &d)
			if err == nil {
				t.Fatalf("normalizeType(%q) returned no error, want %q", tt.sql, tt.wantMsg)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("normalizeType(%q) error type got = %T, want *syntaxError", tt.sql, err)
			}
			if !strings.Contains(se.Msg, tt.wantMsg) {
				t.Errorf("normalizeType(%q) message got = %q, want it to contain %q", tt.sql, se.Msg, tt.wantMsg)
			}
		})
	}
}

// schemaTypePattern reads the columnType pattern out of the embedded schema, so
// that this test cannot drift away from the document it is protecting. A regexp
// is used HERE and nowhere in the package it tests: the point is to check the
// hand-written loop against the pattern the schema actually states.
func schemaTypePattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	var doc struct {
		Defs struct {
			ColumnType struct {
				Pattern   string `json:"pattern"`
				MaxLength int    `json:"maxLength"`
			} `json:"columnType"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemadata.DBDesign, &doc); err != nil {
		t.Fatalf("parse the embedded schema: %v", err)
	}
	if doc.Defs.ColumnType.Pattern == "" {
		t.Fatal("the embedded schema has no columnType pattern")
	}
	if doc.Defs.ColumnType.MaxLength != maxTypeNameLength {
		t.Errorf("maxTypeNameLength got = %v, want %v", maxTypeNameLength, doc.Defs.ColumnType.MaxLength)
	}
	return regexp.MustCompile(doc.Defs.ColumnType.Pattern)
}

// TestNormalizeTypeMatchesSchemaPattern is the test that keeps the importer from
// writing a document the schema rejects: every name the table can produce, on
// its own and under each attribute, has to match the pattern the schema states.
func TestNormalizeTypeMatchesSchemaPattern(t *testing.T) {
	pattern := schemaTypePattern(t)

	inputs := []string{
		"varchar(255)", "char(1)", "character(1)", "binary(16)", "varbinary(255)", "bit(8)",
		"decimal(10,2)", "numeric(10,2)", "dec(10,2)", "fixed(10,2)",
		"datetime(3)", "timestamp(6)", "time(3)", "date", "year",
		"int", "integer", "tinyint(1)", "smallint", "mediumint", "bigint",
		"float", "double", "double precision", "real",
		"bool", "boolean", "json",
		"tinytext", "text", "mediumtext", "longtext",
		"tinyblob", "blob", "mediumblob", "longblob",
		"enum('a','b')", "set('a','b')",
		"geometry", "point", "linestring", "polygon", "multipoint",
		"multilinestring", "multipolygon", "geomcollection",
	}

	for _, in := range inputs {
		for _, sql := range []string{in, in + " unsigned", in + " unsigned zerofill"} {
			t.Run(sql, func(t *testing.T) {
				got, _ := normalize(t, sql)
				if !pattern.MatchString(got.Name) {
					t.Errorf("normalizeType(%q) name got = %q, want a match of %v", sql, got.Name, pattern)
				}
				if len(got.Name) > maxTypeNameLength {
					t.Errorf("normalizeType(%q) name got = %d characters, want at most %d",
						sql, len(got.Name), maxTypeNameLength)
				}
			})
		}
	}
}
