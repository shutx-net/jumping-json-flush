package ddl

import (
	"testing"

	"github.com/shutx-net/jumping-json-flush/internal/model"
)

func TestRenderType(t *testing.T) {
	tests := []struct {
		name      string
		typ       string
		length    *int
		precision *int
		scale     *int
		want      string
	}{
		// A length, and the types that take one.
		{name: "VARCHAR with a length", typ: "VARCHAR", length: intp(255), want: "VARCHAR(255)"},
		{name: "VARCHAR without one", typ: "VARCHAR", want: "VARCHAR"},
		{name: "the spelled-out character varying", typ: "CHARACTER VARYING", length: intp(10), want: "CHARACTER VARYING(10)"},
		{name: "CHAR", typ: "CHAR", length: intp(2), want: "CHAR(2)"},
		{name: "BIT VARYING", typ: "BIT VARYING", length: intp(8), want: "BIT VARYING(8)"},
		// A length is what a character type takes, so a precision on one is
		// not a parameter it can carry.
		{name: "VARCHAR carrying a precision instead", typ: "VARCHAR", precision: intp(9), want: "VARCHAR"},

		// A precision and a scale.
		{name: "NUMERIC with both", typ: "NUMERIC", precision: intp(10), scale: intp(2), want: "NUMERIC(10,2)"},
		{name: "NUMERIC with a precision alone", typ: "NUMERIC", precision: intp(8), want: "NUMERIC(8)"},
		{name: "NUMERIC with neither", typ: "NUMERIC", want: "NUMERIC"},
		{name: "DECIMAL is the same type", typ: "DECIMAL", precision: intp(4), scale: intp(1), want: "DECIMAL(4,1)"},
		// The schema forbids a scale without a precision, but renderType is
		// total over documents that never passed it.
		{name: "a scale without a precision", typ: "NUMERIC", scale: intp(2), want: "NUMERIC"},
		{name: "NUMERIC carrying a length instead", typ: "NUMERIC", length: intp(10), want: "NUMERIC"},

		// Fractional-second digits, appended.
		{name: "TIMESTAMP with a precision", typ: "TIMESTAMP", precision: intp(3), want: "TIMESTAMP(3)"},
		{name: "TIMESTAMPTZ without one", typ: "TIMESTAMPTZ", want: "TIMESTAMPTZ"},
		{name: "TIMETZ with one", typ: "TIMETZ", precision: intp(6), want: "TIMETZ(6)"},
		{name: "INTERVAL with one", typ: "INTERVAL", precision: intp(0), want: "INTERVAL(0)"},

		// Fractional-second digits, infixed. TIMESTAMP WITH TIME ZONE(3) is a
		// syntax error in PostgreSQL and the parameter belongs after the first
		// word.
		{name: "the spelled-out timestamptz with a precision", typ: "TIMESTAMP WITH TIME ZONE", precision: intp(3), want: "TIMESTAMP(3) WITH TIME ZONE"},
		{name: "the spelled-out timestamptz without one", typ: "TIMESTAMP WITH TIME ZONE", want: "TIMESTAMP WITH TIME ZONE"},
		{name: "the spelled-out timestamp", typ: "TIMESTAMP WITHOUT TIME ZONE", precision: intp(6), want: "TIMESTAMP(6) WITHOUT TIME ZONE"},
		{name: "the spelled-out timetz", typ: "TIME WITH TIME ZONE", precision: intp(1), want: "TIME(1) WITH TIME ZONE"},
		{name: "the spelled-out time", typ: "TIME WITHOUT TIME ZONE", precision: intp(2), want: "TIME(2) WITHOUT TIME ZONE"},

		// A parameter a known type cannot take is dropped without a word.
		{name: "INTEGER with a length", typ: "INTEGER", length: intp(11), want: "INTEGER"},
		{name: "TEXT with a precision", typ: "TEXT", precision: intp(4), want: "TEXT"},
		{name: "BOOLEAN alone", typ: "BOOLEAN", want: "BOOLEAN"},
		{name: "DOUBLE PRECISION, whose name has a space", typ: "DOUBLE PRECISION", want: "DOUBLE PRECISION"},

		// A type jjf does not know reproduces what the document says, in the
		// precedence the other two exporters display it in.
		{name: "an unknown type with a length", typ: "ORDER_STATUS", length: intp(12), want: "ORDER_STATUS(12)"},
		{name: "an unknown type with a precision and a scale", typ: "MONEYISH", precision: intp(9), scale: intp(3), want: "MONEYISH(9,3)"},
		{name: "an unknown type with a precision alone", typ: "MONEYISH", precision: intp(9), want: "MONEYISH(9)"},
		{name: "an unknown type with a length beating a precision", typ: "MONEYISH", length: intp(4), precision: intp(9), want: "MONEYISH(4)"},
		{name: "an unknown type with nothing", typ: "GEOMETRY", want: "GEOMETRY"},

		// The fold is ASCII-only and case-insensitive: a document may spell a
		// type in any case and the name still passes through as written.
		{name: "a lowercase known type", typ: "varchar", length: intp(3), want: "varchar(3)"},
		{name: "a mixed-case known type", typ: "TimeStamp", precision: intp(3), want: "TimeStamp(3)"},
		{name: "a mixed-case infix type", typ: "timestamp with time zone", precision: intp(3), want: "timestamp(3) with time zone"},

		// Arrays: the parameter describes the element, so it stays inside the
		// brackets.
		{name: "an array of a bare type", typ: "TEXT ARRAY", want: "TEXT[]"},
		{name: "an array of a parameterised type", typ: "VARCHAR ARRAY", length: intp(255), want: "VARCHAR(255)[]"},
		{name: "an array of an infix type", typ: "TIMESTAMP WITH TIME ZONE ARRAY", precision: intp(3), want: "TIMESTAMP(3) WITH TIME ZONE[]"},
		{name: "an array of an unknown type", typ: "GEOMETRY ARRAY", length: intp(2), want: "GEOMETRY(2)[]"},
		{name: "a lowercase array suffix", typ: "INTEGER array", want: "INTEGER[]"},
		// One suffix is stripped, because the importer appends one even for a
		// multi-dimensional array.
		{name: "two array suffixes, one stripped", typ: "TEXT ARRAY ARRAY", want: "TEXT ARRAY[]"},
		// A type actually called ARRAY keeps its name: the remainder would be
		// no type at all.
		{name: "a type called ARRAY", typ: "ARRAY", want: "ARRAY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &model.Column{Type: tt.typ, Length: tt.length, Precision: tt.precision, Scale: tt.scale}
			if got := renderType(c); got != tt.want {
				t.Errorf("renderType(%+v) = %q, want %q", c, got, tt.want)
			}
		})
	}
}

// intp returns a pointer to v, for model.Column's optional numeric attributes.
func intp(v int) *int { return &v }
