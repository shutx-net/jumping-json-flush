// Package schema compiles the embedded jjf JSON Schema and validates
// database design documents against it, turning validation failures into a
// human readable list of issues.
//
// The validation is done by this package's own evaluator rather than by a
// JSON Schema library. It implements the subset of Draft 2020-12 that the
// embedded schema uses and refuses anything outside it; subset.go says what
// that subset is and why refusing is the safe answer.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// Validator validates database design documents against the embedded schema.
// It holds the decoded schema together with the regular expressions compiled
// out of it, which is the whole reason to build one rather than to compile per
// document. Building it is cheap, and it is read-only once built, so one
// validator serves every document a command touches.
type Validator struct {
	root *schemaNode
}

// NewValidator compiles the embedded database design schema. The schema
// travels inside the binary and every $ref in it resolves within the document,
// so there is nothing to fetch and nothing in this package that could try.
func NewValidator() (*Validator, error) {
	root, err := compileSchema(schemadata.DBDesign)
	if err != nil {
		return nil, err
	}
	return &Validator{root: root}, nil
}

// Validate checks raw against the database design schema. source names the
// document in messages and is usually a file path.
//
// A malformed document yields an error carrying exitcode.InvalidInput; a
// well-formed document that breaks the schema yields an *InvalidDocumentError
// carrying exitcode.SchemaFailed.
func (v *Validator) Validate(source string, raw []byte) error {
	body := StripBOM(raw)
	inst, err := decodeInstance(body)
	if err != nil {
		return exitcode.Wrap(exitcode.InvalidInput, source, locateJSONError(body, err))
	}

	var issues []Issue
	v.root.evaluate("", inst, &issues)
	if len(issues) == 0 {
		return nil
	}
	// The op is deliberately empty: InvalidDocumentError.Error() already names
	// the source, and an op here would name it a second time on the same line.
	return exitcode.Wrap(exitcode.SchemaFailed, "", &InvalidDocumentError{
		Source: source,
		Issues: sortIssues(issues),
	})
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// StripBOM removes a leading UTF-8 byte order mark. Both encoding/json and
// this package's own decoder reject a BOM outright, and editors on Windows
// write one by default.
func StripBOM(b []byte) []byte { return bytes.TrimPrefix(b, utf8BOM) }

// locateJSONError rewrites a decoding failure so that it names a line and
// column. encoding/json reports only a byte offset, which nobody can act on
// while editing a large document.
func locateJSONError(raw []byte, err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := lineColumn(raw, se.Offset)
		return fmt.Errorf("line %d, column %d: %w", line, col, se)
	}
	return err
}

// lineColumn converts a byte offset into a 1-based line and column. Offsets
// outside raw are clamped so that a malformed report never panics.
func lineColumn(raw []byte, offset int64) (line, column int) {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(raw)) {
		offset = int64(len(raw))
	}
	line, column = 1, 1
	for _, b := range raw[:offset] {
		if b == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}
