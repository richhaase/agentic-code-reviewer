package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

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

	cmd := exec.Command("git", "branch", "test-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create branch: %v\n%s", err, out)
	}

	worktreePath := filepath.Join(repoDir, ".worktrees", "test-wt")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		t.Fatalf("failed to create worktrees dir: %v", err)
	}

	cmd = exec.Command("git", "worktree", "add", worktreePath, "test-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create worktree: %v\n%s", err, out)
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Fatal("worktree was not created")
	}

	w := &Worktree{
		Path:     worktreePath,
		repoRoot: repoDir,
	}
	err := w.Remove()
	if err != nil {
		t.Errorf("failed to remove worktree: %v", err)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should have been removed")
	}
}

func TestCreateWorktree_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	cmd := exec.Command("git", "branch", "feature-branch")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create branch: %v\n%s", err, out)
	}

	wt, err := CreateWorktree("feature-branch")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer wt.Remove()

	if wt.Path == "" {
		t.Error("expected non-empty worktree path")
	}
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Error("worktree directory does not exist")
	}

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
	tmpDir := t.TempDir()

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

	if !strings.HasSuffix(commonDir, ".git") {
		t.Errorf("common dir should end with .git, got %s", commonDir)
	}
}

func TestEnsureWorktreesExcluded(t *testing.T) {
	repoDir := setupTestRepo(t)
	commonDir := filepath.Join(repoDir, ".git")

	err := ensureWorktreesExcluded(commonDir)
	if err != nil {
		t.Fatalf("ensureWorktreesExcluded failed: %v", err)
	}

	excludePath := filepath.Join(commonDir, "info", "exclude")
	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	if !strings.Contains(string(content), ".worktrees/") {
		t.Error("exclude file should contain .worktrees/")
	}

	err = ensureWorktreesExcluded(commonDir)
	if err != nil {
		t.Fatalf("second ensureWorktreesExcluded failed: %v", err)
	}

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

	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	err = fetchPRRef(repoDir, "origin", "123")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}

	if !strings.Contains(err.Error(), "#123") {
		t.Errorf("expected error to mention PR #123, got: %v", err)
	}
}

func TestFetchPRRef_UsesRemoteParameter(t *testing.T) {

	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	err = fetchPRRef(repoDir, "upstream", "456")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}

	if !strings.Contains(err.Error(), "#456") {
		t.Errorf("expected error to mention PR #456, got: %v", err)
	}
}

func TestCreateWorktreeFromPR_SignatureAndErrorMessage(t *testing.T) {

	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	_, err = CreateWorktreeFromPR(repoDir, "origin", "999")

	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}

	if !strings.Contains(err.Error(), "#999") {
		t.Errorf("expected error to mention PR #999, got: %v", err)
	}
}

func TestFetchBaseRef_CommandFormat(t *testing.T) {

	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	err = FetchBaseRef(context.Background(), repoDir, "origin", "main")

	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}

	if !strings.Contains(err.Error(), "main") {
		t.Errorf("expected error to mention 'main', got: %v", err)
	}
}

func TestFetchBaseRef_UsesRemoteParameter(t *testing.T) {

	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	err = FetchBaseRef(context.Background(), repoDir, "upstream", "develop")

	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}

	if !strings.Contains(err.Error(), "develop") {
		t.Errorf("expected error to mention 'develop', got: %v", err)
	}
}

func TestFetchBaseRefHonorsCancellation(t *testing.T) {
	repoDir := setupTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := FetchBaseRef(ctx, repoDir, "origin", "main")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchBaseRef() error = %v", err)
	}
}

func TestFetchBaseRefAcceptsForcedRemoteUpdate(t *testing.T) {
	seedRoot := setupTestRepo(t)
	runWorktreeGit(t, seedRoot, "branch", "-M", "main")
	initialRevision := strings.TrimSpace(runWorktreeGit(t, seedRoot, "rev-parse", "HEAD"))
	remoteRoot := filepath.Join(t.TempDir(), "origin.git")
	workingRoot := filepath.Join(t.TempDir(), "working")
	runWorktreeGit(t, t.TempDir(), "clone", "--bare", seedRoot, remoteRoot)
	runWorktreeGit(t, t.TempDir(), "clone", remoteRoot, workingRoot)
	runWorktreeGit(t, seedRoot, "remote", "add", "origin", remoteRoot)

	if err := os.WriteFile(filepath.Join(seedRoot, "test.txt"), []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	runWorktreeGit(t, seedRoot, "add", "test.txt")
	runWorktreeGit(t, seedRoot, "commit", "-m", "second")
	runWorktreeGit(t, seedRoot, "push", "origin", "main")
	if err := FetchBaseRef(context.Background(), workingRoot, "origin", "main"); err != nil {
		t.Fatal(err)
	}

	runWorktreeGit(t, seedRoot, "reset", "--hard", initialRevision)
	if err := os.WriteFile(filepath.Join(seedRoot, "test.txt"), []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	runWorktreeGit(t, seedRoot, "add", "test.txt")
	runWorktreeGit(t, seedRoot, "commit", "-m", "replacement")
	runWorktreeGit(t, seedRoot, "push", "--force", "origin", "main")

	if err := FetchBaseRef(context.Background(), workingRoot, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(runWorktreeGit(t, seedRoot, "rev-parse", "HEAD"))
	got := strings.TrimSpace(runWorktreeGit(t, workingRoot, "rev-parse", "refs/remotes/origin/main"))
	if got != want {
		t.Fatalf("remote-tracking revision = %s, want %s", got, want)
	}
}

func runWorktreeGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestQualifyBaseRef_AddsRemote(t *testing.T) {

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

	result := QualifyBaseRef("origin", "origin/main")
	if result != "origin/main" {
		t.Errorf("expected 'origin/main', got %q", result)
	}

	result = QualifyBaseRef("upstream", "upstream/main")
	if result != "upstream/main" {
		t.Errorf("expected 'upstream/main', got %q", result)
	}
}

func TestShouldQualifyBaseRef_UnqualifiedBranch(t *testing.T) {

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

	if ShouldQualifyBaseRef("origin/main", false) {
		t.Error("expected false for 'origin/main' (looks like remote/branch)")
	}
	if ShouldQualifyBaseRef("upstream/develop", false) {
		t.Error("expected false for 'upstream/develop' (looks like remote/branch)")
	}
}

func TestShouldQualifyBaseRef_AutoDetected(t *testing.T) {

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

	if ShouldQualifyBaseRef("feature/foo", false) {
		t.Error("expected false for explicit 'feature/foo' (ambiguous)")
	}
	if ShouldQualifyBaseRef("release/v1.0", false) {
		t.Error("expected false for explicit 'release/v1.0' (ambiguous)")
	}
}

func TestShouldQualifyBaseRef_AlreadyQualified(t *testing.T) {

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

	if ShouldQualifyBaseRef("abc123def456", false) {
		t.Error("expected false for short SHA 'abc123def456'")
	}
	if ShouldQualifyBaseRef("abc123def456789012345678901234567890abcd", false) {
		t.Error("expected false for full SHA")
	}

	if ShouldQualifyBaseRef("abc123d", false) {
		t.Error("expected false for 7-char SHA 'abc123d'")
	}
}

func TestShouldQualifyBaseRef_Tags(t *testing.T) {

	if ShouldQualifyBaseRef("v1.0.0", false) {
		t.Error("expected false for tag 'v1.0.0'")
	}
	if ShouldQualifyBaseRef("v2.3.4-beta", false) {
		t.Error("expected false for tag 'v2.3.4-beta'")
	}
}

func TestShouldQualifyBaseRef_NonVTags(t *testing.T) {

	if ShouldQualifyBaseRef("1.0.0", false) {
		t.Error("expected false for semver tag '1.0.0'")
	}
	if ShouldQualifyBaseRef("2.3.4-beta", false) {
		t.Error("expected false for semver tag '2.3.4-beta'")
	}

	if ShouldQualifyBaseRef("release-2024-01", false) {
		t.Error("expected false for date tag 'release-2024-01'")
	}
	if ShouldQualifyBaseRef("2024.01.15", false) {
		t.Error("expected false for date tag '2024.01.15'")
	}
}

func TestShouldQualifyBaseRef_HEAD(t *testing.T) {

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
