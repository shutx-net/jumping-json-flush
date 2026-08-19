package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

// command wraps a FlagSet with the usage text of one subcommand and an argument
// parser that tolerates flags anywhere on the line.
type command struct {
	fs     *flag.FlagSet
	header string
}

// newCommand builds a subcommand whose usage starts with header. header is
// printed verbatim, so it must end with a newline.
func newCommand(name, header string) *command {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	// Silence the FlagSet: this package decides what to print and where.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return &command{fs: fs, header: header}
}

// printUsage writes the subcommand's usage, including its flag defaults.
func (c *command) printUsage(w io.Writer) {
	fmt.Fprint(w, c.header)

	flags := 0
	c.fs.VisitAll(func(*flag.Flag) { flags++ })
	if flags == 0 {
		return
	}

	fmt.Fprint(w, "\nFlags:\n")
	c.fs.SetOutput(w)
	c.fs.PrintDefaults()
	c.fs.SetOutput(io.Discard)
}

// parse permutes args so flags may appear before, between, or after operands.
// The standard flag package stops at the first non-flag argument, so a plain
// fs.Parse silently drops "-o out.xlsx" in "export xlsx in.json -o out.xlsx".
// "--" ends flag parsing for ALL remaining arguments, not just the next one.
//
// Checking how many operands came back is the caller's job.
func (c *command) parse(args []string, stdout, stderr io.Writer) ([]string, error) {
	var operands []string
	for len(args) > 0 {
		err := c.fs.Parse(args)
		switch {
		case errors.Is(err, flag.ErrHelp):
			c.printUsage(stdout)
			return nil, errHelpHandled
		case err != nil:
			fmt.Fprintf(stderr, "jjf: %v\n\n", err)
			c.printUsage(stderr)
			return nil, exitcode.Wrap(exitcode.InvalidInput, "", fmt.Errorf("%w: %w", errAlreadyReported, err))
		}

		rest := c.fs.Args()
		if len(rest) == 0 {
			break
		}
		if args[0] == "--" { // everything after "--" is an operand
			return append(operands, rest...), nil
		}
		operands = append(operands, rest[0])
		args = rest[1:]
	}
	return operands, nil
}
