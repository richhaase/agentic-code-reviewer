package agent

import (
	"context"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// These functions delegate to the git package where they now live.
// Kept here for backward compatibility with callers that haven't been updated yet.

// UpdateBranchResult is an alias for git.UpdateBranchResult.
type UpdateBranchResult = git.UpdateBranchResult

// FetchResult is an alias for git.FetchResult.
type FetchResult = git.FetchResult

// UpdateCurrentBranch delegates to git.UpdateCurrentBranch.
func UpdateCurrentBranch(ctx context.Context, workDir string) UpdateBranchResult {
	return git.UpdateCurrentBranch(ctx, workDir)
}

// IsRelativeRef delegates to git.IsRelativeRef.
func IsRelativeRef(ref string) bool {
	return git.IsRelativeRef(ref)
}

// FetchRemoteRef delegates to git.FetchRemoteRef.
func FetchRemoteRef(ctx context.Context, baseRef, workDir string) FetchResult {
	return git.FetchRemoteRef(ctx, baseRef, workDir)
}

// GetGitDiff delegates to git.GetDiff.
func GetGitDiff(ctx context.Context, baseRef, workDir string) (string, error) {
	return git.GetDiff(ctx, baseRef, workDir)
}

// BuildPromptWithDiff combines a review prompt with the git diff.
func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}
	return prompt + "\n\n```diff\n" + diff + "\n```"
}
