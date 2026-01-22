package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// MaxDiffSize is the maximum size of a diff in bytes before truncation.
// Claude's context window is ~200K tokens (~800KB), but we leave room for
// the prompt template and response. Default: 400KB.
const MaxDiffSize = 400 * 1024

// TruncationNotice is appended when a diff is truncated.
const TruncationNotice = "\n\n[DIFF TRUNCATED: Review was truncated to fit within context limits. Focus on the changes shown above.]"

// GetGitDiff returns the git diff against the specified base reference.
// If workDir is empty, uses the current directory.
// The context is used to support cancellation/timeout.
func GetGitDiff(ctx context.Context, baseRef, workDir string) (string, error) {
	// Validate baseRef
	if baseRef == "" {
		return "", fmt.Errorf("base ref cannot be empty")
	}
	// Prevent flag injection (refs starting with - would be interpreted as git flags).
	// The -- must come AFTER baseRef so git treats baseRef as a revision, not a pathspec.
	if strings.HasPrefix(baseRef, "-") {
		return "", fmt.Errorf("invalid base ref %q: must not start with -", baseRef)
	}
	args := []string{"diff", baseRef, "--"}
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

// TruncateDiff truncates a diff to maxSize bytes, attempting to truncate
// at file boundaries to preserve complete file diffs where possible.
// Returns the truncated diff and a boolean indicating if truncation occurred.
func TruncateDiff(diff string, maxSize int) (string, bool) {
	if len(diff) <= maxSize {
		return diff, false
	}

	// Try to truncate at a file boundary (diff --git line)
	// Look backwards from maxSize to find a good break point
	searchStart := maxSize
	if searchStart > len(diff) {
		searchStart = len(diff)
	}

	// Search for "diff --git" backwards from the max size
	// This ensures we keep complete file diffs
	truncated := diff[:searchStart]
	lastFileStart := strings.LastIndex(truncated, "\ndiff --git ")

	if lastFileStart > maxSize/2 {
		// Found a file boundary in the latter half - truncate there
		truncated = diff[:lastFileStart]
	} else {
		// No good file boundary found, try to truncate at a newline
		lastNewline := strings.LastIndex(truncated, "\n")
		if lastNewline > maxSize/2 {
			truncated = diff[:lastNewline]
		}
		// Otherwise just use the hard cutoff
	}

	return truncated + TruncationNotice, true
}

// BuildPromptWithDiff combines a review prompt with the git diff.
// Large diffs are automatically truncated to fit within context limits.
func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}

	// Truncate diff if it exceeds the maximum size
	truncatedDiff, _ := TruncateDiff(diff, MaxDiffSize)

	return prompt + "\n\n```diff\n" + truncatedDiff + "\n```"
}
