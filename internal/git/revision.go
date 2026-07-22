package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

var ErrPathNotFoundAtRevision = errors.New("path not found at revision")

const maxRepositorySymlinkDepth = 32

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
	return readFileAtCommit(ctx, repoRoot, commit, cleanPath, 0, make(map[string]struct{}))
}

func ValidateRepositoryPath(repositoryPath string) error {
	_, err := cleanRepositoryPath(repositoryPath)
	return err
}

func ReadFileWithinRepository(repositoryRoot, repositoryPath string) ([]byte, error) {
	cleanPath, err := cleanRepositoryPath(repositoryPath)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root %q: %w", repositoryRoot, err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository root %q: %w", repositoryRoot, err)
	}
	return readFileWithinRepository(root, cleanPath, 0, make(map[string]struct{}))
}

func readFileWithinRepository(repositoryRoot, repositoryPath string, depth int, visited map[string]struct{}) ([]byte, error) {
	if depth > maxRepositorySymlinkDepth {
		return nil, fmt.Errorf("repository path %q exceeds the symlink resolution limit", repositoryPath)
	}
	if _, exists := visited[repositoryPath]; exists {
		return nil, fmt.Errorf("repository path %q contains a symlink cycle", repositoryPath)
	}
	visited[repositoryPath] = struct{}{}

	components := strings.Split(repositoryPath, "/")
	for index := range components {
		candidate := path.Join(components[:index+1]...)
		candidatePath := filepath.Join(repositoryRoot, filepath.FromSlash(candidate))
		info, err := os.Lstat(candidatePath)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(candidatePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read repository symlink %q: %w", candidate, err)
			}
			if target == "" {
				return nil, fmt.Errorf("repository symlink %q has an empty target", candidate)
			}
			normalizedTarget := strings.ReplaceAll(target, "\\", "/")
			if isAbsoluteRepositoryPath(normalizedTarget) {
				return nil, fmt.Errorf("repository symlink %q has an absolute target %q", candidate, target)
			}
			resolvedPath := path.Join(path.Dir(candidate), normalizedTarget)
			if index+1 < len(components) {
				resolvedPath = path.Join(resolvedPath, path.Join(components[index+1:]...))
			}
			resolvedPath, err = cleanRepositoryPath(resolvedPath)
			if err != nil {
				return nil, fmt.Errorf("repository symlink %q is invalid: %w", candidate, err)
			}
			return readFileWithinRepository(repositoryRoot, resolvedPath, depth+1, visited)
		}
		if index+1 < len(components) {
			if !info.IsDir() {
				return nil, fmt.Errorf("repository path %q is not a directory", candidate)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("repository path %q is not a regular file or symlink", repositoryPath)
		}
		return os.ReadFile(candidatePath)
	}
	return nil, fmt.Errorf("repository path %q is invalid", repositoryPath)
}

func readFileAtCommit(ctx context.Context, repoRoot, commit, repositoryPath string, depth int, visited map[string]struct{}) ([]byte, error) {
	if depth > maxRepositorySymlinkDepth {
		return nil, fmt.Errorf("repository path %q exceeds the symlink resolution limit", repositoryPath)
	}
	if _, exists := visited[repositoryPath]; exists {
		return nil, fmt.Errorf("repository path %q contains a symlink cycle", repositoryPath)
	}
	visited[repositoryPath] = struct{}{}

	components := strings.Split(repositoryPath, "/")
	for index := range components {
		candidate := path.Join(components[:index+1]...)
		entry, err := treeEntryAtCommit(ctx, repoRoot, commit, candidate)
		if err != nil {
			return nil, err
		}
		mode, err := treeEntryMode(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect %s at %s: %w", candidate, commit, err)
		}
		if mode == "120000" {
			targetBytes, err := readBlobAtCommit(ctx, repoRoot, commit, candidate)
			if err != nil {
				return nil, err
			}
			target := string(targetBytes)
			if target == "" {
				return nil, fmt.Errorf("repository symlink %q at %s has an empty target", candidate, commit)
			}
			normalizedTarget := strings.ReplaceAll(target, "\\", "/")
			if isAbsoluteRepositoryPath(normalizedTarget) {
				return nil, fmt.Errorf("repository symlink %q at %s has an absolute target %q", candidate, commit, target)
			}
			resolvedPath := path.Join(path.Dir(candidate), normalizedTarget)
			if index+1 < len(components) {
				resolvedPath = path.Join(resolvedPath, path.Join(components[index+1:]...))
			}
			resolvedPath, err = cleanRepositoryPath(resolvedPath)
			if err != nil {
				return nil, fmt.Errorf("repository symlink %q at %s is invalid: %w", candidate, commit, err)
			}
			data, err := readFileAtCommit(ctx, repoRoot, commit, resolvedPath, depth+1, visited)
			if errors.Is(err, ErrPathNotFoundAtRevision) {
				return nil, fmt.Errorf("repository symlink %q at %s resolves to missing path %q", candidate, commit, resolvedPath)
			}
			return data, err
		}
		if index+1 < len(components) {
			if mode != "040000" {
				return nil, fmt.Errorf("repository path %q at %s is not a directory", candidate, commit)
			}
			continue
		}
		if mode != "100644" && mode != "100755" {
			return nil, fmt.Errorf("repository path %q at %s is not a regular file or symlink", repositoryPath, commit)
		}
		return readBlobAtCommit(ctx, repoRoot, commit, repositoryPath)
	}
	return nil, fmt.Errorf("%w: %s at %s", ErrPathNotFoundAtRevision, repositoryPath, commit)
}

func treeEntryAtCommit(ctx context.Context, repoRoot, commit, repositoryPath string) ([]byte, error) {
	check := exec.CommandContext(ctx, "git", "ls-tree", "-z", commit, "--", repositoryPath)
	check.Dir = repoRoot
	entry, err := check.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to inspect %s at %s: %w", repositoryPath, commit, err)
	}
	if len(entry) == 0 {
		return nil, fmt.Errorf("%w: %s at %s", ErrPathNotFoundAtRevision, repositoryPath, commit)
	}
	return entry, nil
}

func readBlobAtCommit(ctx context.Context, repoRoot, commit, repositoryPath string) ([]byte, error) {
	object := commit + ":" + repositoryPath
	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", object)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to read %s at %s: %w", repositoryPath, commit, err)
	}
	return out, nil
}

func treeEntryMode(entry []byte) (string, error) {
	if len(entry) == 0 || entry[len(entry)-1] != 0 || bytes.Count(entry, []byte{0}) != 1 {
		return "", fmt.Errorf("unexpected git ls-tree output")
	}
	record := entry[:len(entry)-1]
	metadata, _, found := bytes.Cut(record, []byte{'\t'})
	if !found {
		return "", fmt.Errorf("unexpected git ls-tree output")
	}
	fields := bytes.Fields(metadata)
	if len(fields) != 3 {
		return "", fmt.Errorf("unexpected git ls-tree entry")
	}
	return string(fields[0]), nil
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
	normalizedPath := strings.ReplaceAll(repositoryPath, "\\", "/")
	if isAbsoluteRepositoryPath(normalizedPath) {
		return "", fmt.Errorf("repository path %q must be relative", repositoryPath)
	}

	cleanPath := path.Clean(normalizedPath)
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf("repository path %q escapes the repository", repositoryPath)
	}
	return cleanPath, nil
}

func isAbsoluteRepositoryPath(repositoryPath string) bool {
	return strings.HasPrefix(repositoryPath, "/") || hasWindowsVolumePrefix(repositoryPath)
}

func hasWindowsVolumePrefix(repositoryPath string) bool {
	if len(repositoryPath) < 2 || repositoryPath[1] != ':' {
		return false
	}
	letter := repositoryPath[0]
	return letter >= 'a' && letter <= 'z' || letter >= 'A' && letter <= 'Z'
}
