// Package git provides git operations including worktree management.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AddRemote adds a new git remote.
func AddRemote(repoDir, name, url string) error {
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to add remote '%s': %s", name, output)
		}
		return fmt.Errorf("failed to add remote '%s': %w", name, err)
	}
	return nil
}

// RemoveRemote removes a git remote.
func RemoveRemote(repoDir, name string) error {
	cmd := exec.Command("git", "remote", "remove", name)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to remove remote '%s': %s", name, output)
		}
		return fmt.Errorf("failed to remove remote '%s': %w", name, err)
	}
	return nil
}

// FetchBranch fetches a specific branch from a remote.
func FetchBranch(ctx context.Context, repoDir, remote, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, branch)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch '%s' from '%s': %s", branch, remote, output)
		}
		return fmt.Errorf("failed to fetch '%s' from '%s': %w", branch, remote, err)
	}
	return nil
}
