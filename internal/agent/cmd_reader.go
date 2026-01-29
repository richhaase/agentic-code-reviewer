package agent

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

// cmdReader wraps an io.Reader and ensures the command is waited on when closed.
// It implements io.Closer, ExitCoder, and StderrProvider to provide process exit
// code and stderr output after Close().
// This type is used by all agent implementations (codex, claude, gemini) to manage
// subprocess lifecycle.
type cmdReader struct {
	io.Reader
	cmd          *exec.Cmd
	ctx          context.Context
	stderr       *bytes.Buffer
	exitCode     int
	closeOnce    sync.Once
	tempFilePath string // temp file to clean up on Close (used by ref-file pattern)
}

// Close implements io.Closer and waits for the command to complete.
// After Close returns, ExitCode() will return the process exit code.
// If the context was canceled or timed out, it kills the entire process group
// to ensure no orphaned processes are left behind.
// Close is safe for concurrent calls - only the first call performs cleanup.
func (r *cmdReader) Close() error {
	r.closeOnce.Do(func() {
		// Close the reader if it implements io.Closer
		if closer, ok := r.Reader.(io.Closer); ok {
			_ = closer.Close()
		}

		// Kill the process group if context was canceled or timed out
		if r.cmd != nil && r.cmd.Process != nil {
			// Capture PID before any state changes to prevent race condition
			pid := r.cmd.Process.Pid

			if r.ctx != nil && r.ctx.Err() != nil {
				// Kill the entire process group (negative PID)
				// Ignore errors - process may have already exited
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}

			// Wait for command to complete and capture exit code
			err := r.cmd.Wait()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					r.exitCode = exitErr.ExitCode()
				} else {
					r.exitCode = -1
				}
			}
		}

		// Clean up temp file if one was created (ref-file pattern)
		CleanupTempFile(r.tempFilePath)
	})

	return nil
}

// ExitCode implements ExitCoder and returns the process exit code.
// Only valid after Close() has been called. Returns 0 if process succeeded,
// -1 if process could not be waited on, or the actual exit code otherwise.
func (r *cmdReader) ExitCode() int {
	return r.exitCode
}

// Stderr implements StderrProvider and returns captured stderr output.
// Only valid after Close() has been called. Returns empty string if no
// stderr was captured or if stderr buffer was not configured.
func (r *cmdReader) Stderr() string {
	if r.stderr == nil {
		return ""
	}
	return r.stderr.String()
}

// ToExecutionResult wraps this cmdReader in an ExecutionResult.
// This provides a clean API for callers without requiring type assertions.
func (r *cmdReader) ToExecutionResult() *ExecutionResult {
	return NewExecutionResult(
		r,
		func() int { return r.ExitCode() },
		func() string { return r.Stderr() },
	)
}
