package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// out is the destination writer; overridable in tests.
var out io.Writer = os.Stdout

const (
	ErrNotFound = "NOT_FOUND"
	ErrConflict = "CONFLICT"
	ErrUsage    = "USAGE"
	ErrGit      = "GIT"
	ErrState    = "STATE"
)

// Emit prints data as JSON when jsonMode, else prints the human string.
func Emit(jsonMode bool, human string, data any) error {
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}
	_, err := fmt.Fprintln(out, human)
	return err
}

// Errorf prints an error (JSON envelope when jsonMode, else "Error: ..." to stderr)
// and returns a rendered-marked error so callers can `return output.Errorf(...)`
// and the top-level Execute wrapper will not print a second envelope.
func Errorf(jsonMode bool, code, format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"error": msg, "code": code})
	} else {
		fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", code, msg)
	}
	return rendered{err: fmt.Errorf("%s: %s", code, msg)}
}

// rendered marks errors already printed by Errorf so the top-level
// Execute wrapper does not print a second envelope for them.
type rendered struct{ err error }

func (r rendered) Error() string { return r.err.Error() }
func (r rendered) Unwrap() error { return r.err }

// IsRendered reports whether err was already emitted by Errorf/Render.
func IsRendered(err error) bool {
	var r rendered
	return errors.As(err, &r)
}

// Render emits a JSON/text envelope for an error that was NOT produced by
// Errorf (raw filesystem/parsing/Cobra errors), mapping it to a fallback code,
// and returns a rendered-marked error so callers print exactly once.
func Render(jsonMode bool, err error) error {
	if err == nil {
		return nil
	}
	if IsRendered(err) {
		return err // already emitted by Errorf; do not double-print
	}
	code := classify(err)
	msg := err.Error()
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"error": msg, "code": code})
	} else {
		fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", code, msg)
	}
	return rendered{err: fmt.Errorf("%s: %s", code, msg)}
}

// classify maps a raw error to a coarse error code for the envelope.
func classify(err error) string {
	switch {
	case os.IsNotExist(err):
		return ErrNotFound
	case os.IsExist(err):
		return ErrConflict
	default:
		return ErrState
	}
}
