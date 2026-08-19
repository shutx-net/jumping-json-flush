package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

const versionUsage = `Print the version of jjf, along with the Go toolchain and
platform it was built for.

Usage:
  jjf version
`

// version is injected at release time via -ldflags "-X main.version=...".
var version = ""

// buildVersion prefers the -ldflags injected value and falls back to the
// version the go command stamped from VCS metadata. Neither alone is enough:
// ldflags does not reach `go install`, and VCS stamping depends on the clone
// having tags and a clean working tree.
func buildVersion() string {
	if version != "" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "" {
		return "(unknown)"
	}
	return bi.Main.Version // "v1.2.3", "v0.0.0-<ts>-<sha>", or "(devel)"
}

// runVersion implements the version subcommand.
func runVersion(args []string, stdout, stderr io.Writer) error {
	cmd := newCommand("version", versionUsage)
	operands, err := cmd.parse(args, stdout, stderr)
	if err != nil {
		return err
	}
	if len(operands) != 0 {
		fmt.Fprint(stderr, "jjf: version takes no arguments\n\n")
		cmd.printUsage(stderr)
		return exitcode.Wrap(exitcode.InvalidInput, "", errAlreadyReported)
	}

	fmt.Fprintf(stdout, "jjf %s\n", buildVersion())
	fmt.Fprintf(stdout, "built with %s for %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
