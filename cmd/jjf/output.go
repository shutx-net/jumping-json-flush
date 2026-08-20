package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shutx-net/jumping-json-flush/internal/exitcode"
)

// writeFileAtomically writes through a temporary file in the destination
// directory and renames it into place, so that a failure part way through
// leaves no half written document behind.
//
// Both subcommands that produce a file go through here: a half written workbook
// and a half written design document are equally useless, and the rules about
// permissions and error messages should not be decided twice.
func writeFileAtomically(path string, write func(io.Writer) error) error {
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

	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	// CreateTemp makes the file readable by its owner alone, which is not what a
	// document handed to other people should be. A failure to widen it is not
	// worth abandoning the write over.
	_ = tmp.Chmod(0o644)
	if err := tmp.Close(); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "close output file", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return exitcode.Wrap(exitcode.OutputFailed, "rename output file", err)
	}
	return nil
}
