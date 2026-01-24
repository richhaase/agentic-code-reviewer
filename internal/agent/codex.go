package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// BuildCodexRefFilePrompt constructs the review prompt for ref-file mode.
// If customPrompt is provided, it appends ref-file instructions to it.
// Otherwise, it uses DefaultCodexRefFilePrompt.
func BuildCodexRefFilePrompt(customPrompt, diffPath string) string {
	if customPrompt != "" {
		return fmt.Sprintf("%s\n\nThe diff to review is in file: %s\nRead the file contents to examine the changes.", customPrompt, diffPath)
	}
	return fmt.Sprintf(DefaultCodexRefFilePrompt, diffPath)
}

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
// The git diff is either appended to the prompt (default) or written to a
// reference file when the diff is large or UseRefFile is set.
// Otherwise, uses 'codex exec review --base X' for the built-in review behavior.
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	var args []string
	var tempFilePath string

	if config.CustomPrompt != "" {
		// Custom prompt mode: pipe prompt + diff to 'codex exec -'
		diff, err := GetGitDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}

		// Determine if we should use ref-file mode
		useRefFile := config.UseRefFile || len(diff) > RefFileSizeThreshold

		var prompt string
		if useRefFile && diff != "" {
			// Write diff to a temp file in the working directory
			absPath, err := WriteDiffToTempFile(config.WorkDir, diff)
			if err != nil {
				return nil, err
			}
			tempFilePath = absPath
			prompt = BuildCodexRefFilePrompt(config.CustomPrompt, absPath)
		} else {
			prompt = BuildPromptWithDiff(config.CustomPrompt, diff)
		}

		args = []string{"exec", "--json", "--color", "never", "-"}
		cmd = exec.CommandContext(ctx, "codex", args...)
		cmd.Stdin = bytes.NewReader([]byte(prompt))
	} else {
		// Default mode: use built-in 'codex exec review'
		// This mode handles diffs internally
		if config.UseRefFile {
			fmt.Fprintln(os.Stderr, "Note: --ref-file flag has no effect in Codex default mode (requires custom prompt)")
		}
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
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(tempFilePath)
		return nil, fmt.Errorf("failed to start codex: %w", err)
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

// ExecuteSummary runs a summarization task using the codex CLI.
// Uses 'codex exec --color never -' with the prompt and input piped to stdin.
//
// Note: While Codex can read files within its working directory, this function
// embeds the input directly in the prompt for simplicity. Very large inputs
// (>100KB) may hit prompt length limits, but summary inputs are typically
// much smaller since they contain aggregated findings rather than raw diffs.
func (c *CodexAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error) {
	if err := c.IsAvailable(); err != nil {
		return nil, err
	}

	args := []string{"exec", "--json", "--color", "never", "-"}
	cmd := exec.CommandContext(ctx, "codex", args...)
	// Use MultiReader to avoid copying large input byte slice
	cmd.Stdin = io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

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
