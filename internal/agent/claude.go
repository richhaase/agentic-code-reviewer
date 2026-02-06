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

// claudeSummarySchema is the JSON schema for Claude's structured summary output.
// This ensures Claude returns properly formatted JSON without markdown wrapping.
const claudeSummarySchema = `{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}},"info":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}}},"required":["findings","info"]}`

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
// Always uses the default Claude prompt template with optional guidance.
// The git diff is either appended to the prompt (default) or written to a
// reference file when the diff is large or UseRefFile is set.
func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Get git diff
	diff, err := GetGitDiff(ctx, config.BaseRef, config.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff for review: %w", err)
	}

	// Determine if we should use ref-file mode
	useRefFile := config.UseRefFile || len(diff) > RefFileSizeThreshold

	var prompt string
	var tempFilePath string

	if useRefFile && diff != "" {
		// Write diff to a temp file in the working directory
		absPath, err := WriteDiffToTempFile(config.WorkDir, diff)
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		prompt = fmt.Sprintf(DefaultClaudeRefFilePrompt, absPath)
		prompt = RenderPrompt(prompt, config.Guidance)
	} else {
		// Use standard prompt with embedded diff
		prompt = RenderPrompt(DefaultClaudePrompt, config.Guidance)
		prompt = BuildPromptWithDiff(prompt, diff)
	}

	args := []string{"--print", "-"}
	stdin := bytes.NewReader([]byte(prompt))

	return executeCommand(ctx, executeOptions{
		Command:      "claude",
		Args:         args,
		Stdin:        stdin,
		WorkDir:      config.WorkDir,
		TempFilePath: tempFilePath,
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

	// Build command with JSON schema for structured output
	// -: Read prompt from stdin (avoids ARG_MAX limits on large inputs)
	args := []string{"--print", "--output-format", "json", "--json-schema", claudeSummarySchema, "-"}

	return executeCommand(ctx, executeOptions{
		Command:      "claude",
		Args:         args,
		Stdin:        stdin,
		TempFilePath: tempFilePath,
	})
}
