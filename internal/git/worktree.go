package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Worktree struct {
	Path     string
	repoRoot string
}

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

func GetRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func GetHeadSHA(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

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

const staleWorktreeAge = 2 * time.Hour

func PruneStaleWorktrees() error {
	root, err := GetRoot()
	if err != nil {
		return err
	}

	worktreesDir := filepath.Join(root, ".worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read worktrees directory: %w", err)
	}

	cutoff := time.Now().Add(-staleWorktreeAge)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		if !strings.HasPrefix(name, "review-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			wtPath := filepath.Join(worktreesDir, name)

			cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
			cmd.Dir = root
			if err := cmd.Run(); err != nil {

				_ = os.RemoveAll(wtPath)
			}
		}
	}

	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = root
	_ = cmd.Run()

	return nil
}

func CreateWorktree(branch string) (*Worktree, error) {
	commonDir, err := GetCommonDir()
	if err != nil {
		return nil, err
	}

	repoRoot := filepath.Dir(commonDir)

	if err := ensureWorktreesExcluded(commonDir); err != nil {
		return nil, err
	}

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

func FetchBaseRef(ctx context.Context, repoRoot, remote, baseRef string) error {

	if strings.HasPrefix(baseRef, remote+"/") {
		return nil
	}
	return FetchRemoteTrackingBranch(ctx, repoRoot, remote, baseRef)
}

func FetchRemoteTrackingBranch(ctx context.Context, repoRoot, remote, branch string) error {
	refSpec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, refSpec)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("failed to fetch base ref '%s': %w", branch, ctx.Err())
		}
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch base ref '%s' (%s): %w", branch, output, err)
		}
		return fmt.Errorf("failed to fetch base ref '%s': %w", branch, err)
	}
	return nil
}

func QualifyBaseRef(remote, baseRef string) string {
	if strings.HasPrefix(baseRef, remote+"/") {
		return baseRef
	}
	return remote + "/" + baseRef
}

func ShouldQualifyBaseRef(baseRef string, autoDetected bool) bool {

	if autoDetected {
		return true
	}

	if baseRef == "HEAD" || strings.HasPrefix(baseRef, "HEAD~") || strings.HasPrefix(baseRef, "HEAD^") {
		return false
	}

	if isHexString(baseRef) && len(baseRef) >= 7 && len(baseRef) <= 40 {
		return false
	}

	if looksLikeTag(baseRef) {
		return false
	}

	if strings.Contains(baseRef, "/") {
		return false
	}

	return true
}

func looksLikeTag(ref string) bool {

	if strings.HasPrefix(ref, "v") && len(ref) > 1 && ref[1] >= '0' && ref[1] <= '9' {
		return true
	}

	if len(ref) > 0 && ref[0] >= '0' && ref[0] <= '9' {

		if strings.Contains(ref, ".") || strings.Contains(ref, "-") {
			return true
		}
	}

	if containsYearPattern(ref) {
		return true
	}

	return false
}

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

func fetchPRRef(repoRoot, remote, prNumber string) error {

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

func CreateWorktreeFromPR(repoRoot, remote, prNumber string) (*Worktree, error) {

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
