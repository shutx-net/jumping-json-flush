package check

import "fmt"

// Finding is one way a document contradicts itself, reported with a readable
// label for the object it is about rather than a place in the file.
//
// There is no line number to give: the decoded model carries no source
// positions, and internal/model deliberately stays a thin json.Unmarshal that
// does not track any. A label reads better here anyway, because a finding is
// about a relationship between two objects rather than about one bad value.
//
// It is exported because cmd/jjf prints it. Findings never fail a run on their
// own; the CLI decides whether -strict turns them into a failure.
type Finding struct {
	Where   string
	Message string
}

// String renders a finding for the terminal. This is the single place the two
// halves are joined, so that every caller says the same thing the same way.
func (f Finding) String() string { return f.Where + ": " + f.Message }

// findingList collects findings in encounter order.
//
// The order is deterministic without any sorting, and must stay that way: the
// walk visits doc.Tables in document order and the slices inside each table in
// document order. Nothing here or in its callers may ever iterate a Go map to
// produce a finding, and nothing sorts - sorting would hide a non-deterministic
// producer instead of preventing one, and document order is the order the
// author will fix the findings in.
//
// The type is unexported so that a caller cannot reorder or filter what it was
// told.
type findingList struct {
	items []Finding
}

// addf records one finding about where.
func (l *findingList) addf(where, format string, args ...any) {
	l.items = append(l.items, Finding{Where: where, Message: fmt.Sprintf(format, args...)})
}

// all returns the accumulated findings, nil when there are none.
func (l *findingList) all() []Finding { return l.items }
