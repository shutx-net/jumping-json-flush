package main

import (
	"fmt"
	"io"

	"github.com/shutx-net/jumping-json-flush/internal/check"
	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

const validateUsage = `Check a database design document against the jjf database design
schema and report every violation found.

The document is then checked against itself: whether the columns named by
its keys and indexes exist, whether every foreign key names a table this
document defines, matches it column for column and targets columns that
table constrains to be unique, whether a primary key column is declared
nullable, whether one table uses a column or constraint name twice, and
whether a column's default is empty or does not read as a SQL expression.
Those are reported as warnings on standard error and leave the exit code
successful; -strict turns them into an error.

Usage:
  jjf validate <input.json>
`

// runValidate implements the validate subcommand.
func runValidate(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("validate", validateUsage)
	strict := cmd.fs.Bool("strict", false, "treat warnings as errors")
	operands, err := cmd.parse(args, stdout, stderr)
	if err != nil {
		return err
	}
	if len(operands) != 1 {
		fmt.Fprintf(stderr, "jjf: validate takes exactly one input file, got %d\n\n", len(operands))
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}
	input := operands[0]

	// Schema validation and decoding are exactly what export does before it
	// writes anything, so both go through one loader. The referential checks
	// below stay out of it on purpose: export renders whatever the document
	// claims - internal/export/dot draws a foreign key with no target as a
	// dashed stub node - and moving the call into loadDocument would make every
	// command pay for a report only this one prints.
	doc, err := loadDocument(input)
	if err != nil {
		return err
	}

	// A document that failed the schema cannot be meaningfully checked against
	// itself, which is why this comes after loadDocument and never instead of it.
	findings := check.Document(doc)
	// The findings are printed before it is decided what they cost: they are
	// what explains the failure, so -strict must change what happens next, not
	// what the user is told.
	writeFindings(stderr, input, findings)

	if *strict && len(findings) > 0 {
		// InvalidInput (2), not SchemaFailed (3). Exit 3 is reserved for "does
		// not conform to the JSON Schema" and is produced by internal/schema
		// alone; a referential finding is deliberately not a schema violation,
		// and the failure here comes from the invocation asking for strictness.
		// "jjf import -strict" says the same sentence for the same reason.
		return exitcode.Wrap(exitcode.InvalidInput, "", fmt.Errorf("%d warning(s) with -strict", len(findings)))
	}

	if len(findings) > 0 {
		fmt.Fprintf(stdout, "%s: OK, %d warning(s)\n", input, len(findings))
		return nil
	}
	fmt.Fprintf(stdout, "%s: OK\n", input)
	return nil
}

// writeFindings prints the checker's warnings.
//
// The "source: warning: " prefix is the shape editors and CI annotators already
// parse, and it deliberately differs from the "jjf: " prefix, which is reserved
// for errors. It is the line-less form of the importer's writeDiagnostics in
// import.go: there is no line number to give, because the decoded model carries
// no source positions. The two stay separate rather than being generalised -
// they take different types and are four lines each.
func writeFindings(w io.Writer, source string, findings []check.Finding) {
	for _, f := range findings {
		fmt.Fprintf(w, "%s: warning: %s\n", source, f)
	}
}
