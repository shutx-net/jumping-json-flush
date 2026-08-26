package ddl

import (
	"strconv"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// ---------------------------------------------------------------------------
// Type parameters, shared by every dialect
// ---------------------------------------------------------------------------

// What is here is the question every dialect asks in the same words - given
// that a type's parenthesis holds a length, or a precision and a scale, or a
// count of fractional-second digits, what does the parenthesis look like - and
// the fold that decides which question a type name is asking. Which type asks
// which is a fact about a target system, so each dialect keeps its own table in
// its own file.

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
	// after the first word. It has no analogue in a dialect whose
	// parameterised date and time types are spelled as one word - DATETIME(3)
	// - which is why the kind stays a PostgreSQL-only answer even though the
	// enumeration is shared.
	paramTimePrecisionInfix
	paramUnknown
)

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
		// Never reached: pgRenderType sends the infix types to
		// pgInfixTimePrecision, because the parameter lands inside the name
		// rather than after it.
	case paramUnknown:
		// For a type jjf knows, jjf knows which attribute the parenthesis
		// means. For one it does not, the only honest thing is to reproduce
		// what the document says, in the order the rest of the tool already
		// displays it: internal/export/erd's RenderType and
		// internal/export/xlsx/tabledef.go's sizeOf use the same precedence
		// for the same document, so the three exporters cannot disagree. That
		// precedence is a property of the document rather than of any
		// database, which is why this function is shared and the tables that
		// feed it are not.
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
