package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

var _ Agent = (*ClaudeAgent)(nil)

type ClaudeAgent struct {
	model string
}

func NewClaudeAgent(model string) *ClaudeAgent {
	return &ClaudeAgent{model: model}
}

func (c *ClaudeAgent) Name() string {
	return "claude"
}

func (c *ClaudeAgent) IsAvailable() error {
	_, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found in PATH: %w", err)
	}
	return nil
}

func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"--print", "-"}
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "claude",
		Args:          args,
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	})
}

func (c *ClaudeAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var stdin io.Reader
	var tempFilePath string

	if len(input) > RefFileSizeThreshold {

		absPath, err := WriteInputToTempFile("", input, "summary-input.json")
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		fullPrompt := fmt.Sprintf("%s\n\nThe input JSON is in file: %s\nUse the Read tool to examine it.", prompt, absPath)
		stdin = bytes.NewReader([]byte(fullPrompt))
	} else {

		fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
		stdin = bytes.NewReader([]byte(fullPrompt))
	}

	args := []string{"--print", "--output-format", "json", "-"}
	if c.model != "" {
		args = append([]string{"--model", c.model}, args...)
	}

	return executeCommand(ctx, executeOptions{
		Command:      "claude",
		Args:         args,
		Stdin:        stdin,
		TempFilePath: tempFilePath,
	})
}
