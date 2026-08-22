package ddl

import (
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// paramKind says how a column's length, precision and scale are to be spelled
// back into its type.
//
// The document cannot say. "precision": 3 means three fractional-second digits
// for TIMESTAMP and three significant digits for NUMERIC, and nothing in the
// JSON tells the two apart, so the generator has to know the same fact the
// importer knew when it took the parentheses off.
type paramKind uint8

// How a type's parameters are written.
const (
	paramNone paramKind = iota
	paramLength
	paramPrecisionScale
	paramTimePrecision
	// paramTimePrecisionInfix is the one that cannot be appended: PostgreSQL
	// reads TIMESTAMP WITH TIME ZONE(3) as a syntax error and
	// TIMESTAMP(3) WITH TIME ZONE as the type meant, so the parameter goes
	// after the first word.
	paramTimePrecisionInfix
	paramUnknown
)

// typeParamKind reports how the parameters of the type called name are
// written. name has already been upper-cased and stripped of any array suffix.
//
// This is NOT internal/importer/postgres/typemap.go's table read backwards, and
// the two are deliberately separate. canonicalTypeName is keyed on the
// lowercase spellings pg_dump writes and answers "what does jjf call this
// type"; this one is keyed on whatever the document says and answers only
// "which attribute do the parentheses hold". Sharing them would mean an
// exported cross-package API for a fact each side needs in a different shape.
//
// It is a switch and not a package-level map, for the reason canonicalTypeName
// gives verbatim: a map literal at package scope is mutable state any caller
// could corrupt.
func typeParamKind(name string) paramKind {
	switch name {
	// Character and bit strings: the parameter is a length.
	case "VARCHAR", "CHARACTER VARYING", "CHAR", "CHARACTER", "BPCHAR",
		"BIT", "BIT VARYING", "VARBIT":
		return paramLength

	// Exact numerics: the parameters are a precision and a scale.
	case "NUMERIC", "DECIMAL":
		return paramPrecisionScale

	// Date and time: the parameter counts fractional-second digits.
	case "TIMESTAMP", "TIMESTAMPTZ", "TIME", "TIMETZ", "INTERVAL":
		return paramTimePrecision

	// The spelled-out date and time names, whose parameter is infixed. The
	// importer never writes these - canonicalTypeName folds them into
	// TIMESTAMPTZ and friends - but a hand-authored document may, and
	// skills/db-design/references/types.md recommends the long form.
	case "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITHOUT TIME ZONE",
		"TIME WITH TIME ZONE", "TIME WITHOUT TIME ZONE":
		return paramTimePrecisionInfix

	// Types that take no parameter at all.
	case "INTEGER", "INT", "INT4", "BIGINT", "INT8", "SMALLINT", "INT2",
		"BOOLEAN", "BOOL", "DOUBLE PRECISION", "FLOAT8", "REAL", "FLOAT4",
		"TEXT", "BYTEA", "UUID", "JSON", "JSONB", "DATE", "MONEY",
		"INET", "CIDR", "MACADDR", "MACADDR8", "XML", "TSVECTOR", "TSQUERY",
		"OID", "NAME", "PG_LSN", "TXID_SNAPSHOT":
		return paramNone
	}
	return paramUnknown
}

// arraySuffix is what the importer appends to the element type name, because an
// array type cannot match the schema's columnType pattern - it has brackets.
const arraySuffix = " ARRAY"

// renderType folds a column's declared attributes back into its type, which is
// the reverse of what the importer took apart.
//
// The type NAME is passed through exactly as the document writes it and is
// never rewritten: that is the specification's choice 5, and it is why this
// table decides only how the parentheses are read. It is never quoted either,
// unlike every identifier this package emits. That asymmetry looks like an
// inconsistency and is not: quoting would make "ORDER_STATUS" a different type
// from the lowercase order_status PostgreSQL actually created, and "integer" is
// not a type at all.
//
// A parameter a known type cannot take is dropped without a word: INTEGER with
// a length of 11 renders as INTEGER. Emitting integer(11) would be DDL
// PostgreSQL rejects, which breaks the promise that a document this exporter
// accepts produces a script the database takes; refusing the document would be
// an opinion about a design "jjf validate" calls fine; and dropping is what
// internal/importer/postgres/typemap.go does in the mirror situation. It warns
// there and cannot here - Export writes into an io.Writer and has no channel to
// say anything on - so the documentation says it instead.
func renderType(c *model.Column) string {
	name := c.Type

	// The importer appends the suffix once even for a multi-dimensional array,
	// so one is stripped. A type actually called ARRAY keeps its name: the
	// remainder would be empty, which is no type at all.
	array := false
	if len(name) > len(arraySuffix) && upperASCII(name[len(name)-len(arraySuffix):]) == arraySuffix {
		name = name[:len(name)-len(arraySuffix)]
		array = true
	}

	var out string
	if kind := typeParamKind(upperASCII(name)); kind == paramTimePrecisionInfix {
		out = infixTimePrecision(name, c)
	} else {
		out = name + typeParams(kind, c)
	}
	if array {
		// The parameters describe the ELEMENT - character varying(255)[]
		// really is an array of 255-character strings - so the brackets go
		// outside them.
		out += "[]"
	}
	return out
}

// typeParams returns the parenthesised part of a rendered type, empty when the
// column declares no attribute the type can carry.
func typeParams(kind paramKind, c *model.Column) string {
	switch kind {
	case paramLength:
		if c.Length != nil {
			return "(" + strconv.Itoa(*c.Length) + ")"
		}
	case paramPrecisionScale:
		if c.Precision != nil && c.Scale != nil {
			return "(" + strconv.Itoa(*c.Precision) + "," + strconv.Itoa(*c.Scale) + ")"
		}
		if c.Precision != nil {
			return "(" + strconv.Itoa(*c.Precision) + ")"
		}
	case paramTimePrecision:
		if c.Precision != nil {
			return "(" + strconv.Itoa(*c.Precision) + ")"
		}
	case paramTimePrecisionInfix:
		// Never reached: renderType sends the infix types to
		// infixTimePrecision, because the parameter lands inside the name
		// rather than after it.
	case paramUnknown:
		// For a type jjf knows, jjf knows which attribute the parenthesis
		// means. For one it does not, the only honest thing is to reproduce
		// what the document says, in the order the rest of the tool already
		// displays it: internal/export/dot/text.go's renderType and
		// internal/export/xlsx/tabledef.go's sizeOf use the same precedence
		// for the same document, so the three exporters cannot disagree.
		switch {
		case c.Length != nil:
			return "(" + strconv.Itoa(*c.Length) + ")"
		case c.Precision != nil && c.Scale != nil:
			return "(" + strconv.Itoa(*c.Precision) + "," + strconv.Itoa(*c.Scale) + ")"
		case c.Precision != nil:
			return "(" + strconv.Itoa(*c.Precision) + ")"
		}
	}
	return ""
}

// infixTimePrecision writes a precision into a spelled-out date or time type,
// after its first word: TIMESTAMP WITH TIME ZONE with a precision of 3 becomes
// TIMESTAMP(3) WITH TIME ZONE. Appending it instead would be a syntax error,
// and rewriting the name to TIMESTAMPTZ(3) is the rewrite choice 5 forbids.
//
// A type this shape always has a second word, but the split is written so that
// a name without one still renders as itself.
func infixTimePrecision(name string, c *model.Column) string {
	if c.Precision == nil {
		return name
	}
	first, rest, found := strings.Cut(name, " ")
	if !found {
		return name + "(" + strconv.Itoa(*c.Precision) + ")"
	}
	return first + "(" + strconv.Itoa(*c.Precision) + ") " + rest
}

// upperASCII upper-cases the unaccented Latin letters of s and leaves every
// other byte alone.
//
// The fold is ASCII-only and written out rather than taken from strings, for
// the reason internal/check/default.go and internal/importer/postgres/lex.go
// both give: no locale and no Unicode case table may decide what a SQL type is
// called. This is the third such helper in the tree, which is accepted - each
// sits in the package that needs it and none of them is worth a shared one.
func upperASCII(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
