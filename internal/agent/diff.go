package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetGitDiff returns the git diff against the specified base reference.
// If workDir is empty, uses the current directory.
func GetGitDiff(baseRef, workDir string) (string, error) {
	// Use -- to prevent baseRef from being interpreted as a flag if it starts with -
	args := []string{"diff", baseRef, "--"}
	cmd := exec.Command("git", args...)

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
