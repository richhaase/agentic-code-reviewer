package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

var ErrPathNotFoundAtRevision = errors.New("path not found at revision")

func RemoteExists(ctx context.Context, repoRoot, remote string) (bool, error) {
	remotes, err := Remotes(ctx, repoRoot)
	if err != nil {
		return false, err
	}
	for _, name := range remotes {
		if name == remote {
			return true, nil
		}
	}
	return false, nil
}

func Remotes(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list git remotes: %w", err)
	}
	return strings.Fields(string(out)), nil
}

func RefExists(ctx context.Context, repoRoot, ref string) (bool, error) {
	if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "-") {
		return false, fmt.Errorf("ref %q is invalid", ref)
	}
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = repoRoot
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect ref %q: %w", ref, err)
}

func RemoteDefaultBranch(ctx context.Context, repoRoot, remote string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", remote, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return "", fmt.Errorf("failed to resolve the default branch for remote %q (%s): %w", remote, message, err)
		}
		return "", fmt.Errorf("failed to resolve the default branch for remote %q: %w", remote, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "ref:" || fields[2] != "HEAD" {
			continue
		}
		const prefix = "refs/heads/"
		if strings.HasPrefix(fields[1], prefix) {
			return strings.TrimPrefix(fields[1], prefix), nil
		}
	}

	return "", fmt.Errorf("remote %q did not advertise a default branch", remote)
}

func ResolveCommit(ctx context.Context, repoRoot, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("trusted ref %q is invalid", ref)
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	cmd.Dir = repoRoot
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("failed to resolve trusted ref %q (%s): %w", ref, message, err)
		}
		return "", fmt.Errorf("failed to resolve trusted ref %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func ReadFileAtCommit(ctx context.Context, repoRoot, commit, repositoryPath string) ([]byte, error) {
	if !isObjectID(commit) {
		return nil, fmt.Errorf("commit %q must be an immutable object ID", commit)
	}
	cleanPath, err := cleanRepositoryPath(repositoryPath)
	if err != nil {
		return nil, err
	}

	object := commit + ":" + cleanPath
	check := exec.CommandContext(ctx, "git", "ls-tree", "-z", commit, "--", cleanPath)
	check.Dir = repoRoot
	entry, err := check.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to inspect %s at %s: %w", cleanPath, commit, err)
	}
	if len(entry) == 0 {
		return nil, fmt.Errorf("%w: %s at %s", ErrPathNotFoundAtRevision, cleanPath, commit)
	}

	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", object)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s at %s: %w", cleanPath, commit, err)
	}
	return out, nil
}

func isObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character >= '0' && character <= '9' {
			continue
		}
		if character >= 'a' && character <= 'f' {
			continue
		}
		if character >= 'A' && character <= 'F' {
			continue
		}
		return false
	}
	return true
}

func cleanRepositoryPath(repositoryPath string) (string, error) {
	if repositoryPath == "" {
		return "", fmt.Errorf("repository path must not be empty")
	}
	if strings.HasPrefix(repositoryPath, "/") {
		return "", fmt.Errorf("repository path %q must be relative", repositoryPath)
	}

	cleanPath := path.Clean(strings.ReplaceAll(repositoryPath, "\\", "/"))
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf("repository path %q escapes the repository", repositoryPath)
	}
	return cleanPath, nil
}
