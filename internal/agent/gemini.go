package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Compile-time interface check
var _ Agent = (*GeminiAgent)(nil)

// BuildGeminiRefFilePrompt constructs the review prompt for ref-file mode.
// If customPrompt is provided, it appends ref-file instructions to it.
// Otherwise, it uses DefaultGeminiRefFilePrompt.
func BuildGeminiRefFilePrompt(customPrompt, diffPath string) string {
	if customPrompt != "" {
		return fmt.Sprintf("%s\n\nThe diff to review is in file: %s\nRead the file contents to examine the changes.", customPrompt, diffPath)
	}
	return fmt.Sprintf(DefaultGeminiRefFilePrompt, diffPath)
}

// GeminiAgent implements the Agent interface for the Gemini CLI backend.
type GeminiAgent struct{}

// NewGeminiAgent creates a new GeminiAgent instance.
func NewGeminiAgent() *GeminiAgent {
	return &GeminiAgent{}
}

// Name returns the agent's identifier.
func (g *GeminiAgent) Name() string {
	return "gemini"
}

// IsAvailable checks if the gemini CLI is installed and accessible.
func (g *GeminiAgent) IsAvailable() error {
	_, err := exec.LookPath("gemini")
	if err != nil {
		return fmt.Errorf("gemini CLI not found in PATH: %w", err)
	}
	return nil
}

// ExecuteReview runs a code review using the gemini CLI.
// Returns an ExecutionResult for streaming the JSON output.
//
// Uses 'gemini -o json -' with the prompt piped to stdin.
// If config.CustomPrompt is empty, uses DefaultGeminiPrompt.
// The git diff is either appended to the prompt (default) or written to a
// reference file when the diff is large or UseRefFile is set.
func (g *GeminiAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
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
		prompt = BuildGeminiRefFilePrompt(config.CustomPrompt, absPath)
	} else {
		// Use standard prompt with embedded diff
		prompt = config.CustomPrompt
		if prompt == "" {
			prompt = DefaultGeminiPrompt
		}
		prompt = BuildPromptWithDiff(prompt, diff)
	}

	// Build command: gemini -o json -
	args := []string{"-o", "json", "-"}
	stdin := bytes.NewReader([]byte(prompt))

	return executeCommand(ctx, executeOptions{
		Command:      "gemini",
		Args:         args,
		Stdin:        stdin,
		WorkDir:      config.WorkDir,
		TempFilePath: tempFilePath,
	})
}

// ExecuteSummary runs a summarization task using the gemini CLI.
// Uses 'gemini -o json -' with the prompt and input piped to stdin.
// Gemini CLI has file reading capabilities via its ReadFile tool, but for
// summary inputs we embed the JSON directly since they are typically small
// (aggregated findings rather than raw diffs).
func (g *GeminiAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := g.IsAvailable(); err != nil {
		return nil, err
	}

	// Build command: gemini -o json -
	// -: Explicitly read prompt from stdin
	args := []string{"-o", "json", "-"}
	// Use MultiReader to avoid copying large input byte slice
	stdin := io.MultiReader(
		strings.NewReader(prompt),
		strings.NewReader("\n\nINPUT JSON:\n"),
		bytes.NewReader(input),
		strings.NewReader("\n"),
	)

	return executeCommand(ctx, executeOptions{
		Command: "gemini",
		Args:    args,
		Stdin:   stdin,
	})
}
