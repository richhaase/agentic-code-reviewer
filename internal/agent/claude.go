package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// claudeSummarySchema is the JSON schema for Claude's structured summary output.
// This ensures Claude returns properly formatted JSON without markdown wrapping.
const claudeSummarySchema = `{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}},"info":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}}},"required":["findings","info"]}`

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

// ExecuteReview runs a code review using the claude CLI.
// Returns an io.Reader for streaming the output.
//
// Uses 'claude --print "prompt"' for non-interactive execution.
// If config.CustomPrompt is empty, uses DefaultClaudePrompt.
// The git diff is automatically appended to the prompt.
func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
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

// ExecuteSummary runs a summarization task using the claude CLI.
// Uses 'claude --print --output-format json --json-schema <schema> <prompt>'
// with the prompt containing both instructions and input data.
func (c *ClaudeAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Combine prompt and input
	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"

	// Build command with JSON schema for structured output
	args := []string{"--print", "--output-format", "json", "--json-schema", claudeSummarySchema, fullPrompt}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe empty stdin to ensure non-interactive mode
	cmd.Stdin = bytes.NewReader([]byte{})

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}, nil
}
