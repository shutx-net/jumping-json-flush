package main

import (
	"fmt"
	"io"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

const validateUsage = `Check a database design document against the jjf database design
schema and report every violation found.

Usage:
  jjf validate <input.json>
`

// runValidate implements the validate subcommand.
func runValidate(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("validate", validateUsage)
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

	// Validation is exactly what export does before it writes anything, so both
	// go through one loader.
	if _, err := loadDocument(input); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: OK\n", input)
	return nil
}
