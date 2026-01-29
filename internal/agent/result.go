package agent

import (
	"io"
	"sync"
	"sync/atomic"
)

// ExecutionResult wraps command execution output with lifecycle management.
// It implements io.ReadCloser and provides access to exit code and stderr
// after Close() is called.
//
// This type replaces the previous pattern of returning io.Reader and requiring
// callers to type-assert for io.Closer, ExitCoder, and StderrProvider.
// ExecutionResult makes cleanup mandatory and provides a clean API.
type ExecutionResult struct {
	reader       io.ReadCloser
	exitCode     int
	exitCodeFunc func() int
	stderr       string
	stderrFunc   func() string
	closeOnce    sync.Once
	closed       atomic.Bool
}

// NewExecutionResult creates a new ExecutionResult wrapping the given reader.
// The exitCodeFunc and stderrFunc are called during Close() to capture the
// final exit code and stderr output after the underlying process completes.
func NewExecutionResult(reader io.ReadCloser, exitCodeFunc func() int, stderrFunc func() string) *ExecutionResult {
	return &ExecutionResult{
		reader:       reader,
		exitCodeFunc: exitCodeFunc,
		stderrFunc:   stderrFunc,
	}
}

// Read implements io.Reader.
func (r *ExecutionResult) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

// Close implements io.Closer. It closes the underlying reader and captures
// the exit code and stderr from the completed process.
// Close is safe for concurrent calls - only the first call performs cleanup.
// After Close returns, ExitCode() and Stderr() return valid values.
func (r *ExecutionResult) Close() error {
	var closeErr error
	r.closeOnce.Do(func() {
		closeErr = r.reader.Close()
		if r.exitCodeFunc != nil {
			r.exitCode = r.exitCodeFunc()
		}
		if r.stderrFunc != nil {
			r.stderr = r.stderrFunc()
		}
		r.closed.Store(true)
	})
	return closeErr
}

// ExitCode returns the process exit code.
// Only valid after Close() has been called. Returns 0 if process succeeded,
// -1 if process could not be waited on, or the actual exit code otherwise.
func (r *ExecutionResult) ExitCode() int {
	return r.exitCode
}

// Stderr returns captured stderr output.
// Only valid after Close() has been called. Returns empty string if no
// stderr was captured.
func (r *ExecutionResult) Stderr() string {
	return r.stderr
}

// IsClosed returns true if Close() has been called.
// Safe for concurrent use.
func (r *ExecutionResult) IsClosed() bool {
	return r.closed.Load()
}
