package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

var _ Agent = (*GeminiAgent)(nil)

type GeminiAgent struct {
	model string
}

func NewGeminiAgent(model string) *GeminiAgent {
	return &GeminiAgent{model: model}
}

func (g *GeminiAgent) Name() string {
	return "gemini"
}

func (g *GeminiAgent) IsAvailable() error {
	_, err := exec.LookPath("gemini")
	if err != nil {
		return fmt.Errorf("gemini CLI not found in PATH: %w", err)
	}
	return nil
}

func (g *GeminiAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"-o", "json", "-"}
	if g.model != "" {
		args = append([]string{"--model", g.model}, args...)
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "gemini",
		Args:          args,
		DefaultPrompt: DefaultGeminiPrompt,
		RefFilePrompt: DefaultGeminiRefFilePrompt,
	})
}

func (g *GeminiAgent) ExecuteSummary(ctx context.Context, config *SummaryConfig) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"-o", "json", "-"}
	if g.model != "" {
		args = append([]string{"--model", g.model}, args...)
	}

	stdin := io.MultiReader(
		strings.NewReader(config.Prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(config.Input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "gemini",
		Args:    args,
		Stdin:   stdin,
		WorkDir: config.WorkDir,
	})
}
