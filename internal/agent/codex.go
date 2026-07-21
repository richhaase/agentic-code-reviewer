package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

var _ Agent = (*CodexAgent)(nil)

type CodexAgent struct {
	model string
}

func NewCodexAgent(model string) *CodexAgent {
	return &CodexAgent{model: model}
}

func (c *CodexAgent) Name() string {
	return "codex"
}

func (c *CodexAgent) IsAvailable() error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH: %w", err)
	}
	return nil
}

func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	if config.Guidance != "" || config.DiffPrecomputed {
		args := []string{"exec", "--json", "--color", "never", "-"}
		if c.model != "" {
			args = append([]string{"--model", c.model}, args...)
		}
		return executeDiffBasedReview(ctx, config, diffReviewConfig{
			Command:       "codex",
			Args:          args,
			DefaultPrompt: DefaultCodexPrompt,
			RefFilePrompt: DefaultCodexRefFilePrompt,
		})
	}

	args := []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}

	return executeCommand(ctx, executeOptions{
		Command: "codex",
		Args:    args,
		WorkDir: config.WorkDir,
	})
}

func (c *CodexAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"exec", "--json", "--color", "never", "-"}
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}

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
