package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/importer/postgres"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

const importUsage = `Convert a PostgreSQL schema dump into a jjf database design document.
The input is the output of "pg_dump --schema-only". The generated document is
validated against the jjf database design schema before it is written, so a
document that would fail "jjf validate" is never produced.

Usage:
  jjf import <dialect> <input.sql> [-o <output>]

Dialects:
  postgres  PostgreSQL, from pg_dump --schema-only

Without -o the output goes next to the input, with its extension replaced by
".json". "-o -" writes to standard output.

Statements jjf does not map are skipped in silence. A statement it does map but
that carries a clause the design format cannot hold is reported as a warning on
standard error, naming the line; -strict turns those warnings into an error and
writes nothing.
`

// dialectPostgres is the only source dialect this build understands.
const dialectPostgres = "postgres"

// extJSON is the extension used when -o is left out.
const extJSON = ".json"

// generatedDocument names the importer's own output in a schema report, so that
// a failure there cannot be mistaken for a complaint about the user's dump.
const generatedDocument = "the generated document"

// runImport implements the import subcommand.
func runImport(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("import", importUsage)
	out := cmd.fs.String("o", "", `output path, or "-" for standard output`)
	schemaName := cmd.fs.String("schema", postgres.DefaultSchema, "PostgreSQL schema to import")
	database := cmd.fs.String("database", "", "database name for the generated document; defaults to the input file name")
	strict := cmd.fs.Bool("strict", false, "treat warnings as errors")
	operands, err := cmd.parse(args, stdout, stderr)
	if err != nil {
		return err
	}
	if len(operands) != 2 {
		fmt.Fprintf(stderr, "jjf: import takes a dialect and exactly one input file, got %d argument(s)\n\n", len(operands))
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}
	dialect, input := operands[0], operands[1]
	if dialect != dialectPostgres {
		fmt.Fprintf(stderr, "jjf: unsupported dialect %q; supported dialects: %s\n\n", dialect, dialectPostgres)
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}

	raw, err := os.ReadFile(input)
	if err != nil {
		return exitcode.Wrap(exitcode.InvalidInput, "", err)
	}

	doc, diags, err := postgres.Import(raw, postgres.Options{
		Schema:   *schemaName,
		Database: *database,
		Source:   input,
	})
	// The warnings are printed even when the import failed: they are usually
	// what explains the failure.
	writeDiagnostics(stderr, input, diags)
	if err != nil {
		return err
	}
	if *strict && len(diags) > 0 {
		return exitcode.Wrap(exitcode.InvalidInput, "", fmt.Errorf("%d warning(s) with -strict", len(diags)))
	}

	encoded, err := model.Encode(doc)
	if err != nil {
		return err
	}
	if err := validateGenerated(encoded); err != nil {
		return err
	}

	// Unlike export, "-o -" is not refused for a terminal. A workbook on a
	// terminal is garbage; JSON on a terminal is something people reasonably
	// want to look at.
	if *out == stdoutPath {
		_, err := stdout.Write(encoded)
		return err
	}

	dest := *out
	if dest == "" {
		dest = strings.TrimSuffix(input, filepath.Ext(input)) + extJSON
	}
	if err := writeFileAtomically(dest, func(w io.Writer) error {
		_, err := w.Write(encoded)
		return err
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: written\n", dest)
	return nil
}

// writeDiagnostics prints the importer's warnings.
//
// The source:line: prefix is the shape editors and CI annotators already parse,
// and it deliberately differs from the "jjf: " prefix, which is reserved for
// errors.
func writeDiagnostics(w io.Writer, source string, diags []postgres.Diagnostic) {
	for _, d := range diags {
		if d.Line == 0 {
			fmt.Fprintf(w, "%s: warning: %s\n", source, d.Message)
			continue
		}
		fmt.Fprintf(w, "%s:%d: warning: %s\n", source, d.Line, d.Message)
	}
}

// validateGenerated checks the importer's own output against the schema before
// a single byte is written.
//
// This is a safety net against a bug in the importer, not a check on the user's
// dump: a document jjf itself produced and jjf itself would then reject is the
// worst thing this command could leave behind. Compiling the schema costs a
// millisecond, so it always runs.
func validateGenerated(raw []byte) error {
	validator, err := schema.NewValidator()
	if err != nil {
		return exitcode.Wrap(exitcode.General, "", err)
	}
	return validator.Validate(generatedDocument, raw)
}
