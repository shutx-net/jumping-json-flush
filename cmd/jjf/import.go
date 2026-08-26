package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/importer/mysql"
	"github.com/shutx-net/jumping-json-flush/internal/importer/postgres"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// importUsageHead is the part of the usage text above the dialect list. The
// list itself is generated from the dialect table so the two cannot drift, the
// way exportUsageHead and exportFormats already do it for the other command.
const importUsageHead = `Convert a database schema dump into a jjf database design document. The
generated document is validated against the jjf database design schema before
it is written, so a document that would fail "jjf validate" is never produced.

Usage:
  jjf import <dialect> <input.sql> [-o <output>]

Dialects:
`

// importUsageTail is the part below the dialect list. It opens with the blank
// line that separates it from the list.
const importUsageTail = `
Without -o the output goes next to the input, with its extension replaced by
".json". "-o -" writes to standard output.

-schema belongs to postgres alone and is an error for any other dialect: a
MySQL schema IS a database, so there is no second level to choose.

Statements jjf does not map are skipped in silence. A statement it does map but
that carries a clause the design format cannot hold is reported as a warning on
standard error, naming the line; -strict turns those warnings into an error and
writes nothing.
`

// warning is one importer diagnostic, in the shape this command prints.
//
// It exists so that writeDiagnostics stays a single function over a single
// type. Both importer packages export a Diagnostic of exactly these two fields
// and neither imports the other; adapting them here, at the only place in the
// program that ever sees both, is six lines per dialect and keeps that
// independence. A shared Diagnostic type in a third package was the
// alternative: it would couple the two importers forever for a two-field
// struct.
type warning struct {
	Line    int
	Message string
}

// importDialect is one source dialect the import subcommand can read.
type importDialect struct {
	// name is what the user types as <dialect>.
	name string
	// summary is the one line the usage text prints beside the name.
	summary string
	// schemaFlag is true when -schema means something for this dialect. Every
	// other dialect refuses the flag when it was explicitly set, rather than
	// accepting it and quietly doing nothing - which is the kind of silent lie
	// internal/export/ddl refuses when it declines to guess a document's target.
	schemaFlag bool
	// imports reads a dump and returns the document and what was lost.
	imports func(src []byte, opt importOptions) (*model.Document, []warning, error)
}

// importOptions carries what the flags said, in the one shape every dialect's
// imports function is handed. Schema is meaningful only for a dialect whose
// schemaFlag is true; runImport has already refused it for the rest.
type importOptions struct {
	Schema   string
	Database string
	Source   string
}

// importDialects lists the dialects in the order the usage text prints them.
//
// It is a function returning a fresh slice rather than a package-level var, for
// the reason exportFormats gives: a package-level slice of structs holding
// function values is exactly the mutable package-level state AGENTS.md rules
// out.
//
// postgres stays first because it was first, and because -schema is documented
// against it.
func importDialects() []importDialect {
	return []importDialect{
		{
			name: "postgres", summary: "PostgreSQL, from pg_dump --schema-only", schemaFlag: true,
			imports: func(src []byte, opt importOptions) (*model.Document, []warning, error) {
				doc, diags, err := postgres.Import(src, postgres.Options{
					Schema:   opt.Schema,
					Database: opt.Database,
					Source:   opt.Source,
				})
				out := make([]warning, 0, len(diags))
				for _, d := range diags {
					out = append(out, warning{Line: d.Line, Message: d.Message})
				}
				return doc, out, err
			},
		},
		// No Schema is passed here, and there is no field to pass it in:
		// mysql.Options has none. See importDialect.schemaFlag.
		{
			name: "mysql", summary: "MySQL, from mysqldump --no-data",
			imports: func(src []byte, opt importOptions) (*model.Document, []warning, error) {
				doc, diags, err := mysql.Import(src, mysql.Options{
					Database: opt.Database,
					Source:   opt.Source,
				})
				out := make([]warning, 0, len(diags))
				for _, d := range diags {
					out = append(out, warning{Line: d.Line, Message: d.Message})
				}
				return doc, out, err
			},
		},
	}
}

// lookupImportDialect finds the dialect called name.
//
// The comparison is exact, as lookupExportFormat's is: MySQL and Postgres are
// unknown dialects. A slice walk rather than a map, because the table is a
// handful of entries and its order is meaningful.
func lookupImportDialect(name string) (importDialect, bool) {
	for _, d := range importDialects() {
		if d.name == name {
			return d, true
		}
	}
	return importDialect{}, false
}

// importDialectNames lists the dialect names for the unsupported-dialect
// message.
func importDialectNames() string {
	names := make([]string, 0, len(importDialects()))
	for _, d := range importDialects() {
		names = append(names, d.name)
	}
	return strings.Join(names, ", ")
}

// importUsage builds the usage text with one line per dialect, so that a
// dialect added to the table appears in the help without anyone remembering to
// add it.
func importUsage() string {
	width := 0
	for _, d := range importDialects() {
		width = max(width, len(d.name))
	}

	var b strings.Builder
	b.WriteString(importUsageHead)
	for _, d := range importDialects() {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, d.name, d.summary)
	}
	b.WriteString(importUsageTail)
	return b.String()
}

// extJSON is the extension used when -o is left out.
const extJSON = ".json"

// generatedDocument names the importer's own output in a schema report, so that
// a failure there cannot be mistaken for a complaint about the user's dump.
const generatedDocument = "the generated document"

// schemaFlagName is the flag whose meaning depends on the dialect. It is named
// once because runImport both registers it and, for a dialect that has no use
// for it, reports it.
const schemaFlagName = "schema"

// runImport implements the import subcommand.
func runImport(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("import", importUsage())
	out := cmd.fs.String("o", "", `output path, or "-" for standard output`)
	schemaName := cmd.fs.String(schemaFlagName, postgres.DefaultSchema, "PostgreSQL schema to import")
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
	d, ok := lookupImportDialect(dialect)
	if !ok {
		fmt.Fprintf(stderr, "jjf: unsupported dialect %q; supported dialects: %s\n\n", dialect, importDialectNames())
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}
	// The flag has to be REGISTERED for every dialect, because cmd/jjf/flags.go
	// permutes flags and operands so that a flag may appear before them - so
	// the dialect is not known until parsing is done. Refusing a value that was
	// explicitly SET is therefore the only honest option available, and
	// FlagSet.Visit is what tells "set" from "left at its default".
	if !d.schemaFlag && flagWasSet(cmd, schemaFlagName) {
		return exitcode.Wrap(exitcode.InvalidInput, "",
			fmt.Errorf("-%s does not apply to the %s dialect: a %s dump holds one database, which is what the document describes",
				schemaFlagName, d.name, d.name))
	}

	raw, err := os.ReadFile(input)
	if err != nil {
		return exitcode.Wrap(exitcode.InvalidInput, "", err)
	}

	doc, diags, err := d.imports(raw, importOptions{
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

// flagWasSet reports whether the named flag appeared on the command line, as
// opposed to having been left at its default.
//
// FlagSet.Visit walks only the flags that were set, which is the whole of the
// mechanism. It is a walk rather than a lookup because the standard library
// offers no other way to ask, and the flag set has four entries.
func flagWasSet(cmd *command, name string) bool {
	found := false
	cmd.fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// writeDiagnostics prints the importer's warnings.
//
// The source:line: prefix is the shape editors and CI annotators already parse,
// and it deliberately differs from the "jjf: " prefix, which is reserved for
// errors.
func writeDiagnostics(w io.Writer, source string, diags []warning) {
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
