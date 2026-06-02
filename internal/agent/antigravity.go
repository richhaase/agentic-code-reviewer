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
const antigravityPrintTimeoutGrace = 5 * time.Second

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
		Args:          antigravityPrintArgs(antigravityCommandTimeout(config.Timeout)),
		DefaultPrompt: DefaultAntigravityPrompt,
		RefFilePrompt: DefaultAntigravityRefFilePrompt,
	})
}

// ExecuteSummary runs a summarization task using the agy CLI.
// Uses 'agy --print=-' with the prompt and input piped via stdin.
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
		Args:    antigravityPrintArgs(antigravityCommandTimeoutFromContext(ctx, time.Now())),
		Stdin:   stdin,
	})
}

func antigravityPrintArgs(timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = antigravityDefaultPrintTimeout
	}
	// agy --print requires an argument. Verified with agy 1.0.2:
	// --print=- reads the prompt from stdin, which avoids shell argument
	// length limits for large diffs and summary payloads.
	return []string{"--print=-", "--print-timeout", formatAntigravityTimeout(timeout)}
}

func formatAntigravityTimeout(timeout time.Duration) string {
	seconds := int64(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func antigravityCommandTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 0
	}
	return timeout + antigravityPrintTimeoutGrace
}

func antigravityCommandTimeoutFromContext(ctx context.Context, now time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return time.Second
	}
	return remaining + antigravityPrintTimeoutGrace
}
