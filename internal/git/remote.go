package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

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

func RemoteURL(ctx context.Context, repoDir, remote string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", remote)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get url for remote '%s': %w", remote, err)
	}
	return strings.TrimSpace(string(out)), nil
}

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
