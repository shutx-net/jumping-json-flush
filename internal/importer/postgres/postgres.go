package postgres

import (
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// DefaultSchema is the PostgreSQL schema jjf imports unless told otherwise. A
// design document has nowhere to put a schema qualification, so exactly one
// schema can be imported at a time.
const DefaultSchema = "public"

// The range of pg_dump versions this importer was written against. Every major
// in it has a real dump committed under testdata/dump, and all of them import to
// the same document. A dump from outside the range is still imported; the
// warning only says that the output shape it was tested on may have moved.
const (
	minSupportedMajor = 13
	maxSupportedMajor = 18
)

// Options controls one import.
type Options struct {
	// Schema is the PostgreSQL schema to import. Empty means DefaultSchema.
	Schema string
	// Database is the name the document gives the database. Empty means: take
	// it from a \connect line in the dump, or from the name of the input file.
	Database string
	// Source names the dump in messages and supplies the fallback database
	// name. It is a display name and is never opened.
	Source string
}

// DefaultOptions returns the options the jjf command imports with. It exists so
// that the command and the tests agree on the default schema without repeating
// the literal.
func DefaultOptions() Options { return Options{Schema: DefaultSchema} }

// Import converts the bytes of a "pg_dump --schema-only" file into a database
// design document.
//
// The diagnostics are returned whether or not the import succeeded, so that a
// caller can print the warnings that preceded a failure. Failures are wrapped
// with an exit code here, the same way internal/model.Decode wraps its own, so
// that the command never has to classify them.
func Import(src []byte, opt Options) (*model.Document, []Diagnostic, error) {
	var d diagList
	version, versionLine := dumpVersion(src)
	checkDumpVersion(version, versionLine, &d)

	dump, err := parse(src, &d)
	if err != nil {
		return nil, d.all(), exitcode.Wrap(exitcode.InvalidInput, opt.Source, err)
	}
	doc, err := build(dump, opt, &d)
	if err != nil {
		return nil, d.all(), exitcode.Wrap(exitcode.InvalidInput, opt.Source, err)
	}
	return doc, d.all(), nil
}

// checkDumpVersion warns when the dump came from a pg_dump this importer was
// not written against. A dump with no banner is not warned about: a hand-edited
// or concatenated file is a legitimate input, and there is nothing to check.
func checkDumpVersion(version string, line int, d *diagList) {
	major, ok := majorVersion(version)
	if !ok || (major >= minSupportedMajor && major <= maxSupportedMajor) {
		return
	}
	d.warnf(line, "dump was produced by pg_dump %s; jjf supports %d to %d and may misread this file",
		version, minSupportedMajor, maxSupportedMajor)
}

// majorVersion reads the leading integer of a pg_dump version banner such as
// "16.13 (Ubuntu 16.13-0ubuntu0.24.04.1)".
func majorVersion(version string) (int, bool) {
	head, _, _ := strings.Cut(version, ".")
	head = strings.TrimSpace(head)
	if head == "" {
		return 0, false
	}
	major, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}
	return major, true
}
