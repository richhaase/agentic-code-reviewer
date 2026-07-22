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

var _ Agent = (*AntigravityAgent)(nil)

type AntigravityAgent struct{}

const antigravityDefaultPrintTimeout = 30 * time.Minute

const antigravityPrintTimeoutGrace = 5 * time.Second

func NewAntigravityAgent(_ string) *AntigravityAgent {
	return &AntigravityAgent{}
}

func (a *AntigravityAgent) Name() string {
	return "agy"
}

func (a *AntigravityAgent) IsAvailable() error {
	_, err := exec.LookPath("agy")
	if err != nil {
		return fmt.Errorf("agy CLI not found in PATH: %w", err)
	}
	return nil
}

func (a *AntigravityAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "agy",
		Args:          antigravityPrintArgs(antigravityPrintTimeoutCeiling(config.Timeout)),
		DefaultPrompt: DefaultAntigravityPrompt,
		RefFilePrompt: DefaultAntigravityRefFilePrompt,
	})
}

func (a *AntigravityAgent) ExecuteSummary(ctx context.Context, config *SummaryConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	stdin := io.MultiReader(
		strings.NewReader(config.Prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(config.Input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "agy",
		Args:    antigravityPrintArgs(antigravityPrintTimeoutCeilingFromContext(ctx, time.Now())),
		Stdin:   stdin,
		WorkDir: config.WorkDir,
	})
}

func antigravityPrintArgs(timeout time.Duration) []string {
	if timeout <= 0 {
		timeout = antigravityDefaultPrintTimeout
	}

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

func antigravityPrintTimeoutCeiling(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 0
	}
	return timeout + antigravityPrintTimeoutGrace
}

func antigravityPrintTimeoutCeilingFromContext(ctx context.Context, now time.Time) time.Duration {
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
