package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// Compile-time interface check
var _ Agent = (*ClaudeAgent)(nil)

// Note: We intentionally do NOT use --json-schema with Claude's ExecuteSummary.
// While --json-schema forces structured output for one schema, ExecuteSummary is
// called by multiple subsystems (summarizer, FP filter, feedback) that each expect
// different JSON formats. The prompts already specify "Return ONLY valid JSON"
// which Claude follows reliably with --output-format json.

// ClaudeAgent implements the Agent interface for the Claude CLI backend.
type ClaudeAgent struct{}

// NewClaudeAgent creates a new ClaudeAgent instance.
func NewClaudeAgent() *ClaudeAgent {
	return &ClaudeAgent{}
}

// Name returns the agent's identifier.
func (c *ClaudeAgent) Name() string {
	return "claude"
}

// IsAvailable checks if the claude CLI is installed and accessible.
func (c *ClaudeAgent) IsAvailable() error {
	_, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the claude CLI.
// Returns an ExecutionResult for streaming the output.
//
// Uses the pre-computed diff from config.Diff when available, otherwise fetches it.
// The diff is either appended to the prompt or written to a reference file for large diffs.
func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	return executeDiffBasedReview(ctx, config, diffReviewConfig{
		Command:       "claude",
		Args:          []string{"--print", "-"},
		DefaultPrompt: DefaultClaudePrompt,
		RefFilePrompt: DefaultClaudeRefFilePrompt,
	})
}

// ExecuteSummary runs a summarization task using the claude CLI.
// Uses 'claude --print --output-format json --json-schema <schema> -'
// with the prompt piped via stdin.
// For large inputs (>100KB), writes input to a temp file and instructs Claude
// to read it using the Read tool to avoid prompt length errors.
func (c *ClaudeAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var stdin io.Reader
	var tempFilePath string

	// Check if input is large enough to warrant ref-file mode
	// Claude has file-reading capability via its Read tool
	if len(input) > RefFileSizeThreshold {
		// Write input to a temp file
		absPath, err := WriteInputToTempFile("", input, "summary-input.json")
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		fullPrompt := fmt.Sprintf("%s\n\nThe input JSON is in file: %s\nUse the Read tool to examine it.", prompt, absPath)
		stdin = bytes.NewReader([]byte(fullPrompt))
	} else {
		// Standard mode: embed input in prompt
		fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
		stdin = bytes.NewReader([]byte(fullPrompt))
	}

	// Build command with JSON output format (no --json-schema — see note above)
	// -: Read prompt from stdin (avoids ARG_MAX limits on large inputs)
	args := []string{"--print", "--output-format", "json", "-"}

	return executeCommand(ctx, executeOptions{
		Command:      "claude",
		Args:         args,
		Stdin:        stdin,
		TempFilePath: tempFilePath,
	})
}
