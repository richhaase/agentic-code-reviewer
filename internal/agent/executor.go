package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// executeOptions configures command execution for agent CLI invocations.
type executeOptions struct {
	// Command is the CLI executable name (e.g., "codex", "claude", "gemini").
	Command string
	// Args are the command-line arguments.
	Args []string
	// Stdin provides input to the command (typically the prompt).
	Stdin io.Reader
	// WorkDir sets the working directory for the command.
	WorkDir string
	// TempFilePath is a temp file to clean up on Close (used by ref-file pattern).
	TempFilePath string
}

// executeCommand runs a CLI command with proper process group setup and resource management.
// This is the shared implementation used by all agent ExecuteReview/ExecuteSummary methods.
//
// It handles:
//   - Setting process group for proper signal handling (Setpgid)
//   - Capturing stderr for error diagnostics
//   - Creating stdout pipe for streaming output
//   - Starting the command and returning a managed ExecutionResult
//   - Cleaning up temp files on error or when the result is closed
func executeCommand(ctx context.Context, opts executeOptions) (*ExecutionResult, error) {
	// #nosec G204 - Command is always one of the known agent CLIs (codex, claude, gemini)
	// passed from trusted code in the agent implementations, not user input.
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to start %s: %w", opts.Command, err)
	}

	reader := &cmdReader{
		Reader:       stdout,
		cmd:          cmd,
		ctx:          ctx,
		stderr:       stderr,
		tempFilePath: opts.TempFilePath,
	}

	return reader.ToExecutionResult(), nil
}
