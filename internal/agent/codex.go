package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Compile-time interface check
var _ Agent = (*CodexAgent)(nil)

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

// ExecuteReview runs a code review using the codex CLI.
// Returns an ExecutionResult for streaming the JSONL output.
//
// Always uses 'codex exec review --base X' for the built-in review behavior.
// When guidance is provided, it is piped via stdin using the '-' flag.
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var stdin io.Reader
	args := []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}

	if config.Guidance != "" {
		// Pipe guidance via stdin to codex exec review
		args = append(args, "-")
		stdin = bytes.NewReader([]byte(config.Guidance))
	}

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
		Stdin:   stdin,
		WorkDir: config.WorkDir,
	})
}

// ExecuteSummary runs a summarization task using the codex CLI.
// Uses 'codex exec --color never -' with the prompt and input piped to stdin.
//
// Note: While Codex can read files within its working directory, this function
// embeds the input directly in the prompt for simplicity. Very large inputs
// (>100KB) may hit prompt length limits, but summary inputs are typically
// much smaller since they contain aggregated findings rather than raw diffs.
func (c *CodexAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"exec", "--json", "--color", "never", "-"}
	// Use MultiReader to avoid copying large input byte slice
	stdin := io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
		Stdin:   stdin,
	})
}
