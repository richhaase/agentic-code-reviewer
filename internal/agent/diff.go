package agent

import (
	"context"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

type UpdateBranchResult = git.UpdateBranchResult

type FetchResult = git.FetchResult

func UpdateCurrentBranch(ctx context.Context, workDir string) UpdateBranchResult {
	return git.UpdateCurrentBranch(ctx, workDir)
}

func IsRelativeRef(ref string) bool {
	return git.IsRelativeRef(ref)
}

func FetchRemoteRef(ctx context.Context, baseRef, workDir string) FetchResult {
	return git.FetchRemoteRef(ctx, baseRef, workDir)
}

func GetGitDiff(ctx context.Context, baseRef, workDir string) (string, error) {
	return git.GetDiff(ctx, baseRef, workDir)
}

func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}
	return prompt + "\n\n```diff\n" + diff + "\n```"
}
