// Package schema compiles the embedded jjf JSON Schema and validates
// database design documents against it, turning validation failures into a
// human readable list of issues.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	schemadata "github.com/shutx-net/jumping-json-flush/schema"
)

// Validator validates database design documents against the embedded schema.
// Build one with NewValidator and reuse it; compiling is cheap but registering
// the same resource twice is an error.
type Validator struct {
	schema *jsonschema.Schema
}

// NewValidator compiles the embedded database design schema. It never touches
// the network.
func NewValidator() (*Validator, error) {
	// jsonschema.UnmarshalJSON is required here: passing an io.Reader or a
	// []byte makes Compile fail with "invalid jsonType", and plain
	// json.Unmarshal loses json.Number precision.
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemadata.DBDesign))
	if err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020) // fallback when a document omits $schema
	c.UseLoader(denyLoader{})            // jjf never fetches anything over the network
	// Do not call c.AssertFormat: format stays an annotation, per Draft 2020-12.

	if err := c.AddResource(schemadata.DBDesignURL, doc); err != nil {
		return nil, fmt.Errorf("register embedded schema: %w", err)
	}
	// The loc passed to Compile must equal the url passed to AddResource.
	sch, err := c.Compile(schemadata.DBDesignURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded schema: %w", err)
	}
	return &Validator{schema: sch}, nil
}

// Validate checks raw against the database design schema. source names the
// document in messages and is usually a file path.
//
// A malformed document yields an error carrying exitcode.InvalidInput; a
// well-formed document that breaks the schema yields an *InvalidDocumentError
// carrying exitcode.SchemaFailed.
func (v *Validator) Validate(source string, raw []byte) error {
	body := StripBOM(raw)
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return exitcode.Wrap(exitcode.InvalidInput, source, locateJSONError(body, err))
	}

	err = v.schema.Validate(inst)
	if err == nil {
		return nil
	}

	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return exitcode.Wrap(exitcode.SchemaFailed, source, err)
	}
	return exitcode.Wrap(exitcode.SchemaFailed, "", &InvalidDocumentError{
		Source: source,
		Issues: issuesOf(ve),
	})
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// StripBOM removes a leading UTF-8 byte order mark. Both encoding/json and
// jsonschema.UnmarshalJSON reject a BOM outright, and editors on Windows
// write one by default.
func StripBOM(b []byte) []byte { return bytes.TrimPrefix(b, utf8BOM) }

// denyLoader refuses every remote reference. The embedded schema is
// self-contained, so any attempted fetch is a bug, not a fallback.
type denyLoader struct{}

// Load always fails.
func (denyLoader) Load(u string) (any, error) {
	return nil, fmt.Errorf("refusing to load external schema resource: %s", u)
}

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
