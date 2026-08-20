// Command jjf validates database design specification JSON documents and
// generates human-readable design artifacts such as Excel workbooks from them.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
	"github.com/shutx-net/jumping-json-flush/internal/schema"
)

const rootUsage = `jjf manages database design specifications as JSON and generates
human readable design documents from them. The JSON is the source of truth;
generated documents are derived artifacts.

Usage:
  jjf <command> [arguments]

Commands:
  import <dialect> <input.sql> -o <output.json>
        build a document from a schema dump (dialects: postgres)
  validate <input.json>
        check a document against the jjf database design schema
  export <format> <input.json> -o <output>
        generate a design document (formats: xlsx)
  version
        print version information

Run "jjf <command> -h" for the options of one command.

Exit codes:
  0  success
  1  general error
  2  invalid input
  3  schema validation error
  4  output generation error
`

// errHelpHandled reports that help was requested and already written to stdout.
// It is not a failure.
var errHelpHandled = errors.New("help requested")

// errAlreadyReported reports that a subcommand has written its own message and
// usage, so the top level must not print anything more.
var errAlreadyReported = errors.New("already reported")

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is the testable entry point. It never calls os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		fmt.Fprint(stderr, rootUsage)
		return int(exitcode.InvalidInput)
	case args[0] == "-h", args[0] == "--help", args[0] == "help":
		fmt.Fprint(stdout, rootUsage)
		return int(exitcode.OK)
	}

	var err error
	switch args[0] {
	case "import":
		err = runImport(args[1:], stdout, stderr)
	case "validate":
		err = runValidate(args[1:], stdout, stderr)
	case "export":
		err = runExport(args[1:], stdout, stderr)
	case "version":
		err = runVersion(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "jjf: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, rootUsage)
		return int(exitcode.InvalidInput)
	}
	return reportAndCode(err, stderr)
}

// reportAndCode writes whatever err still needs said and returns the exit code
// it carries.
func reportAndCode(err error, stderr io.Writer) int {
	if err == nil || errors.Is(err, errHelpHandled) {
		return int(exitcode.OK)
	}

	var ide *schema.InvalidDocumentError
	switch {
	case errors.As(err, &ide):
		ide.WriteReport(stderr)
	case errors.Is(err, errAlreadyReported):
		// the subcommand printed its own message and usage
	default:
		fmt.Fprintf(stderr, "jjf: %v\n", err)
	}
	return int(exitcode.Of(err))
}
