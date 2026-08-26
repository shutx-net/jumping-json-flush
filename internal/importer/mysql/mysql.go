// Package mysql reads a "mysqldump --no-data" file and builds a jjf database
// design document from it.
//
// It is the second importer, and it is deliberately independent of the first:
// nothing here imports internal/importer/postgres and nothing there imports
// this. The two share a shape - a lexer, a parser, an intermediate
// representation, a resolution pass and a type map - and share no code, because
// the only thing a MySQL dump and a PostgreSQL dump have in common is that both
// are SQL, and every place the two grammars agree is a place one of them is
// about to stop agreeing. cmd/jjf is where they meet, and it meets them through
// a table of dialects rather than through a shared type.
package mysql

import (
	"strconv"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/model"
)

// The range of MySQL server majors this importer was written against.
//
// Every major in it has a real dump committed under testdata/dump, and all of
// them import to the same document; fixture_test.go's
// TestCapturedSeriesCoverTheSupportedMajors is what holds the two together, so
// widening the range without capturing a dump from the new major fails a test.
// A dump from outside the range is still imported; the warning only says that
// the output shape it was tested on may have moved.
//
// The bound is the MAJOR, although the captures are kept per release SERIES -
// testdata/dump/mysql8.0 rather than testdata/dump/mysql8 - because 8.0 and 8.4
// are different series of one major whose mysqldump output differs, so a
// directory per major would hide the difference the captures exist to expose.
// The major is nonetheless the granularity at which jjf can honestly claim
// anything, since that is what the banner's leading number means.
const (
	minSupportedMajor = 8
	maxSupportedMajor = 8
)

// Options controls one import.
//
// There is no Schema field, and there is no room for one: a MySQL schema IS a
// database, so there is no second level to choose. cmd/jjf refuses an
// explicitly-set -schema for this dialect rather than accepting it and quietly
// doing nothing with it.
type Options struct {
	// Database is the name the document gives the database. Empty means: take
	// it from the dump's own USE statement or header banner, and failing both
	// from the name of the input file.
	Database string
	// Source names the dump in messages and supplies the last fallback for the
	// database name. It is a display name and is never opened.
	Source string
}

// DefaultOptions returns the options the jjf command imports with.
//
// It has nothing to set, unlike its PostgreSQL counterpart, which has a default
// schema to name. It exists anyway so that the command, the tests and any
// future caller ask for the defaults the same way for both dialects - a caller
// that has to know which importer needs a constructor and which does not is a
// caller that will eventually guess wrong.
func DefaultOptions() Options { return Options{} }

// Import converts the bytes of a "mysqldump --no-data" file into a database
// design document.
//
// The diagnostics are returned whether or not the import succeeded, so that a
// caller can print the warnings that preceded a failure - they are usually what
// explains it. Failures are wrapped with an exit code here, the same way
// internal/model.Decode wraps its own, so that the command never has to
// classify them.
func Import(src []byte, opt Options) (*model.Document, []Diagnostic, error) {
	var d diagList

	// The banner is read here, from the raw bytes, and not taken off the
	// myDump the parse fills in - although parse reads exactly the same two
	// values into exactly the same helper. The duplicated call buys the one
	// thing that matters: a dump from a server this importer was not written
	// against still SAYS so when it then fails to parse, which is the case
	// where the warning explains the failure. Reading it afterwards would
	// silence it precisely when it is worth the most.
	version, versionLine := serverVersion(src)
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

// checkDumpVersion warns when the dump came from a MySQL server this importer
// was not written against. A dump with no banner is not warned about: a
// hand-edited or concatenated file is a legitimate input, and there is nothing
// to check.
func checkDumpVersion(version string, line int, d *diagList) {
	major, ok := majorVersion(version)
	if !ok || (major >= minSupportedMajor && major <= maxSupportedMajor) {
		return
	}
	d.warnf(line, "dump was produced by MySQL server %s; jjf supports %s and may misread this file",
		version, supportedMajors())
}

// supportedMajors renders the supported range for a diagnostic. A range of one
// major is written as one number, because "supports 8 to 8" reads as a mistake
// rather than as a fact.
func supportedMajors() string {
	if minSupportedMajor == maxSupportedMajor {
		return strconv.Itoa(minSupportedMajor)
	}
	return strconv.Itoa(minSupportedMajor) + " to " + strconv.Itoa(maxSupportedMajor)
}

// majorVersion reads the leading integer of a MySQL server version banner such
// as "8.0.46-0ubuntu0.24.04.3".
//
// The distribution suffix is why the leading component is cut at a "." and then
// parsed rather than parsed whole: an Ubuntu or a Debian package appends its
// own revision, a MariaDB server appends "-MariaDB", and none of that changes
// which major wrote the file. A banner whose first component is not a number at
// all reports false, and Import then says nothing rather than guessing.
//
// It reads the SERVER version, not mysqldump's own. The first line of a dump
// says "MySQL dump 10.13", which is the format version of the file and has been
// 10.13 since 2005; it identifies nothing. This is the one place the two
// importers' version checks are about different things, and
// internal/importer/postgres/ir.go's DumpVersion is the counterpart that is
// about the tool.
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
