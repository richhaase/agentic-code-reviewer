package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Compile-time interface check
var _ Agent = (*AntigravityAgent)(nil)

// AntigravityAgent implements the Agent interface for the Antigravity CLI backend.
type AntigravityAgent struct{}

const antigravityDefaultPrintTimeout = 30 * time.Minute

// NewAntigravityAgent creates a new AntigravityAgent instance.
//
// Antigravity CLI model selection is currently managed by agy configuration
// rather than a non-interactive command-line flag, so model is accepted for
// factory compatibility and intentionally ignored.
func NewAntigravityAgent(_ string) *AntigravityAgent {
	return &AntigravityAgent{}
}

// Name returns the agent's identifier.
func (a *AntigravityAgent) Name() string {
	return "agy"
}

// IsAvailable checks if the agy CLI is installed and accessible.
func (a *AntigravityAgent) IsAvailable() error {
	_, err := exec.LookPath("agy")
	if err != nil {
		return fmt.Errorf("agy CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the agy CLI.
// Returns an ExecutionResult for streaming the output.
//
// Uses the pre-computed diff from config.Diff when available, otherwise fetches it.
// The diff is either appended to the prompt or written to a reference file for large diffs.
func (a *AntigravityAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "agy",
		Args:          antigravityPrintArgs(config.Timeout),
		DefaultPrompt: DefaultAntigravityPrompt,
		RefFilePrompt: DefaultAntigravityRefFilePrompt,
	})
}

// ExecuteSummary runs a summarization task using the agy CLI.
// Uses 'agy --print -' with the prompt and input piped via stdin.
func (a *AntigravityAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	stdin := io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "agy",
		Args:    antigravityPrintArgs(0),
		Stdin:   stdin,
	})
}

func antigravityPrintArgs(timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = antigravityDefaultPrintTimeout
	}
	// agy --print requires an argument. "-" tells agy to read the prompt
	// from stdin, which avoids shell argument length limits for large diffs.
	return []string{"--print", "-", "--print-timeout", timeout.String()}
}
