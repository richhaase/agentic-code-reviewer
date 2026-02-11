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
	cmd := exec.Command("git", "worktree", "remove", "--force", w.Path)
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
		output := strings.TrimSpace(string(out))
		if output != "" {
			return nil, fmt.Errorf("failed to create worktree for branch '%s' (%s): %w", branch, output, err)
		}
		return nil, fmt.Errorf("failed to create worktree for branch '%s': %w", branch, err)
	}

	return &Worktree{
		Path:     worktreePath,
		repoRoot: repoRoot,
	}, nil
}

// FetchBaseRef fetches a base ref (e.g., "main") from the specified remote.
// This ensures the base ref exists locally for diff operations in PR worktrees.
// The ref is fetched as remote/base (e.g., origin/main) which can be used directly
// in git diff commands.
func FetchBaseRef(repoRoot, remote, baseRef string) error {
	// Skip if already remote-qualified
	if strings.HasPrefix(baseRef, remote+"/") {
		return nil
	}

	// Fetch the base ref from remote
	refSpec := fmt.Sprintf("refs/heads/%s:refs/remotes/%s/%s", baseRef, remote, baseRef)
	cmd := exec.Command("git", "fetch", remote, refSpec)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch base ref '%s' (%s): %w", baseRef, output, err)
		}
		return fmt.Errorf("failed to fetch base ref '%s': %w", baseRef, err)
	}
	return nil
}

// QualifyBaseRef returns a remote-qualified ref (e.g., "origin/main") for use in diff.
// If the ref is already qualified with the given remote, it's returned as-is.
func QualifyBaseRef(remote, baseRef string) string {
	if strings.HasPrefix(baseRef, remote+"/") {
		return baseRef
	}
	return remote + "/" + baseRef
}

// ShouldQualifyBaseRef returns true if the base ref should be qualified with a remote prefix.
// The autoDetected parameter indicates whether the ref was auto-detected from a PR (always
// an unqualified branch name) vs explicitly set by the user (may be SHA, tag, or qualified ref).
//
// When autoDetected is true, always returns true (PR base refs are always unqualified branches).
// When autoDetected is false, returns false for:
// - Already qualified refs (e.g., origin/main)
// - Commit SHAs (hex strings of 7+ chars)
// - Tags (v-prefixed, semver, or date-based patterns)
// - HEAD and HEAD-relative refs
// - Refs containing "/" (ambiguous - could be branch or qualified ref)
func ShouldQualifyBaseRef(baseRef string, autoDetected bool) bool {
	// Auto-detected refs from PRs are always unqualified branch names
	// from the GitHub API - always qualify them
	if autoDetected {
		return true
	}

	// HEAD and HEAD-relative refs
	if baseRef == "HEAD" || strings.HasPrefix(baseRef, "HEAD~") || strings.HasPrefix(baseRef, "HEAD^") {
		return false
	}

	// Commit SHAs: 7-40 hex characters
	if isHexString(baseRef) && len(baseRef) >= 7 && len(baseRef) <= 40 {
		return false
	}

	// Tags: check for various common patterns
	if looksLikeTag(baseRef) {
		return false
	}

	// Refs containing "/" are ambiguous (could be origin/main or feature/foo)
	// For safety, don't qualify them - user should be explicit
	if strings.Contains(baseRef, "/") {
		return false
	}

	return true
}

// looksLikeTag returns true if the ref looks like a tag rather than a branch.
// Recognizes:
// - v-prefixed semver: v1.0.0, v2.3.4-beta
// - bare semver: 1.0.0, 2.3.4-rc1
// - date-based: 2024.01.15, release-2024-01
func looksLikeTag(ref string) bool {
	// v-prefixed semver (v1.0.0, v2.3.4-beta)
	if strings.HasPrefix(ref, "v") && len(ref) > 1 && ref[1] >= '0' && ref[1] <= '9' {
		return true
	}

	// Bare semver starting with digit (1.0.0, 2.3.4-rc1)
	if len(ref) > 0 && ref[0] >= '0' && ref[0] <= '9' {
		// Must contain at least one dot or hyphen to look like a version
		if strings.Contains(ref, ".") || strings.Contains(ref, "-") {
			return true
		}
	}

	// Date-based patterns (release-2024-01, 2024.01.15)
	// Look for 4-digit year pattern
	if containsYearPattern(ref) {
		return true
	}

	return false
}

// containsYearPattern checks if the string contains a 4-digit year (19xx or 20xx).
func containsYearPattern(s string) bool {
	for i := 0; i <= len(s)-4; i++ {
		if (s[i] == '1' && s[i+1] == '9') || (s[i] == '2' && s[i+1] == '0') {
			if s[i+2] >= '0' && s[i+2] <= '9' && s[i+3] >= '0' && s[i+3] <= '9' {
				return true
			}
		}
	}
	return false
}

// isHexString returns true if s contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}
	return len(s) > 0
}

// fetchPRRef fetches a PR ref from the specified remote to FETCH_HEAD.
// This avoids creating a named local branch, preventing collisions with
// existing branches or checked-out branches.
func fetchPRRef(repoRoot, remote, prNumber string) error {
	// Fetch PR head to FETCH_HEAD (no local branch created)
	refSpec := fmt.Sprintf("pull/%s/head", prNumber)
	cmd := exec.Command("git", "fetch", remote, refSpec)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch PR #%s (%s): %w", prNumber, output, err)
		}
		return fmt.Errorf("failed to fetch PR #%s: %w", prNumber, err)
	}
	return nil
}

// CreateWorktreeFromPR fetches a PR and creates a detached worktree for it.
// The remote parameter specifies which remote to fetch from (e.g., "origin").
// The worktree is created in detached HEAD mode from FETCH_HEAD, avoiding
// conflicts with existing local branches.
// The caller is responsible for calling Remove() on the returned Worktree.
func CreateWorktreeFromPR(repoRoot, remote, prNumber string) (*Worktree, error) {
	// Fetch the PR ref to FETCH_HEAD (no local branch created)
	if err := fetchPRRef(repoRoot, remote, prNumber); err != nil {
		return nil, err
	}

	commonDir, err := GetCommonDir()
	if err != nil {
		return nil, err
	}

	if err := ensureWorktreesExcluded(commonDir); err != nil {
		return nil, err
	}

	// Generate unique ID
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate worktree ID: %w", err)
	}
	worktreeID := hex.EncodeToString(idBytes)

	worktreeName := fmt.Sprintf("review-pr%s-%s", prNumber, worktreeID)
	worktreesDir := filepath.Join(repoRoot, ".worktrees")
	worktreePath := filepath.Join(worktreesDir, worktreeName)

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	// Create detached worktree from FETCH_HEAD - avoids branch name conflicts
	cmd := exec.Command("git", "worktree", "add", "--detach", worktreePath, "FETCH_HEAD")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return nil, fmt.Errorf("failed to create worktree for PR #%s (%s): %w", prNumber, output, err)
		}
		return nil, fmt.Errorf("failed to create worktree for PR #%s: %w", prNumber, err)
	}

	return &Worktree{
		Path:     worktreePath,
		repoRoot: repoRoot,
	}, nil
}
