package mysql

import "fmt"

// This file is a near-copy of internal/importer/postgres/diag.go, and that is
// the whole of its design. Two independent importers must not grow a common
// dependency for a two-field struct and a slice append: cmd/jjf adapts both to
// one printer at the only place the two ever meet, which is where the sharing
// belongs. The tree has made this call before, for checkGolden and for
// tableLabel, and written the reasoning down both times.

// Diagnostic is one warning about a dump: something jjf understood but cannot
// represent in a design document, reported with the line it was found on.
//
// It is exported because cmd/jjf prints it. Warnings never abort an import on
// their own; the CLI decides whether -strict turns them into a failure.
type Diagnostic struct {
	Line    int
	Message string
}

// String renders a diagnostic for the terminal. A zero line means the warning
// is about the dump as a whole rather than a place in it.
func (d Diagnostic) String() string {
	if d.Line == 0 {
		return d.Message
	}
	return fmt.Sprintf("line %d: %s", d.Line, d.Message)
}

// diagList collects diagnostics in encounter order.
//
// The order is deterministic without any sorting, and must stay that way: the
// parse pass appends while walking the statements of a dump from top to bottom,
// and the build pass appends afterwards while walking slices only. Nothing here
// or in its callers may ever iterate a Go map to produce a warning.
//
// The type is unexported so that a caller cannot reorder or filter what it was
// told.
type diagList struct {
	items []Diagnostic
}

// warnf records one warning about line. Pass 0 when no line applies.
func (l *diagList) warnf(line int, format string, args ...any) {
	l.items = append(l.items, Diagnostic{Line: line, Message: fmt.Sprintf(format, args...)})
}

// all returns the accumulated warnings, nil when there are none.
func (l *diagList) all() []Diagnostic { return l.items }
