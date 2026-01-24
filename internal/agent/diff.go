package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GetGitDiff returns the git diff against the specified base reference.
// If workDir is empty, uses the current directory.
// If fetchRemote is true, fetches the latest base ref from origin before diffing
// and compares against origin/<baseRef>. Falls back to local ref if fetch fails.
// The context is used to support cancellation/timeout.
func GetGitDiff(ctx context.Context, baseRef, workDir string, fetchRemote bool) (string, error) {
	// Validate baseRef
	if baseRef == "" {
		return "", fmt.Errorf("base ref cannot be empty")
	}
	// Prevent flag injection (refs starting with - would be interpreted as git flags).
	// The -- must come AFTER baseRef so git treats baseRef as a revision, not a pathspec.
	if strings.HasPrefix(baseRef, "-") {
		return "", fmt.Errorf("invalid base ref %q: must not start with -", baseRef)
	}

	// Determine the ref to diff against
	diffRef := baseRef

	if fetchRemote && !strings.HasPrefix(baseRef, "origin/") {
		// Try to fetch the latest base ref from origin
		fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", baseRef)
		if workDir != "" {
			fetchCmd.Dir = workDir
		}

		if err := fetchCmd.Run(); err == nil {
			// Fetch succeeded, use the remote ref
			diffRef = "origin/" + baseRef
		}
		// If fetch fails (offline, ref doesn't exist on remote, etc.), fall back to local ref
	}

	args := []string{"diff", diffRef, "--"}
	cmd := exec.CommandContext(ctx, "git", args...)

	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git diff: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// BuildPromptWithDiff combines a review prompt with the git diff.
func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}
	return prompt + "\n\n```diff\n" + diff + "\n```"
}
