package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary git repository for testing.
// Returns the repo path and a cleanup function.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git name: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to git commit: %v\n%s", err, out)
	}

	return tmpDir
}

func TestWorktree_Remove_EmptyPath(t *testing.T) {
	w := &Worktree{Path: ""}
	err := w.Remove()
	if err != nil {
		t.Errorf("expected no error for empty path, got %v", err)
	}
}

func TestWorktree_Remove_ValidWorktree(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Create a test branch
	cmd := exec.Command("git", "branch", "test-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create branch: %v\n%s", err, out)
	}

	// Create worktree directory
	worktreePath := filepath.Join(repoDir, ".worktrees", "test-wt")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		t.Fatalf("failed to create worktrees dir: %v", err)
	}

	// Create a worktree
	cmd = exec.Command("git", "worktree", "add", worktreePath, "test-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create worktree: %v\n%s", err, out)
	}

	// Verify worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatal("worktree was not created")
	}

	// Test Remove
	w := &Worktree{
		Path:     worktreePath,
		repoRoot: repoDir,
	}
	err := w.Remove()
	if err != nil {
		t.Errorf("failed to remove worktree: %v", err)
	}

	// Verify worktree is gone
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should have been removed")
	}
}

func TestCreateWorktree_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Change to repo dir so GetCommonDir works
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a branch to checkout
	cmd := exec.Command("git", "branch", "feature-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create branch: %v\n%s", err, out)
	}

	// Test CreateWorktree
	wt, err := CreateWorktree("feature-branch")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer wt.Remove()

	// Verify worktree was created
	if wt.Path == "" {
		t.Error("expected non-empty worktree path")
	}
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Error("worktree directory does not exist")
	}

	// Verify path format
	if !strings.Contains(wt.Path, "review-feature-branch-") {
		t.Errorf("worktree path should contain branch name, got %s", wt.Path)
	}
}

func TestCreateWorktree_InvalidBranch(t *testing.T) {
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Try to create worktree for non-existent branch
	_, err = CreateWorktree("nonexistent-branch")
	if err == nil {
		t.Error("expected error for non-existent branch")
	}
}

func TestCreateWorktree_BranchWithSlashes(t *testing.T) {
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a branch with slashes
	cmd := exec.Command("git", "branch", "feature/test/branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create branch: %v\n%s", err, out)
	}

	wt, err := CreateWorktree("feature/test/branch")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer wt.Remove()

	// Verify slashes were replaced with dashes
	if strings.Contains(wt.Path, "/feature") || strings.Contains(wt.Path, "/test") {
		t.Errorf("worktree path should have slashes replaced: %s", wt.Path)
	}
	if !strings.Contains(wt.Path, "review-feature-test-branch-") {
		t.Errorf("worktree path format unexpected: %s", wt.Path)
	}
}

func TestGetRoot_InGitRepo(t *testing.T) {
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	root, err := GetRoot()
	if err != nil {
		t.Fatalf("GetRoot failed: %v", err)
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expectedRoot, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatalf("failed to resolve symlinks: %v", err)
	}
	actualRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("failed to resolve symlinks: %v", err)
	}

	if actualRoot != expectedRoot {
		t.Errorf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestGetRoot_NotInGitRepo(t *testing.T) {
	tmpDir := t.TempDir() // Not a git repo

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer os.Chdir(origDir)

	_, err = GetRoot()
	if err == nil {
		t.Error("expected error when not in git repo")
	}
}

func TestGetCommonDir_InGitRepo(t *testing.T) {
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	commonDir, err := GetCommonDir()
	if err != nil {
		t.Fatalf("GetCommonDir failed: %v", err)
	}

	// Common dir should end with .git
	if !strings.HasSuffix(commonDir, ".git") {
		t.Errorf("common dir should end with .git, got %s", commonDir)
	}
}

func TestEnsureWorktreesExcluded(t *testing.T) {
	repoDir := setupTestRepo(t)
	commonDir := filepath.Join(repoDir, ".git")

	// First call should add .worktrees/
	err := ensureWorktreesExcluded(commonDir)
	if err != nil {
		t.Fatalf("ensureWorktreesExcluded failed: %v", err)
	}

	// Verify exclude file contains .worktrees/
	excludePath := filepath.Join(commonDir, "info", "exclude")
	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	if !strings.Contains(string(content), ".worktrees/") {
		t.Error("exclude file should contain .worktrees/")
	}

	// Second call should be idempotent
	err = ensureWorktreesExcluded(commonDir)
	if err != nil {
		t.Fatalf("second ensureWorktreesExcluded failed: %v", err)
	}

	// Verify it wasn't added twice
	content, err = os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	count := strings.Count(string(content), ".worktrees/")
	if count != 1 {
		t.Errorf("expected .worktrees/ once, found %d times", count)
	}
}

func TestFetchPRRef_CommandFormat(t *testing.T) {
	// This tests the command building logic, not actual git execution
	// We verify the function exists and has correct signature
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// fetchPRRef signature: fetchPRRef(repoRoot, remote, prNumber string)
	// It should fetch to FETCH_HEAD, not create a named branch
	err = fetchPRRef(repoDir, "origin", "123")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention PR #123 (the third parameter), not PR #origin
	if !strings.Contains(err.Error(), "#123") {
		t.Errorf("expected error to mention PR #123, got: %v", err)
	}
}

func TestFetchPRRef_UsesRemoteParameter(t *testing.T) {
	// Test that fetchPRRef uses the remote parameter (second arg) for fetch
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Test with custom remote name - error should mention PR #456
	err = fetchPRRef(repoDir, "upstream", "456")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention PR #456, proving prNumber is the third parameter
	if !strings.Contains(err.Error(), "#456") {
		t.Errorf("expected error to mention PR #456, got: %v", err)
	}
}

func TestCreateWorktreeFromPR_SignatureAndErrorMessage(t *testing.T) {
	// Test that CreateWorktreeFromPR has signature (repoRoot, remote, prNumber)
	// and error messages correctly reference the PR number
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// CreateWorktreeFromPR(repoRoot, remote, prNumber) - prNumber is third arg
	_, err = CreateWorktreeFromPR(repoDir, "origin", "999")

	// Expected to fail because there's no remote
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention PR #999 (the third parameter)
	if !strings.Contains(err.Error(), "#999") {
		t.Errorf("expected error to mention PR #999, got: %v", err)
	}
}

// Tests for FetchBaseRef - Issue 1: PR base ref missing locally breaks diff

func TestFetchBaseRef_CommandFormat(t *testing.T) {
	// Test that FetchBaseRef fetches the base ref from the remote
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// FetchBaseRef should fetch refs/heads/main from remote
	err = FetchBaseRef(repoDir, "origin", "main")

	// Expected to fail because there's no remote
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should indicate what we were trying to fetch
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("expected error to mention 'main', got: %v", err)
	}
}

func TestFetchBaseRef_UsesRemoteParameter(t *testing.T) {
	// Test that FetchBaseRef uses the remote parameter
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Test with custom remote name
	err = FetchBaseRef(repoDir, "upstream", "develop")

	// Expected to fail because there's no remote
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention what we were fetching
	if !strings.Contains(err.Error(), "develop") {
		t.Errorf("expected error to mention 'develop', got: %v", err)
	}
}

func TestQualifyBaseRef_AddsRemote(t *testing.T) {
	// Test that QualifyBaseRef returns remote-qualified ref
	result := QualifyBaseRef("origin", "main")
	if result != "origin/main" {
		t.Errorf("expected 'origin/main', got %q", result)
	}

	result = QualifyBaseRef("upstream", "develop")
	if result != "upstream/develop" {
		t.Errorf("expected 'upstream/develop', got %q", result)
	}
}

func TestQualifyBaseRef_AlreadyQualified(t *testing.T) {
	// Test that already qualified refs are returned as-is
	result := QualifyBaseRef("origin", "origin/main")
	if result != "origin/main" {
		t.Errorf("expected 'origin/main', got %q", result)
	}

	// Different remote prefix should still work
	result = QualifyBaseRef("upstream", "upstream/main")
	if result != "upstream/main" {
		t.Errorf("expected 'upstream/main', got %q", result)
	}
}

// Tests for ShouldQualifyBaseRef - Issue: PR mode rewrites explicit refs

func TestShouldQualifyBaseRef_UnqualifiedBranch(t *testing.T) {
	// Simple unqualified branch names should be qualified (explicit or auto-detected)
	if !ShouldQualifyBaseRef("main", false) {
		t.Error("expected true for unqualified branch 'main'")
	}
	if !ShouldQualifyBaseRef("develop", false) {
		t.Error("expected true for unqualified branch 'develop'")
	}
	if !ShouldQualifyBaseRef("my-feature-branch", false) {
		t.Error("expected true for unqualified branch 'my-feature-branch'")
	}
}

func TestShouldQualifyBaseRef_BranchWithSlash(t *testing.T) {
	// Branches with slashes that look like remote-qualified refs should not be qualified
	if ShouldQualifyBaseRef("origin/main", false) {
		t.Error("expected false for 'origin/main' (looks like remote/branch)")
	}
	if ShouldQualifyBaseRef("upstream/develop", false) {
		t.Error("expected false for 'upstream/develop' (looks like remote/branch)")
	}
}

func TestShouldQualifyBaseRef_AutoDetected(t *testing.T) {
	// Auto-detected base refs from PRs should ALWAYS be qualified
	// These are always unqualified branch names from the GitHub API
	if !ShouldQualifyBaseRef("release/v1.0", true) {
		t.Error("expected true for auto-detected 'release/v1.0'")
	}
	if !ShouldQualifyBaseRef("feature/foo", true) {
		t.Error("expected true for auto-detected 'feature/foo'")
	}
	if !ShouldQualifyBaseRef("main", true) {
		t.Error("expected true for auto-detected 'main'")
	}
}

func TestShouldQualifyBaseRef_ExplicitBranchWithSlash(t *testing.T) {
	// Explicit branches with slashes are ambiguous - we can't tell if it's
	// an unqualified branch (feature/foo) or already qualified (origin/main)
	// For safety, don't qualify them (user should be explicit)
	if ShouldQualifyBaseRef("feature/foo", false) {
		t.Error("expected false for explicit 'feature/foo' (ambiguous)")
	}
	if ShouldQualifyBaseRef("release/v1.0", false) {
		t.Error("expected false for explicit 'release/v1.0' (ambiguous)")
	}
}

func TestShouldQualifyBaseRef_AlreadyQualified(t *testing.T) {
	// Already qualified refs should NOT be qualified again
	if ShouldQualifyBaseRef("origin/main", false) {
		t.Error("expected false for already qualified 'origin/main'")
	}
	if ShouldQualifyBaseRef("upstream/develop", false) {
		t.Error("expected false for already qualified 'upstream/develop'")
	}
	if ShouldQualifyBaseRef("remote/feature/foo", false) {
		t.Error("expected false for already qualified 'remote/feature/foo'")
	}
}

func TestShouldQualifyBaseRef_CommitSHA(t *testing.T) {
	// Commit SHAs should NOT be qualified
	if ShouldQualifyBaseRef("abc123def456", false) {
		t.Error("expected false for short SHA 'abc123def456'")
	}
	if ShouldQualifyBaseRef("abc123def456789012345678901234567890abcd", false) {
		t.Error("expected false for full SHA")
	}
	// 7-char short SHA (minimum git uses)
	if ShouldQualifyBaseRef("abc123d", false) {
		t.Error("expected false for 7-char SHA 'abc123d'")
	}
}

func TestShouldQualifyBaseRef_Tags(t *testing.T) {
	// Tags (v-prefixed) should NOT be qualified
	if ShouldQualifyBaseRef("v1.0.0", false) {
		t.Error("expected false for tag 'v1.0.0'")
	}
	if ShouldQualifyBaseRef("v2.3.4-beta", false) {
		t.Error("expected false for tag 'v2.3.4-beta'")
	}
}

func TestShouldQualifyBaseRef_NonVTags(t *testing.T) {
	// Non-v tags should also NOT be qualified
	// Semver without v prefix
	if ShouldQualifyBaseRef("1.0.0", false) {
		t.Error("expected false for semver tag '1.0.0'")
	}
	if ShouldQualifyBaseRef("2.3.4-beta", false) {
		t.Error("expected false for semver tag '2.3.4-beta'")
	}
	// Date-based tags
	if ShouldQualifyBaseRef("release-2024-01", false) {
		t.Error("expected false for date tag 'release-2024-01'")
	}
	if ShouldQualifyBaseRef("2024.01.15", false) {
		t.Error("expected false for date tag '2024.01.15'")
	}
}

func TestShouldQualifyBaseRef_HEAD(t *testing.T) {
	// HEAD and HEAD-relative refs should NOT be qualified
	if ShouldQualifyBaseRef("HEAD", false) {
		t.Error("expected false for 'HEAD'")
	}
	if ShouldQualifyBaseRef("HEAD~1", false) {
		t.Error("expected false for 'HEAD~1'")
	}
	if ShouldQualifyBaseRef("HEAD^", false) {
		t.Error("expected false for 'HEAD^'")
	}
}
