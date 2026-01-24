package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// GeminiAgent implements the Agent interface for the Gemini CLI backend.
type GeminiAgent struct{}

// NewGeminiAgent creates a new GeminiAgent instance.
func NewGeminiAgent() *GeminiAgent {
	return &GeminiAgent{}
}

// Name returns the agent's identifier.
func (g *GeminiAgent) Name() string {
	return "gemini"
}

// IsAvailable checks if the gemini CLI is installed and accessible.
func (g *GeminiAgent) IsAvailable() error {
	_, err := exec.LookPath("gemini")
	if err != nil {
		return fmt.Errorf("gemini CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the gemini CLI.
// Returns an io.Reader for streaming the JSON output.
//
// Uses 'gemini -o json -' with the prompt piped to stdin.
// If config.CustomPrompt is empty, uses DefaultGeminiPrompt.
// The git diff is automatically appended to the prompt.
func (g *GeminiAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	// Use custom prompt if provided, otherwise use default
	prompt := config.CustomPrompt
	if prompt == "" {
		prompt = DefaultGeminiPrompt
	}

	// Get git diff and append to prompt
	diff, err := GetGitDiff(ctx, config.BaseRef, config.WorkDir, config.FetchRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff for review: %w", err)
	}
	prompt = BuildPromptWithDiff(prompt, diff)

	// Build command: gemini -o json -
	args := []string{"-o", "json", "-"}
	cmd := exec.CommandContext(ctx, "gemini", args...)
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
		return nil, fmt.Errorf("failed to start gemini: %w", err)
	}

	// Return a reader that will also wait for the command to complete
	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
		stderr: stderr,
	}, nil
}

// ExecuteSummary runs a summarization task using the gemini CLI.
// Uses 'gemini -o json -' with the prompt and input piped to stdin.
func (g *GeminiAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	// Combine prompt and input
	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"

	// Build command: gemini -o json -
	// -: Explicitly read prompt from stdin
	args := []string{"-o", "json", "-"}
	cmd := exec.CommandContext(ctx, "gemini", args...)
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
		return nil, fmt.Errorf("failed to start gemini: %w", err)
	}

	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
		stderr: stderr,
	}, nil
}
