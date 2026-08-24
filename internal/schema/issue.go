package schema

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// maxPointerColumn caps how wide the JSON Pointer column in a report grows, so
// that one deeply nested location does not push every message off the screen.
const maxPointerColumn = 44

// documentRoot labels an issue that applies to the document as a whole.
const documentRoot = "(document root)"

// Issue is one human readable schema violation.
type Issue struct {
	Pointer string // JSON Pointer into the instance, e.g. /tables/0/columns/1/name
	Message string
}

// InvalidDocumentError aggregates every schema violation found in one document.
type InvalidDocumentError struct {
	Source string
	Issues []Issue
}

// Error returns a one line summary. The full listing goes to WriteReport so
// that wrapping this error with %w never produces a multi-line message.
func (e *InvalidDocumentError) Error() string {
	return fmt.Sprintf("%s: %d schema violation(s)", e.Source, len(e.Issues))
}

// WriteReport prints every violation as an aligned list.
func (e *InvalidDocumentError) WriteReport(w io.Writer) {
	fmt.Fprintf(w, "%s: does not conform to the jjf database design schema\n", e.Source)

	width := 0
	for _, is := range e.Issues {
		p := is.Pointer
		if p == "" {
			p = documentRoot
		}
		if len(p) > width {
			width = len(p)
		}
	}
	if width > maxPointerColumn {
		width = maxPointerColumn
	}

	for _, is := range e.Issues {
		p := is.Pointer
		if p == "" {
			p = documentRoot
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, p, is.Message)
	}

	fmt.Fprintf(w, "\n%d error(s). See schema/db-design.schema.json.\n", len(e.Issues))
}

// sortIssues puts the issues a document produced into the order a report
// prints them in: top to bottom like the document, and the same on every run.
//
// The second half is not free. The evaluator walks Go maps, and Go randomises
// map iteration, so the only way a report can be reproducible is for the order
// to be decided here rather than inherited from a walk.
func sortIssues(issues []Issue) []Issue {
	out := dedupe(issues)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pointer != out[j].Pointer {
			return lessPointer(out[i].Pointer, out[j].Pointer)
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// dedupe drops exact repeats. The evaluator visits every location once, so
// there is nothing here for it to find today; it stays because "sorted and
// without repeats" is what a report promises, and this is the single place
// that promise is kept rather than assumed.
func dedupe(issues []Issue) []Issue {
	seen := make(map[Issue]bool, len(issues))
	out := issues[:0]
	for _, is := range issues {
		if seen[is] {
			continue
		}
		seen[is] = true
		out = append(out, is)
	}
	return out
}

// lessPointer orders JSON Pointers with numeric aware segment comparison so
// that /tables/0/columns/2 sorts before /tables/0/columns/10.
func lessPointer(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		if aerr == nil && berr == nil {
			return an < bn
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}
