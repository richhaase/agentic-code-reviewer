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
var _ Agent = (*GeminiAgent)(nil)

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
// Returns an ExecutionResult for streaming the JSON output.
//
// Uses the pre-computed diff from config.Diff when available, otherwise fetches it.
// The diff is either appended to the prompt or written to a reference file for large diffs.
func (g *GeminiAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "gemini",
		Args:          []string{"-o", "json", "-"},
		DefaultPrompt: DefaultGeminiPrompt,
		RefFilePrompt: DefaultGeminiRefFilePrompt,
	})
}

// ExecuteSummary runs a summarization task using the gemini CLI.
// Uses 'gemini -o json -' with the prompt and input piped to stdin.
// Gemini CLI has file reading capabilities via its ReadFile tool, but for
// summary inputs we embed the JSON directly since they are typically small
// (aggregated findings rather than raw diffs).
func (g *GeminiAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	// Build command: gemini -o json -
	// -: Explicitly read prompt from stdin
	args := []string{"-o", "json", "-"}
	// Use MultiReader to avoid copying large input byte slice
	stdin := io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "gemini",
		Args:    args,
		Stdin:   stdin,
	})
}
