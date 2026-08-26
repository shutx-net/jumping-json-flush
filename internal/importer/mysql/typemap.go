package mysql

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
//
// There is no AutoIncrement field, where the PostgreSQL sibling's columnType
// has one. That field exists there for the serial pseudo-types, which are a
// type name that implies a sequence; MySQL writes AUTO_INCREMENT on the column
// itself and has no type that means it, so the flag arrives on myColumn and
// never has to travel through the type.
type columnType struct {
	Name      string
	Length    *int
	Precision *int
	Scale     *int
}

// paramKind says how the parenthesised arguments of a type are to be read. It
// exists because the same syntax means four different things in MySQL:
// varchar(255) is a length, decimal(10,2) is a precision and a scale,
// datetime(3) is a count of fractional-second digits, and enum('a','b') is a
// value list the design format has nowhere to keep.
//
// The value list is deliberately NOT a kind of its own. It is answered by
// paramNone plus a warning, because the outcome is the same as for any other
// unrepresentable parameter and a fourth constant would suggest there is
// somewhere for the values to go.
type paramKind uint8

// How a type's arguments are interpreted.
const (
	paramNone paramKind = iota
	paramLength
	paramPrecisionScale
	paramTimePrecision
)

// normalizeType maps a parsed MySQL type onto a schema-legal column type.
//
// obj names the column being normalized ("orders.placed_at") and appears in
// diagnostics only. An error is returned only when the type cannot be written
// to a document at all; everything else that is lost produces a warning and a
// usable result.
func normalizeType(t myType, obj string, d *diagList) (columnType, error) {
	words := strings.Join(t.Words, " ")
	name, param, known := canonicalTypeName(words)
	if !known {
		name = defaultTypeName(words)
		param = paramNone
	}

	ct := columnType{Name: name}
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
		// datetime(3) is three fractional-second DIGITS, not three characters.
		// Recording it as a length would describe a different column entirely.
		if len(t.Args) > 0 {
			ct.Precision = argInt(t.Args[0], 1, obj, "precision", t.Line, d)
		}
		if len(t.Args) > 1 {
			d.warnf(t.Line, "%s: extra parameters of type %s are not represented", obj, words)
		}
	case paramNone:
		switch {
		case len(t.Args) == 0:
		case name == "ENUM" || name == "SET":
			// The one parenthesis in MySQL that carries AUTHORED content
			// rather than a number, so the warning names what was lost rather
			// than counting it. See canonicalTypeName for why the type is
			// still imported.
			d.warnf(t.Line, "%s: the value list of type %s is not represented; imported as %s with no values",
				obj, words, name)
		default:
			d.warnf(t.Line, "%s: parameters of type %s are not represented", obj, words)
		}
	}
	// The schema's dependentRequired says a scale may not appear without a
	// precision, so a dropped precision takes the scale with it.
	if ct.Precision == nil {
		ct.Scale = nil
	}

	// UNSIGNED and ZEROFILL are part of the NAME, appended after the parameters
	// have been read from the type they qualify.
	//
	// The schema's columnType pattern is ^[A-Za-z][A-Za-z0-9_ ]*$ - it permits
	// spaces - and both attributes change the range of values the column holds,
	// so dropping either would describe a different column. It is the move the
	// PostgreSQL sibling makes for its " ARRAY" suffix and for BIT VARYING: a
	// multi-word name is a name. The alternative, dropping the attribute with a
	// warning as that importer does for an interval field qualifier, is wrong
	// here because a qualifier genuinely has nowhere to go and this does.
	//
	// The order is the order MySQL writes and internal/export/ddl/mysql.go's
	// myTypeAttributes splits back off, so a change to either is a change to
	// both.
	if t.Unsigned {
		ct.Name += " UNSIGNED"
	}
	if t.Zerofill {
		ct.Name += " ZEROFILL"
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

// canonicalTypeName maps the words of a MySQL type onto the spelling jjf writes
// and how its parameters are to be read. known is false for anything not
// listed.
//
// Two rules generated this table. First, the canonical name is the spelling a
// reader recognises and the one internal/export/ddl/mysql.go's myTypeParamKind
// answers for, so that a document imported from MySQL exports back to the type
// it came from. Second, no type is invented and none is collapsed into
// another: tinyint(1) stays TINYINT with a length of 1 rather than becoming
// BOOLEAN, because MySQL stores a BOOLEAN as tinyint(1) and mysqldump writes
// tinyint(1) - importing it as BOOLEAN would be jjf naming a type the dump did
// not, and the round trip would then never reach a fixed point, since exporting
// BOOLEAN produces a database that dumps tinyint(1) again.
// skills/db-design/references/types.md already tells authors to write TINYINT
// with a length of 1 for a MySQL boolean, so the imported document matches the
// documented convention rather than arguing with it.
//
// BOOL and BOOLEAN are still listed, because a HAND-WRITTEN dump may spell them
// out. They keep their own name: the words the file used are what jjf records,
// and rewriting them to TINYINT would be the same invention in the other
// direction.
//
// ENUM and SET are listed as taking no parameter jjf can write. Their
// parenthesis holds a value list, the format has nowhere to put one, and every
// ENUM column that reaches this importer - including one jjf itself produced -
// is in that state, so refusing the type would refuse a document jjf wrote.
// The loss is reported instead, which is the MySQL face of the limitation
// docs/usage.md states for a PostgreSQL user-defined type.
//
// It is a switch and not a package-level map on purpose: a map literal at
// package scope is mutable state any caller could corrupt, and a switch can
// return several facts about one case at once.
func canonicalTypeName(words string) (name string, param paramKind, known bool) {
	switch words {
	// Character, binary and bit strings: the parameter is a length. TINYINT is
	// here rather than with the integers, for the reason above.
	case "varchar":
		return "VARCHAR", paramLength, true
	case "char", "character":
		return "CHAR", paramLength, true
	case "binary":
		return "BINARY", paramLength, true
	case "varbinary":
		return "VARBINARY", paramLength, true
	case "bit":
		return "BIT", paramLength, true
	case "tinyint":
		return "TINYINT", paramLength, true

	// Exact numerics: the parameters are a precision and a scale. All four
	// spellings are one type in MySQL, and DECIMAL is the one mysqldump writes.
	case "decimal", "numeric", "dec", "fixed":
		return "DECIMAL", paramPrecisionScale, true

	// Date and time: the parameter counts fractional-second digits. MySQL
	// spells every one of them as a single word, so there is no counterpart to
	// the PostgreSQL sibling's "timestamp without time zone".
	case "datetime":
		return "DATETIME", paramTimePrecision, true
	case "timestamp":
		return "TIMESTAMP", paramTimePrecision, true
	case "time":
		return "TIME", paramTimePrecision, true

	// Integers other than TINYINT. A display width has been deprecated since
	// MySQL 8.0.17 and mysqldump no longer writes one, so a width that does
	// arrive - from an older dump or a hand-written file - is reported and
	// dropped rather than recorded, which is what keeps the exporter from
	// writing back a construct the server is removing.
	case "int", "integer":
		return "INTEGER", paramNone, true
	case "smallint":
		return "SMALLINT", paramNone, true
	case "mediumint":
		return "MEDIUMINT", paramNone, true
	case "bigint":
		return "BIGINT", paramNone, true

	// Approximate numerics. FLOAT keeps paramNone rather than a precision and
	// a scale on purpose: MySQL reads float(24) as a precision in BITS and
	// float(10,2) as a deprecated precision and scale, so the two arguments do
	// not mean one thing, and internal/export/ddl/mysql.go leaves FLOAT to
	// paramUnknown for the same reason.
	case "float":
		return "FLOAT", paramNone, true
	case "double", "double precision", "real":
		return "DOUBLE", paramNone, true

	// Types that take no parameter at all.
	case "bool", "boolean":
		return "BOOLEAN", paramNone, true
	case "date":
		return "DATE", paramNone, true
	case "year":
		return "YEAR", paramNone, true
	case "json":
		return "JSON", paramNone, true
	case "tinytext":
		return "TINYTEXT", paramNone, true
	case "text":
		return "TEXT", paramNone, true
	case "mediumtext":
		return "MEDIUMTEXT", paramNone, true
	case "longtext":
		return "LONGTEXT", paramNone, true
	case "tinyblob":
		return "TINYBLOB", paramNone, true
	case "blob":
		return "BLOB", paramNone, true
	case "mediumblob":
		return "MEDIUMBLOB", paramNone, true
	case "longblob":
		return "LONGBLOB", paramNone, true

	// The two whose parenthesis holds a value list rather than a number.
	case "enum":
		return "ENUM", paramNone, true
	case "set":
		return "SET", paramNone, true
	}
	return "", paramNone, false
}

// defaultTypeName handles every type the table does not list: the geometry
// types, and whatever a future MySQL adds. Upper-casing the words is enough for
// all of them.
//
// There is no schema qualification to strip, which is where this differs from
// the PostgreSQL sibling's defaultTypeName and why it needs neither a
// diagnostic nor the line it happened on: MySQL has no CREATE TYPE and no
// user-defined types at all, so a type name is always one bare word.
func defaultTypeName(words string) string { return upperType(words) }

// upperType upper-cases ASCII letters and leaves every other byte alone.
//
// The fold is ASCII-only and written out by hand, for the reason the lexer's
// foldASCII gives: no locale and no Unicode case table may decide what a SQL
// type is called. It is a copy of the PostgreSQL sibling's helper of the same
// name, which is the call this tree has already made for checkGolden and for
// diag.go - two importers do not grow a shared dependency for eight lines.
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
// match nothing with one, and so that it cannot drift from the pattern the
// diagnostics quote.
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
