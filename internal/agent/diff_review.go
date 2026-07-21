package agent

import (
	"bytes"
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

type diffReviewConfig struct {
	Command string

	Args []string

	DefaultPrompt string

	RefFilePrompt string
}

func executeDiffBasedReview(ctx context.Context, config *ReviewConfig, dc diffReviewConfig) (*ExecutionResult, error) {

	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}
	}

	useRefFile := config.UseRefFile || len(diff) > RefFileSizeThreshold

	var prompt string
	var tempFilePath string

	if useRefFile && diff != "" {

		absPath, err := WriteDiffToTempFile(config.WorkDir, diff)
		if err != nil {
			return nil, err
		}
		tempFilePath = absPath
		prompt = fmt.Sprintf(dc.RefFilePrompt, absPath)
		prompt = RenderPrompt(prompt, config.Guidance)
	} else {

		prompt = RenderPrompt(dc.DefaultPrompt, config.Guidance)
		prompt = BuildPromptWithDiff(prompt, diff)
	}

	stdin := bytes.NewReader([]byte(prompt))

	return executeCommand(ctx, executeOptions{
		Command:      dc.Command,
		Args:         dc.Args,
		Stdin:        stdin,
		WorkDir:      config.WorkDir,
		TempFilePath: tempFilePath,
	})
}
