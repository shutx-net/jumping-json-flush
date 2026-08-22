package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/export/ddl"
	"github.com/shutx-net/jumping-json-flush/internal/export/dot"
	"github.com/shutx-net/jumping-json-flush/internal/export/xlsx"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

// exportUsageHead is the part of the usage text above the format list. The
// list itself is generated from the format table so the two cannot drift.
const exportUsageHead = `Generate a design document from a database design document. The input is
validated against the jjf database design schema first, so a document that
fails "jjf validate" produces no output at all.

Usage:
  jjf export <format> <input.json> [-o <output>]

Formats:
`

// exportUsageTail is the part below the format list. It opens with the blank
// line that separates it from the list.
const exportUsageTail = `
Without -o the output goes next to the input, with its extension replaced by
the one of the chosen format. "-o -" writes to standard output, which must not
be a terminal for a binary format such as xlsx.

Only ddl refuses a document that contradicts itself. A document a database
would reject makes no useful SQL, while it still makes a useful workbook and a
useful diagram - so xlsx and dot render it and "jjf validate" is where those
contradictions are reported.
`

// exportFormat is one format the export subcommand can produce.
type exportFormat struct {
	// name is what the user types as <format>.
	name string
	// ext replaces the input's extension when -o is left out.
	ext string
	// summary is the one line the usage text prints beside the name.
	summary string
	// binary is true when the format's bytes must not be written to a
	// terminal. The question is not "is this a zip" but "would this garble the
	// screen": DOT is text, so it says false and may be written to a terminal,
	// exactly as the JSON of "jjf import" may.
	binary bool
	// accept reports why this format refuses doc, or nil when it accepts it.
	// Only ddl sets it: see the entry below for why xlsx and dot deliberately
	// do not.
	accept func(doc *model.Document) error
	// render writes doc to w in this format.
	render func(w io.Writer, doc *model.Document) error
}

// exportFormats lists the formats in the order the usage text prints them.
//
// It is a function returning a fresh slice rather than a package-level var,
// because a package-level slice of structs holding function values is exactly
// the mutable package-level state AGENTS.md rules out. It is called a handful
// of times per process, so the allocation is beside the point.
//
// xlsx stays first: it is the format every document already describes.
func exportFormats() []exportFormat {
	return []exportFormat{
		{
			name: "xlsx", ext: ".xlsx", summary: "Excel workbook", binary: true,
			render: func(w io.Writer, doc *model.Document) error {
				return xlsx.Export(w, doc, xlsx.DefaultOptions())
			},
		},
		{
			name: "dot", ext: ".dot", summary: "Graphviz DOT entity relationship diagram",
			render: dot.Export,
		},
		// The one entry whose extension is not its name. A ".ddl" file is
		// nothing, and calling the format "sql" would promise arbitrary SQL
		// rather than the one schema-creating script this writes.
		//
		// It is also the only entry with an accept, and that asymmetry is
		// deliberate rather than an inconsistency to be tidied away. A
		// document that contradicts itself still makes a useful workbook and
		// a useful diagram - internal/export/dot draws a foreign key with no
		// target as a dashed stub on purpose and says in its own package
		// comment that it reports nothing, ever. DDL a database rejects is
		// worth nothing at all, so this format alone refuses what the other
		// two render.
		{
			name: "ddl", ext: ".sql", summary: "PostgreSQL DDL script (.sql)",
			accept: ddl.Accept, render: ddl.Export,
		},
	}
}

// lookupExportFormat finds the format called name.
//
// The comparison is exact: XLSX and Dot are unknown formats, just as MySQL is
// an unknown import dialect today. A slice walk rather than a map, because the
// table is a handful of entries and its order is meaningful.
func lookupExportFormat(name string) (exportFormat, bool) {
	for _, f := range exportFormats() {
		if f.name == name {
			return f, true
		}
	}
	return exportFormat{}, false
}

// exportFormatNames lists the format names for the unsupported-format message.
func exportFormatNames() string {
	names := make([]string, 0, len(exportFormats()))
	for _, f := range exportFormats() {
		names = append(names, f.name)
	}
	return strings.Join(names, ", ")
}

// exportUsage builds the usage text with one line per format, so that a format
// added to the table appears in the help without anyone remembering to add it.
func exportUsage() string {
	width := 0
	for _, f := range exportFormats() {
		width = max(width, len(f.name))
	}

	var b strings.Builder
	b.WriteString(exportUsageHead)
	for _, f := range exportFormats() {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, f.name, f.summary)
	}
	b.WriteString(exportUsageTail)
	return b.String()
}

// stdoutPath is the output path that means standard output.
const stdoutPath = "-"

// errStdoutIsTerminal reports that "-o -" was given with a terminal on the other
// end of standard output. Writing a zip archive there would only garble the
// screen.
//
// The guard applies to the formats whose table entry sets binary, which today
// is xlsx alone - hence the wording, which is quoted verbatim in the
// documentation. The day a second binary format is added, this sentence has to
// be generalised along with those quotations.
var errStdoutIsTerminal = errors.New("refusing to write a workbook to the terminal; redirect standard output or pass -o <file>")

// runExport implements the export subcommand.
func runExport(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("export", exportUsage())
	out := cmd.fs.String("o", "", `output path, or "-" for standard output`)
	operands, err := cmd.parse(args, stdout, stderr)
	if err != nil {
		return err
	}
	if len(operands) != 2 {
		fmt.Fprintf(stderr, "jjf: export takes a format and exactly one input file, got %d argument(s)\n\n", len(operands))
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}
	format, input := operands[0], operands[1]
	f, ok := lookupExportFormat(format)
	if !ok {
		fmt.Fprintf(stderr, "jjf: unsupported format %q; supported formats: %s\n\n", format, exportFormatNames())
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}

	// The order is deliberate: the format is checked before the input is read,
	// so an unknown format never opens a file; the document is validated and
	// decoded before a single output byte is produced, so a document that
	// fails validation produces no output at all in any format; and the format
	// is asked whether it accepts the document before the output path is even
	// decided, so a refusal is reported as the document's fault rather than as
	// whatever writeFileAtomically makes of it afterwards.
	doc, err := loadDocument(input)
	if err != nil {
		return err
	}

	if f.accept != nil {
		if err := f.accept(doc); err != nil {
			return reportRefusal(stderr, input, err)
		}
	}

	if *out == stdoutPath {
		if f.binary && isTerminal(stdout) {
			return exitcode.Wrap(exitcode.InvalidInput, "", errStdoutIsTerminal)
		}
		return f.render(stdout, doc)
	}

	dest := *out
	if dest == "" {
		dest = strings.TrimSuffix(input, filepath.Ext(input)) + f.ext
	}
	// Written atomically, so a failed export leaves nothing behind whichever
	// format it was.
	if err := writeFileAtomically(dest, func(w io.Writer) error { return f.render(w, doc) }); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: written\n", dest)
	return nil
}

// reportRefusal prints why a format refused the document and returns the coded
// error the exit status comes from.
//
// The itemised lines come first because they are what explains the failure, and
// the coded error carries the one-line summary - the shape runValidate already
// uses under -strict, for the same reason. The path-prefixed form is reserved
// for per-object findings, where a locator is useful; the dbms errors are about
// the invocation as a whole, fit on one line, already carry their exit code and
// go through the ordinary "jjf: " channel untouched.
func reportRefusal(stderr io.Writer, input string, err error) error {
	var re *ddl.RefusedError
	if !errors.As(err, &re) {
		return err
	}
	writeFindings(stderr, input, "error", re.Findings)
	return exitcode.Wrap(exitcode.InvalidInput, "",
		fmt.Errorf("%d problem(s) prevent PostgreSQL DDL generation", len(re.Findings)))
}

// loadDocument reads, validates and decodes a database design document.
//
// The order matters and is the same for every subcommand: a schema violation
// has to be reported as one (exit 3) rather than as whatever the decoder makes
// of it, and an unsupported format version has to survive schema validation to
// be diagnosed at all.
func loadDocument(input string) (*model.Document, error) {
	raw, err := os.ReadFile(input)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.InvalidInput, "", err)
	}
	validator, err := schema.NewValidator()
	if err != nil {
		return nil, exitcode.Wrap(exitcode.General, "", err)
	}
	if err := validator.Validate(input, raw); err != nil {
		return nil, err
	}
	// Decoding checks the one rule the schema cannot express: whether this build
	// understands the document's format version.
	return model.Decode(raw)
}

// isTerminal reports whether w writes to a character device, which is how "-o -"
// tells a pipe or a file apart from a terminal it must not fill with binary. It
// uses nothing but the standard library, so golang.org/x/term stays out of the
// build. Anything that is not an *os.File - a buffer in a test, for instance -
// is not a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
