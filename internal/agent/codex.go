package agent

import (
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
func (c *CodexAgent) Execute(ctx context.Context, config *AgentConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Build command arguments
	args := []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}

	cmd := exec.CommandContext(ctx, "codex", args...) //nolint:gosec // BaseRef is validated CLI input
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
type cmdReader struct {
	io.Reader
	cmd *exec.Cmd
}

// Close implements io.Closer and waits for the command to complete.
func (r *cmdReader) Close() error {
	// Close the reader if it implements io.Closer
	if closer, ok := r.Reader.(io.Closer); ok {
		_ = closer.Close()
	}

	// Wait for command to complete
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Wait()
	}

	return nil
}
