package postgres

import (
	"strconv"
	"strings"
)

// maxTypeNameLength is the limit the JSON Schema puts on a column type name.
// A longer name is refused rather than truncated: a truncated type name is a
// lie that looks like data.
const maxTypeNameLength = 64

// columnType is what the JSON Schema has room for: a parameterless type name
// plus the numeric attributes that were written in parentheses.
//
// The three numbers are pointers because model.Column's are: absent has to
// render as an empty cell, which is not the same as an explicit 0.
type columnType struct {
	Name          string
	Length        *int
	Precision     *int
	Scale         *int
	AutoIncrement bool
}

// paramKind says how the parenthesised arguments of a type are to be read. It
// exists because the same syntax means three different things: varchar(255) is
// a length, numeric(10,2) is a precision and a scale, and timestamp(3) is a
// count of fractional-second digits.
type paramKind uint8

// How a type's arguments are interpreted.
const (
	paramNone paramKind = iota
	paramLength
	paramPrecisionScale
	paramTimePrecision
)

// normalizeType maps a parsed PostgreSQL type onto a schema-legal column type.
//
// obj names the column being normalized ("users.created_at") and appears in
// diagnostics only. An error is returned only when the type cannot be written
// to a document at all; everything else that is lost produces a warning and a
// usable result.
func normalizeType(t pgType, obj string, d *diagList) (columnType, error) {
	words := strings.Join(t.Words, " ")
	name, param, auto, known := canonicalTypeName(words)
	if !known {
		name = defaultTypeName(words, t.Line, obj, d)
		param = paramNone
	}
	if name == "INTERVAL" && len(t.Words) > 1 {
		d.warnf(t.Line, "%s: interval field qualifier %q is not represented",
			obj, strings.Join(t.Words[1:], " "))
	}

	ct := columnType{Name: name, AutoIncrement: auto}
	switch param {
	case paramLength:
		if len(t.Args) > 0 {
			ct.Length = argInt(t.Args[0], 1, obj, "length", t.Line, d)
		}
		if len(t.Args) > 1 {
			d.warnf(t.Line, "%s: extra parameters of type %s are not represented", obj, words)
		}
	case paramPrecisionScale:
		if len(t.Args) > 0 {
			ct.Precision = argInt(t.Args[0], 1, obj, "precision", t.Line, d)
		}
		if len(t.Args) > 1 {
			ct.Scale = argInt(t.Args[1], 0, obj, "scale", t.Line, d)
		}
		if len(t.Args) > 2 {
			d.warnf(t.Line, "%s: extra parameters of type %s are not represented", obj, words)
		}
	case paramTimePrecision:
		// timestamp(3) is three fractional-second DIGITS, not three
		// characters. Recording it as a length would describe a different
		// column entirely.
		if len(t.Args) > 0 {
			ct.Precision = argInt(t.Args[0], 1, obj, "precision", t.Line, d)
		}
		if len(t.Args) > 1 {
			d.warnf(t.Line, "%s: extra parameters of type %s are not represented", obj, words)
		}
	case paramNone:
		if len(t.Args) > 0 {
			d.warnf(t.Line, "%s: parameters of type %s are not represented", obj, words)
		}
	}
	// The schema's dependentRequired says a scale may not appear without a
	// precision, so a dropped precision takes the scale with it.
	if ct.Precision == nil {
		ct.Scale = nil
	}

	if t.ArrayDims >= 1 {
		// An array type cannot match the schema's type pattern - it has
		// brackets - so it is spelled out instead. The parameters stay: they
		// describe the ELEMENT, and character varying(255)[] really is an
		// array of 255-character strings.
		ct.Name += " ARRAY"
		if t.ArrayDims > 1 {
			// PostgreSQL does not enforce the number of dimensions anyway.
			d.warnf(t.Line, "%s: multi-dimensional array is imported as %s", obj, ct.Name)
		}
	}

	if len(ct.Name) > maxTypeNameLength {
		return columnType{}, errAt(t.Line, "%s: type name %s is longer than %d characters",
			obj, ct.Name, maxTypeNameLength)
	}
	if !isSchemaTypeName(ct.Name) {
		return columnType{}, errAt(t.Line, "%s: type %s cannot be written to a design document", obj, words)
	}
	return ct, nil
}

// canonicalTypeName maps the words of a PostgreSQL type onto the spelling jjf
// writes, how its parameters are to be read, and whether the type implies an
// auto-incrementing column. known is false for anything not listed.
//
// Two rules generated this table. First, the canonical name is the spelling a
// reader recognises: VARCHAR rather than "character varying". Second, TIMESTAMP
// and TIMESTAMPTZ must never collapse into one another, because the difference
// changes what the stored data means.
//
// It is a switch and not a package-level map on purpose: a map literal at
// package scope is mutable state any caller could corrupt, and a switch can
// return several facts about one case at once.
func canonicalTypeName(words string) (name string, param paramKind, auto bool, known bool) {
	// INTERVAL may carry a field qualifier - "interval day to second" - which
	// is part of the type name in PostgreSQL and has no place in a document.
	if words == "interval" || strings.HasPrefix(words, "interval ") {
		return "INTERVAL", paramTimePrecision, false, true
	}

	switch words {
	// Character and bit strings: the parameter is a length.
	case "character varying", "varchar":
		return "VARCHAR", paramLength, false, true
	case "character", "char", "bpchar":
		return "CHAR", paramLength, false, true
	case "bit":
		return "BIT", paramLength, false, true
	case "bit varying", "varbit":
		return "BIT VARYING", paramLength, false, true

	// Exact numerics: the parameters are a precision and a scale.
	case "numeric", "decimal":
		return "NUMERIC", paramPrecisionScale, false, true

	// Date and time: the parameter counts fractional-second digits.
	case "timestamp without time zone", "timestamp":
		return "TIMESTAMP", paramTimePrecision, false, true
	case "timestamp with time zone", "timestamptz":
		return "TIMESTAMPTZ", paramTimePrecision, false, true
	case "time without time zone", "time":
		return "TIME", paramTimePrecision, false, true
	case "time with time zone", "timetz":
		return "TIMETZ", paramTimePrecision, false, true

	// Types that take no parameter at all.
	case "integer", "int", "int4":
		return "INTEGER", paramNone, false, true
	case "bigint", "int8":
		return "BIGINT", paramNone, false, true
	case "smallint", "int2":
		return "SMALLINT", paramNone, false, true
	case "boolean", "bool":
		return "BOOLEAN", paramNone, false, true
	case "double precision", "float8", "float":
		return "DOUBLE PRECISION", paramNone, false, true
	case "real", "float4":
		return "REAL", paramNone, false, true
	case "text":
		return "TEXT", paramNone, false, true
	case "bytea":
		return "BYTEA", paramNone, false, true
	case "uuid":
		return "UUID", paramNone, false, true
	case "json":
		return "JSON", paramNone, false, true
	case "jsonb":
		return "JSONB", paramNone, false, true
	case "date":
		return "DATE", paramNone, false, true
	case "money":
		return "MONEY", paramNone, false, true
	case "inet":
		return "INET", paramNone, false, true
	case "cidr":
		return "CIDR", paramNone, false, true
	case "macaddr":
		return "MACADDR", paramNone, false, true
	case "macaddr8":
		return "MACADDR8", paramNone, false, true
	case "xml":
		return "XML", paramNone, false, true
	case "tsvector":
		return "TSVECTOR", paramNone, false, true
	case "tsquery":
		return "TSQUERY", paramNone, false, true
	case "oid":
		return "OID", paramNone, false, true
	case "name":
		return "NAME", paramNone, false, true
	case "pg_lsn":
		return "PG_LSN", paramNone, false, true
	case "txid_snapshot":
		return "TXID_SNAPSHOT", paramNone, false, true

	// The serial pseudo-types. pg_dump expands them into an integer plus a
	// nextval() default, but a hand-written dump may still spell them out.
	case "serial", "serial4":
		return "INTEGER", paramNone, true, true
	case "bigserial", "serial8":
		return "BIGINT", paramNone, true, true
	case "smallserial", "serial2":
		return "SMALLINT", paramNone, true, true
	}
	return "", paramNone, false, false
}

// defaultTypeName handles every type the alias table does not list: the
// geometric and range types, PostGIS types, enums and domains. Upper-casing the
// words is enough for all of them, because the only thing standing between such
// a name and the schema is a schema qualification.
func defaultTypeName(words string, line int, obj string, d *diagList) string {
	name := upperType(words)
	schema, bare, qualified := strings.Cut(name, ".")
	if !qualified {
		return name
	}
	// A type in the default schema is written "public.status" by pg_dump; the
	// qualification carries no information and the dot cannot appear in a type
	// name.
	if schema != "PUBLIC" {
		d.warnf(line, "%s: type %s is defined in another schema; imported as %s", obj, words, bare)
	}
	return bare
}

// upperType upper-cases ASCII letters and leaves every other byte alone. The
// fold is ASCII-only for the same reason the lexer's is: no locale or Unicode
// case table may decide what a type is called.
func upperType(words string) string {
	out := make([]byte, len(words))
	for i := 0; i < len(words); i++ {
		c := words[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// argInt reads one type parameter. min is the smallest value the schema allows
// for the attribute: 1 for a length or a precision, 0 for a scale. Anything
// unreadable is dropped with a warning rather than refused, because a type
// parameter jjf cannot record is not a reason to lose the column.
func argInt(raw string, min int, obj, attr string, line int, d *diagList) *int {
	text := strings.TrimSpace(raw)
	v, err := strconv.Atoi(text)
	if err != nil {
		d.warnf(line, "%s: %s %q is not a number; not imported", obj, attr, text)
		return nil
	}
	if v < min {
		d.warnf(line, "%s: %s %d is out of range; not imported", obj, attr, v)
		return nil
	}
	return intp(v)
}

// intp returns a pointer to v, for the optional numeric fields of model.Column.
func intp(v int) *int { return &v }

// isSchemaTypeName reports whether name satisfies the columnType pattern of
// schema/db-design.schema.json, ^[A-Za-z][A-Za-z0-9_ ]*$. It is written out
// rather than compiled as a regexp so that this package keeps its promise to
// match nothing with one.
func isSchemaTypeName(name string) bool {
	if name == "" || !isASCIILetter(name[0]) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if isASCIILetter(c) || isDigit(c) || c == '_' || c == ' ' {
			continue
		}
		return false
	}
	return true
}

// isASCIILetter reports whether c is an unaccented Latin letter.
func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
