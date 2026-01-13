// Package git provides git operations including worktree management.
package git

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree with a path.
type Worktree struct {
	Path     string
	repoRoot string
}

// Remove cleans up the worktree.
func (w *Worktree) Remove() error {
	if w.Path == "" {
		return nil
	}
	cmd := exec.Command("git", "worktree", "remove", "--force", w.Path) //nolint:gosec // w.Path is controlled internally
	cmd.Dir = w.repoRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove worktree %s: %w", w.Path, err)
	}
	return nil
}

// GetRoot returns the root directory of the current git repository.
func GetRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetCommonDir returns the git common directory (shared across worktrees).
func GetCommonDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git common dir: %w", err)
	}
	path := strings.TrimSpace(string(out))
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve common dir path: %w", err)
	}
	return abs, nil
}

// ensureWorktreesExcluded adds .worktrees/ to .git/info/exclude if not already present.
func ensureWorktreesExcluded(commonDir string) error {
	infoDir := filepath.Join(commonDir, "info")
	excludePath := filepath.Join(infoDir, "exclude")

	if err := os.MkdirAll(infoDir, 0755); err != nil {
		return fmt.Errorf("failed to create info directory: %w", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read exclude file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if line == ".worktrees/" {
			return nil
		}
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open exclude file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(".worktrees/\n"); err != nil {
		return fmt.Errorf("failed to write to exclude file: %w", err)
	}

	return nil
}

// CreateWorktree creates a temporary worktree for the given branch.
// The caller is responsible for calling Remove() on the returned Worktree.
func CreateWorktree(branch string) (*Worktree, error) {
	commonDir, err := GetCommonDir()
	if err != nil {
		return nil, err
	}

	repoRoot := filepath.Dir(commonDir)

	if err := ensureWorktreesExcluded(commonDir); err != nil {
		return nil, err
	}

	// Generate unique ID
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate worktree ID: %w", err)
	}
	worktreeID := hex.EncodeToString(idBytes)

	safeBranch := strings.ReplaceAll(branch, "/", "-")
	worktreeName := fmt.Sprintf("review-%s-%s", safeBranch, worktreeID)
	worktreesDir := filepath.Join(repoRoot, ".worktrees")
	worktreePath := filepath.Join(worktreesDir, worktreeName)

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", worktreePath, branch)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create worktree for branch '%s': %s", branch, strings.TrimSpace(string(out)))
	}

	return &Worktree{
		Path:     worktreePath,
		repoRoot: repoRoot,
	}, nil
}
