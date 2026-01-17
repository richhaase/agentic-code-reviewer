package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// CodexAgent implements the Agent interface for the Codex CLI backend.
type CodexAgent struct{}

// NewCodexAgent creates a new CodexAgent instance.
func NewCodexAgent() *CodexAgent {
	return &CodexAgent{}
}

// Name returns the agent's identifier.
func (c *CodexAgent) Name() string {
	return "codex"
}

// IsAvailable checks if the codex CLI is installed and accessible.
func (c *CodexAgent) IsAvailable() error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH: %w", err)
	}
	return nil
}

// Execute runs a code review using the codex CLI.
// Returns an io.Reader for streaming the JSONL output.
//
// If config.CustomPrompt is provided, uses 'codex exec -' with the prompt on stdin.
// Otherwise, uses 'codex exec review --base X' for the built-in review behavior.
func (c *CodexAgent) Execute(ctx context.Context, config *AgentConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	var args []string

	if config.CustomPrompt != "" {
		// Custom prompt mode: pipe prompt to 'codex exec -'
		args = []string{"exec", "--json", "--color", "never", "-"}
		cmd = exec.CommandContext(ctx, "codex", args...)
		cmd.Stdin = bytes.NewReader([]byte(config.CustomPrompt))
	} else {
		// Default mode: use built-in 'codex exec review'
		args = []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}
		cmd = exec.CommandContext(ctx, "codex", args...)
	}

	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start codex: %w", err)
	}

	// Return a reader that will also wait for the command to complete
	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}, nil
}

// cmdReader wraps an io.Reader and ensures the command is waited on when closed.
// It implements io.Closer and ExitCoder to provide process exit code after Close().
type cmdReader struct {
	io.Reader
	cmd      *exec.Cmd
	exitCode int
	closed   bool
}

// Close implements io.Closer and waits for the command to complete.
// After Close returns, ExitCode() will return the process exit code.
func (r *cmdReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	// Close the reader if it implements io.Closer
	if closer, ok := r.Reader.(io.Closer); ok {
		_ = closer.Close()
	}

	// Wait for command to complete and capture exit code
	if r.cmd != nil && r.cmd.Process != nil {
		err := r.cmd.Wait()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				r.exitCode = exitErr.ExitCode()
			} else {
				r.exitCode = -1
			}
		}
	}

	return nil
}

// ExitCode implements ExitCoder and returns the process exit code.
// Only valid after Close() has been called. Returns 0 if process succeeded,
// -1 if process could not be waited on, or the actual exit code otherwise.
func (r *cmdReader) ExitCode() int {
	return r.exitCode
}
