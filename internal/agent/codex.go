package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// CodexAgent implements the Agent interface for the Codex CLI backend.
type CodexAgent struct{}

// NewCodexAgent creates a new CodexAgent instance.
func NewCodexAgent() *CodexAgent {
	return &CodexAgent{}
}

// Name returns the agent's identifier.
func (c *CodexAgent) Name() string {
	return "codex"
}

// IsAvailable checks if the codex CLI is installed and accessible.
func (c *CodexAgent) IsAvailable() error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the codex CLI.
// Returns an io.Reader for streaming the JSONL output.
//
// If config.CustomPrompt is provided, uses 'codex exec -' with the prompt on stdin.
// Otherwise, uses 'codex exec review --base X' for the built-in review behavior.
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	var args []string

	if config.CustomPrompt != "" {
		// Custom prompt mode: pipe prompt + diff to 'codex exec -'
		diff, err := GetGitDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}
		prompt := BuildPromptWithDiff(config.CustomPrompt, diff)

		args = []string{"exec", "--json", "--color", "never", "-"}
		cmd = exec.CommandContext(ctx, "codex", args...)
		cmd.Stdin = bytes.NewReader([]byte(prompt))
	} else {
		// Default mode: use built-in 'codex exec review'
		// This mode handles diffs internally, so UseRefFile/RefFileSizeThreshold
		// don't apply here - they're only relevant for custom prompt mode.
		args = []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}
		cmd = exec.CommandContext(ctx, "codex", args...)
	}

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
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start codex: %w", err)
	}

	// Return a reader that will also wait for the command to complete
	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
		stderr: stderr,
	}, nil
}

// ExecuteSummary runs a summarization task using the codex CLI.
// Uses 'codex exec --color never -' with the prompt and input piped to stdin.
//
// Note: For summarization, we embed input directly in the prompt rather than
// using ref-file mode, since summary inputs are typically smaller than raw diffs.
// Very large inputs (>100KB) may hit prompt length limits.
func (c *CodexAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	// Combine prompt and input
	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"

	args := []string{"exec", "--color", "never", "-"}
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Stdin = bytes.NewReader([]byte(fullPrompt))

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture stderr for error diagnostics
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start codex: %w", err)
	}

	return &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
		stderr: stderr,
	}, nil
}
