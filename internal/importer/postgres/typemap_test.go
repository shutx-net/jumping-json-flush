package postgres

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// parseType runs the real path from SQL text to a pgType, so that these tests
// exercise the parser's idea of a type rather than a hand-built struct.
func parseType(t *testing.T, sql string) pgType {
	t.Helper()
	toks, err := lex([]byte(sql))
	if err != nil {
		t.Fatalf("lex(%q) returned error %v, want no error", sql, err)
	}
	var d diagList
	p := &parser{toks: toks[:len(toks)-1]} // drop the eof sentinel
	typ, err := p.parseTypeName(&d)
	if err != nil {
		t.Fatalf("parseTypeName(%q) returned error %v, want no error", sql, err)
	}
	return typ
}

// normalize is the whole phase under test: SQL text in, columnType out.
func normalize(t *testing.T, sql string) (columnType, []Diagnostic) {
	t.Helper()
	var d diagList
	ct, err := normalizeType(parseType(t, sql), "users.c", &d)
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

func TestNormalizeType(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		want      string
		length    *int
		precision *int
		scale     *int
		auto      bool
	}{
		{name: "varchar with a length", sql: "character varying(255)", want: "VARCHAR", length: intp(255)},
		{name: "varchar without a length", sql: "character varying", want: "VARCHAR"},
		{name: "varchar alias", sql: "varchar(10)", want: "VARCHAR", length: intp(10)},
		{name: "char with a length", sql: "character(1)", want: "CHAR", length: intp(1)},
		{name: "char without a length", sql: "char", want: "CHAR"},
		{name: "bpchar", sql: "bpchar(4)", want: "CHAR", length: intp(4)},
		{name: "bit", sql: "bit(8)", want: "BIT", length: intp(8)},
		{name: "bit varying", sql: "bit varying(64)", want: "BIT VARYING", length: intp(64)},
		{name: "varbit", sql: "varbit", want: "BIT VARYING"},
		{name: "numeric with precision and scale", sql: "numeric(10,2)", want: "NUMERIC", precision: intp(10), scale: intp(2)},
		{name: "numeric with a precision", sql: "numeric(10)", want: "NUMERIC", precision: intp(10)},
		{name: "numeric bare", sql: "numeric", want: "NUMERIC"},
		{name: "numeric with a zero scale", sql: "numeric(10,0)", want: "NUMERIC", precision: intp(10), scale: intp(0)},
		{name: "decimal", sql: "decimal(5,3)", want: "NUMERIC", precision: intp(5), scale: intp(3)},
		{name: "timestamp with a precision", sql: "timestamp(3) without time zone", want: "TIMESTAMP", precision: intp(3)},
		{name: "timestamp", sql: "timestamp without time zone", want: "TIMESTAMP"},
		{name: "timestamptz", sql: "timestamp with time zone", want: "TIMESTAMPTZ"},
		{name: "timestamptz alias", sql: "timestamptz", want: "TIMESTAMPTZ"},
		{name: "bare timestamp", sql: "timestamp", want: "TIMESTAMP"},
		{name: "time", sql: "time without time zone", want: "TIME"},
		{name: "timetz with a precision", sql: "time(6) with time zone", want: "TIMETZ", precision: intp(6)},
		{name: "timetz alias", sql: "timetz", want: "TIMETZ"},
		{name: "interval", sql: "interval", want: "INTERVAL"},
		{name: "interval with a precision", sql: "interval(6)", want: "INTERVAL", precision: intp(6)},
		{name: "interval with a qualifier", sql: "interval day to second(6)", want: "INTERVAL", precision: intp(6)},
		{name: "integer", sql: "integer", want: "INTEGER"},
		{name: "int4", sql: "int4", want: "INTEGER"},
		{name: "bigint", sql: "bigint", want: "BIGINT"},
		{name: "int8", sql: "int8", want: "BIGINT"},
		{name: "smallint", sql: "smallint", want: "SMALLINT"},
		{name: "int2", sql: "int2", want: "SMALLINT"},
		{name: "boolean", sql: "boolean", want: "BOOLEAN"},
		{name: "bool", sql: "bool", want: "BOOLEAN"},
		{name: "double precision", sql: "double precision", want: "DOUBLE PRECISION"},
		{name: "float8", sql: "float8", want: "DOUBLE PRECISION"},
		{name: "real", sql: "real", want: "REAL"},
		{name: "float4", sql: "float4", want: "REAL"},
		{name: "text", sql: "text", want: "TEXT"},
		{name: "bytea", sql: "bytea", want: "BYTEA"},
		{name: "uuid", sql: "uuid", want: "UUID"},
		{name: "json", sql: "json", want: "JSON"},
		{name: "jsonb", sql: "jsonb", want: "JSONB"},
		{name: "date", sql: "date", want: "DATE"},
		{name: "money", sql: "money", want: "MONEY"},
		{name: "inet", sql: "inet", want: "INET"},
		{name: "cidr", sql: "cidr", want: "CIDR"},
		{name: "macaddr", sql: "macaddr", want: "MACADDR"},
		{name: "macaddr8", sql: "macaddr8", want: "MACADDR8"},
		{name: "xml", sql: "xml", want: "XML"},
		{name: "tsvector", sql: "tsvector", want: "TSVECTOR"},
		{name: "tsquery", sql: "tsquery", want: "TSQUERY"},
		{name: "oid", sql: "oid", want: "OID"},
		{name: "name", sql: "name", want: "NAME"},
		{name: "pg_lsn", sql: "pg_lsn", want: "PG_LSN"},
		{name: "txid_snapshot", sql: "txid_snapshot", want: "TXID_SNAPSHOT"},
		{name: "serial", sql: "serial", want: "INTEGER", auto: true},
		{name: "serial4", sql: "serial4", want: "INTEGER", auto: true},
		{name: "bigserial", sql: "bigserial", want: "BIGINT", auto: true},
		{name: "serial8", sql: "serial8", want: "BIGINT", auto: true},
		{name: "smallserial", sql: "smallserial", want: "SMALLINT", auto: true},
		{name: "serial2", sql: "serial2", want: "SMALLINT", auto: true},
		{name: "point", sql: "point", want: "POINT"},
		{name: "int4range", sql: "int4range", want: "INT4RANGE"},
		{name: "int8multirange", sql: "int8multirange", want: "INT8MULTIRANGE"},
		{name: "enum in the default schema", sql: "public.order_status", want: "ORDER_STATUS"},
		{name: "varchar array", sql: "character varying(255)[]", want: "VARCHAR ARRAY", length: intp(255)},
		{name: "text array", sql: "text[]", want: "TEXT ARRAY"},
		{name: "double precision array", sql: "double precision[]", want: "DOUBLE PRECISION ARRAY"},
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
			if got.AutoIncrement != tt.auto {
				t.Errorf("normalizeType(%q) auto increment got = %v, want %v", tt.sql, got.AutoIncrement, tt.auto)
			}
			// The schema's dependentRequired rule: a scale may never appear
			// without a precision.
			if got.Scale != nil && got.Precision == nil {
				t.Errorf("normalizeType(%q) produced a scale without a precision", tt.sql)
			}
		})
	}

	// TIMESTAMP and TIMESTAMPTZ describe different data and must never
	// collapse into one another.
	withZone, _ := normalize(t, "timestamp with time zone")
	withoutZone, _ := normalize(t, "timestamp without time zone")
	if withZone.Name == withoutZone.Name {
		t.Errorf("timestamp with and without time zone both map to %v", withZone.Name)
	}
}

// schemaTypePattern reads the columnType pattern out of the embedded schema, so
// that this test cannot drift away from the document it is protecting.
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
// writing a document the schema rejects: every name the alias table can produce,
// as itself and as an array, has to match the pattern the schema states.
func TestNormalizeTypeMatchesSchemaPattern(t *testing.T) {
	pattern := schemaTypePattern(t)

	inputs := []string{
		"character varying(255)", "varchar", "character(1)", "char", "bpchar",
		"bit(8)", "bit varying(64)", "varbit",
		"numeric(10,2)", "decimal",
		"timestamp(3) without time zone", "timestamp with time zone", "timestamptz",
		"time without time zone", "time(6) with time zone", "timetz",
		"interval", "interval day to second(6)",
		"integer", "int", "int4", "bigint", "int8", "smallint", "int2",
		"boolean", "bool", "double precision", "float8", "float", "real", "float4",
		"text", "bytea", "uuid", "json", "jsonb", "date", "money",
		"inet", "cidr", "macaddr", "macaddr8", "xml", "tsvector", "tsquery",
		"oid", "name", "pg_lsn", "txid_snapshot",
		"serial", "serial4", "bigserial", "serial8", "smallserial", "serial2",
		"point", "line", "lseg", "box", "path", "polygon", "circle",
		"int4range", "int8range", "numrange", "tsrange", "tstzrange", "daterange",
		"int4multirange", "datemultirange",
		"public.order_status", "geometry",
	}

	for _, in := range inputs {
		for _, sql := range []string{in, in + "[]"} {
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

func TestNormalizeTypeWarnings(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantName    string
		wantMessage string
	}{
		{
			name:        "interval field qualifier",
			sql:         "interval day to second(6)",
			wantName:    "INTERVAL",
			wantMessage: `interval field qualifier "day to second" is not represented`,
		},
		{
			name:        "multi-dimensional array",
			sql:         "text[][]",
			wantName:    "TEXT ARRAY",
			wantMessage: "multi-dimensional array is imported as TEXT ARRAY",
		},
		{
			name:        "parameters on a type that has none",
			sql:         "integer(5)",
			wantName:    "INTEGER",
			wantMessage: "parameters of type integer are not represented",
		},
		{
			name:        "postgis parameters",
			sql:         "geometry(Point,4326)",
			wantName:    "GEOMETRY",
			wantMessage: "parameters of type geometry are not represented",
		},
		{
			name:        "type from another schema",
			sql:         "sales.order_status",
			wantName:    "ORDER_STATUS",
			wantMessage: "is defined in another schema; imported as ORDER_STATUS",
		},
		{
			name:        "non-positive length",
			sql:         "character varying(0)",
			wantName:    "VARCHAR",
			wantMessage: "length 0 is out of range",
		},
		{
			name:        "extra parameter on a length type",
			sql:         "character varying(10,2)",
			wantName:    "VARCHAR",
			wantMessage: "extra parameters of type character varying are not represented",
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

func TestNormalizeTypeErrors(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{
			name:    "a name the schema pattern rejects",
			sql:     `"my-type"`,
			wantMsg: `users.c: type my-type cannot be written to a design document`,
		},
		{
			name:    "a non-ASCII type name",
			sql:     `"ユーザー種別"`,
			wantMsg: "cannot be written to a design document",
		},
		{
			name:    "an over-long type name",
			sql:     strings.Repeat("a", maxTypeNameLength+6),
			wantMsg: "is longer than 64 characters",
		},
		{
			name:    "an over-long array type name",
			sql:     strings.Repeat("a", maxTypeNameLength-3) + "[]",
			wantMsg: "is longer than 64 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d diagList
			_, err := normalizeType(parseType(t, tt.sql), "users.c", &d)
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
