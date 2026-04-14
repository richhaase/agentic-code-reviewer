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
type CodexAgent struct {
	model string
}

// NewCodexAgent creates a new CodexAgent instance.
// If model is non-empty, it overrides the default model via --model.
func NewCodexAgent(model string) *CodexAgent {
	return &CodexAgent{model: model}
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
// Without guidance, uses 'codex exec review --base X' for the built-in review behavior.
// With guidance, falls back to the diff-based review path because codex's --base flag
// and stdin prompt (-) are mutually exclusive (see #170).
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Per-reviewer model override
	model := c.model
	if config.Model != "" {
		model = config.Model
	}

	// Use diff-based review path when guidance or phase is set.
	// Codex's built-in "review --base" path ignores ReviewConfig.Phase/TargetFiles,
	// so we must route through the diff-based path for those features.
	// Note: DiffPrecomputed alone does NOT trigger this path — in mixed-agent runs
	// (codex+claude), DiffPrecomputed is globally true for Claude's benefit,
	// but Codex should still use its built-in review when no guidance/phase is set.
	if config.Guidance != "" || config.Phase != "" {
		args := []string{"exec", "--json", "--color", "never", "-"}
		if model != "" {
			args = append([]string{"--model", model}, args...)
		}
		return executeDiffBasedReview(ctx, config, diffReviewConfig{
			Command:       "codex",
			Args:          args,
			DefaultPrompt: DefaultCodexPrompt,
			RefFilePrompt: DefaultCodexRefFilePrompt,
		})
	}

	args := []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}
	if model != "" {
		args = append([]string{"--model", model}, args...)
	}

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
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
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}
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
