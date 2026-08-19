// Package exitcode defines the process exit codes jjf uses and the error
// wrapper that carries one. It deliberately has no dependencies so every other
// package can classify its own failures.
package exitcode

import "errors"

// Code is a jjf process exit code.
type Code int

const (
	OK           Code = 0 // success
	General      Code = 1 // general error
	InvalidInput Code = 2 // invalid input: bad flags, missing file, malformed JSON
	SchemaFailed Code = 3 // the document does not conform to the JSON Schema
	OutputFailed Code = 4 // the artifact could not be generated or written
)

// CodedError attaches an exit code to an error.
type CodedError struct {
	Code Code
	Op   string
	Err  error
}

// Error returns the wrapped message, prefixed with the operation when one was
// recorded.
func (e *CodedError) Error() string {
	if e.Op == "" {
		return e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error so that errors.Is and errors.As keep
// working through the chain.
func (e *CodedError) Unwrap() error { return e.Err }

// Wrap attaches code to err. It returns nil when err is nil.
func Wrap(code Code, op string, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Op: op, Err: err}
}

// Of reports the exit code err carries. Unclassified errors are General.
func Of(err error) Code {
	if err == nil {
		return OK
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return General
}
