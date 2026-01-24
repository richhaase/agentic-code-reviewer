package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// claudeSummarySchema is the JSON schema for Claude's structured summary output.
// This ensures Claude returns properly formatted JSON without markdown wrapping.
const claudeSummarySchema = `{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}},"info":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"summary":{"type":"string"},"messages":{"type":"array","items":{"type":"string"}},"reviewer_count":{"type":"integer"},"sources":{"type":"array","items":{"type":"integer"}}},"required":["title","summary","messages","reviewer_count","sources"]}}},"required":["findings","info"]}`

// BuildRefFilePrompt constructs the review prompt for ref-file mode.
// If customPrompt is provided, it appends ref-file instructions to it.
// Otherwise, it uses DefaultClaudeRefFilePrompt.
func BuildRefFilePrompt(customPrompt, diffPath string) string {
	if customPrompt != "" {
		return fmt.Sprintf("%s\n\nThe diff to review is in file: %s\nUse the Read tool to examine it.", customPrompt, diffPath)
	}
	return fmt.Sprintf(DefaultClaudeRefFilePrompt, diffPath)
}

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
// Returns an io.Reader for streaming the output.
//
// Uses 'claude --print -' with the prompt piped via stdin.
// If config.CustomPrompt is empty, uses DefaultClaudePrompt.
// The git diff is either appended to the prompt (default) or written to a
// reference file when the diff is large or UseRefFile is set.
func (c *ClaudeAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Get git diff
	diff, err := GetGitDiff(ctx, config.BaseRef, config.WorkDir, config.FetchRemote)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff for review: %w", err)
	}

	// Determine if we should use ref-file mode
	// Claude has file-reading capability via its Read tool, so ref-file mode is supported
	useRefFile := config.UseRefFile || len(diff) > RefFileSizeThreshold

	var prompt string
	var tempFilePath string

	if useRefFile && diff != "" {
		// Write diff to a temp file in the working directory
		// (Claude's sandboxed Read tool needs access to the file)
		absPath, err := WriteDiffToTempFile(config.WorkDir, diff)
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		prompt = BuildRefFilePrompt(config.CustomPrompt, absPath)
	} else {
		// Use standard prompt with embedded diff
		prompt = config.CustomPrompt
		if prompt == "" {
			prompt = DefaultClaudePrompt
		}
		prompt = BuildPromptWithDiff(prompt, diff)
	}

	// Build command: claude --print -
	// --print: Output response only (non-interactive)
	// -: Read prompt from stdin (avoids ARG_MAX limits on large diffs)
	args := []string{"--print", "-"}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe prompt via stdin
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Return a reader that will also wait for the command to complete
	// and clean up the temp file when done
	return &cmdReader{
		Reader:       stdout,
		cmd:          cmd,
		ctx:          ctx,
		stderr:       stderr,
		tempFilePath: tempFilePath,
	}, nil
}

// ExecuteSummary runs a summarization task using the claude CLI.
// Uses 'claude --print --output-format json --json-schema <schema> -'
// with the prompt piped via stdin.
// For large inputs (>100KB), writes input to a temp file and instructs Claude
// to read it using the Read tool to avoid prompt length errors.
func (c *ClaudeAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var fullPrompt string
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
		fullPrompt = fmt.Sprintf("%s\n\nThe input JSON is in file: %s\nUse the Read tool to examine it.", prompt, absPath)
	} else {
		// Standard mode: embed input in prompt
		fullPrompt = prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	}

	// Build command with JSON schema for structured output
	// -: Read prompt from stdin (avoids ARG_MAX limits on large inputs)
	args := []string{"--print", "--output-format", "json", "--json-schema", claudeSummarySchema, "-"}
	cmd := exec.CommandContext(ctx, "claude", args...)

	// Pipe prompt via stdin
	cmd.Stdin = bytes.NewReader([]byte(fullPrompt))

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	return &cmdReader{
		Reader:       stdout,
		cmd:          cmd,
		ctx:          ctx,
		stderr:       stderr,
		tempFilePath: tempFilePath,
	}, nil
}
