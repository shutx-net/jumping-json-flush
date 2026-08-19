package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

// SupportedFormatMajor is the database design format major version this build
// understands. Documents from a newer major are rejected with an actionable
// message rather than a generic schema error.
const SupportedFormatMajor = "1"

// utf8BOM is the UTF-8 byte order mark. encoding/json rejects a leading BOM
// outright and editors on Windows write one by default, so it is stripped
// before decoding just as it is before validation.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Decode parses a database design document.
//
// Callers are expected to have validated raw against the JSON Schema first;
// the checks here are the few that JSON Schema cannot express plus a defensive
// net for callers that skip validation. Unknown fields are accepted on
// purpose: the schema already forbids them, and rejecting them here would make
// this package a second source of truth and break forward compatibility with
// documents written for a newer schema.
func Decode(raw []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), &doc); err != nil {
		return nil, exitcode.Wrap(exitcode.InvalidInput, "decode document", err)
	}
	if err := checkFormatVersion(doc.FormatVersion); err != nil {
		return nil, exitcode.Wrap(exitcode.InvalidInput, "", err)
	}
	return &doc, nil
}

// checkFormatVersion reports whether v is a format version this build can
// process.
func checkFormatVersion(v string) error {
	major, _, ok := strings.Cut(v, ".")
	if !ok || major != SupportedFormatMajor {
		return fmt.Errorf("unsupported formatVersion %q; this jjf supports %s.x - please upgrade jjf", v, SupportedFormatMajor)
	}
	return nil
}
