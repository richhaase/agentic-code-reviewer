// Package git provides git operations including worktree management.
package git

import (
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
