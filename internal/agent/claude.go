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
// Uses 'claude --print -' with the prompt piped via stdin.
// If config.CustomPrompt is empty, uses DefaultClaudePrompt.
// The git diff is automatically appended to the prompt.
// If config.DiffOverride is set, it is used instead of fetching from git.
func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Use custom prompt if provided, otherwise use default
	prompt := config.CustomPrompt
	if prompt == "" {
		prompt = DefaultClaudePrompt
	}

	// Use DiffOverride if provided, otherwise fetch from git
	var diff string
	var err error
	if config.DiffOverride != "" {
		diff = config.DiffOverride
	} else {
		diff, err = GetGitDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}
	}
	prompt = BuildPromptWithDiff(prompt, diff)

	// Build command: claude --print -
	// --print: Output response only (non-interactive)
	// -: Read prompt from stdin (avoids ARG_MAX limits on large diffs)
	args := []string{"--print", "-"}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe prompt via stdin
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

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
		stderr: stderr,
	}, nil
}

// ExecuteSummary runs a summarization task using the claude CLI.
// Uses 'claude --print --output-format json --json-schema <schema> -'
// with the prompt piped via stdin.
func (c *ClaudeAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Combine prompt and input
	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"

	// Build command with JSON schema for structured output
	// -: Read prompt from stdin (avoids ARG_MAX limits on large inputs)
	args := []string{"--print", "--output-format", "json", "--json-schema", claudeSummarySchema, "-"}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe prompt via stdin
	cmd.Stdin = bytes.NewReader([]byte(fullPrompt))

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

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
		stderr: stderr,
	}, nil
}
