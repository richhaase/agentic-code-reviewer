package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// ClaudeAgent implements the Agent interface for the Claude CLI backend.
type ClaudeAgent struct{}

// NewClaudeAgent creates a new ClaudeAgent instance.
func NewClaudeAgent() *ClaudeAgent {
	return &ClaudeAgent{}
}

// Name returns the agent's identifier.
func (c *ClaudeAgent) Name() string {
	return "claude"
}

// IsAvailable checks if the claude CLI is installed and accessible.
func (c *ClaudeAgent) IsAvailable() error {
	_, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found in PATH: %w", err)
	}
	return nil
}

// Execute runs a code review using the claude CLI.
// Returns an io.Reader for streaming the output.
//
// Uses 'claude --print "prompt"' for non-interactive execution.
// If config.CustomPrompt is empty, uses DefaultClaudePrompt.
// The git diff is automatically appended to the prompt.
func (c *ClaudeAgent) Execute(ctx context.Context, config *AgentConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Use custom prompt if provided, otherwise use default
	prompt := config.CustomPrompt
	if prompt == "" {
		prompt = DefaultClaudePrompt
	}

	// Get git diff and append to prompt
	diff, err := GetGitDiff(config.BaseRef, config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff for review: %w", err)
	}
	prompt = BuildPromptWithDiff(prompt, diff)

	// Build command: claude --print "prompt"
	// --print: Output response only (non-interactive)
	// prompt is passed as a positional argument
	args := []string{"--print", prompt}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe empty stdin to ensure non-interactive mode
	cmd.Stdin = bytes.NewReader([]byte{})

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
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Return a reader that will also wait for the command to complete
	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}, nil
}
