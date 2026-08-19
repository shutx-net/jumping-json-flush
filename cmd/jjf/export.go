package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/export/xlsx"
	"github.com/shutx-net/jumping-json-flush/internal/model"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

const exportUsage = `Generate a design document from a database design document. The input is
validated against the jjf database design schema first, so a document that
fails "jjf validate" produces no output at all.

Usage:
  jjf export <format> <input.json> [-o <output>]

Formats:
  xlsx  Excel workbook

Without -o the output goes next to the input, with its extension replaced by
the one of the chosen format. "-o -" writes to standard output, which must not
be a terminal.
`

// formatXLSX is the only export format this build supports.
const formatXLSX = "xlsx"

// extXLSX is the extension used when -o is left out.
const extXLSX = ".xlsx"

// stdoutPath is the output path that means standard output.
const stdoutPath = "-"

// errStdoutIsTerminal reports that "-o -" was given with a terminal on the other
// end of standard output. Writing a zip archive there would only garble the
// screen.
var errStdoutIsTerminal = errors.New("refusing to write a workbook to the terminal; redirect standard output or pass -o <file>")

// runExport implements the export subcommand.
func runExport(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("export", exportUsage)
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
	if format != formatXLSX {
		fmt.Fprintf(stderr, "jjf: unsupported format %q; supported formats: %s\n\n", format, formatXLSX)
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}

	doc, err := loadDocument(input)
	if err != nil {
		return err
	}

	if *out == stdoutPath {
		if isTerminal(stdout) {
			return exitcode.Wrap(exitcode.InvalidInput, "", errStdoutIsTerminal)
		}
		return xlsx.Export(stdout, doc, xlsx.DefaultOptions())
	}

	dest := *out
	if dest == "" {
		dest = strings.TrimSuffix(input, filepath.Ext(input)) + extXLSX
	}
	if err := exportToFile(dest, doc); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: written\n", dest)
	return nil
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

// exportToFile writes the workbook through a temporary file in the destination
// directory and renames it into place, so that a failure part way through leaves
// no half written document behind.
func exportToFile(path string, doc *model.Document) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		// The error names the temporary file, which the caller never asked for;
		// report the destination they did ask for instead.
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = fmt.Errorf("%s: %w", path, pe.Err)
		}
		return exitcode.Wrap(exitcode.OutputFailed, "cannot create output file", err)
	}
	// Remove the temporary file unless the rename below has already claimed it.
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := xlsx.Export(tmp, doc, xlsx.DefaultOptions()); err != nil {
		_ = tmp.Close()
		return err
	}
	// CreateTemp makes the file readable by its owner alone, which is not what a
	// document handed to other people should be. A failure to widen it is not
	// worth abandoning the export over.
	_ = tmp.Chmod(0o644)
	if err := tmp.Close(); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "close output file", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "rename output file", err)
	}
	return nil
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
