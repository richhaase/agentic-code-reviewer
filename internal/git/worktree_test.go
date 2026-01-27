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

	// fetchPRRef will fail because there's no remote, but we can verify
	// the function exists and returns an appropriate error
	err = fetchPRRef(repoDir, "123", "test-branch")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention the fetch failure, not a function signature issue
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("expected fetch-related error, got: %v", err)
	}
}
