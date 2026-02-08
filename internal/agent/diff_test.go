package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetGitDiff_EmptyBaseRef(t *testing.T) {
	ctx := context.Background()
	_, err := GetGitDiff(ctx, "", "")
	if err == nil {
		t.Error("GetGitDiff() should return error for empty baseRef")
	}
	if !strings.Contains(err.Error(), "base ref cannot be empty") {
		t.Errorf("GetGitDiff() error = %v, want error containing 'base ref cannot be empty'", err)
	}
}

func TestGetGitDiff_InvalidBaseRef(t *testing.T) {
	ctx := context.Background()
	_, err := GetGitDiff(ctx, "-invalidref", "")
	if err == nil {
		t.Error("GetGitDiff() should return error for baseRef starting with -")
	}
	if !strings.Contains(err.Error(), "must not start with -") {
		t.Errorf("GetGitDiff() error = %v, want error containing 'must not start with -'", err)
	}
}

func TestGetGitDiff_Basic(t *testing.T) {
	// Create a temporary git repo for testing
	tmpDir, err := os.MkdirTemp("", "git-diff-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	if err := runGit(tmpDir, "init"); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit(tmpDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runGit(tmpDir, "commit", "-m", "initial"); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Modify file
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Get diff
	ctx := context.Background()
	diff, err := GetGitDiff(ctx, "HEAD", tmpDir)
	if err != nil {
		t.Fatalf("GetGitDiff() error = %v", err)
	}

	// Verify diff contains the change
	if !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+modified") {
		t.Errorf("GetGitDiff() diff doesn't contain expected changes: %s", diff)
	}
}

func TestFetchRemoteRef_AlreadyHasOriginPrefix(t *testing.T) {
	ctx := context.Background()
	result := FetchRemoteRef(ctx, "origin/main", "")

	if result.ResolvedRef != "origin/main" {
		t.Errorf("FetchRemoteRef() ResolvedRef = %q, want %q", result.ResolvedRef, "origin/main")
	}
	if !result.RefResolved {
		t.Error("FetchRemoteRef() RefResolved = false, want true")
	}
	if result.FetchAttempted {
		t.Error("FetchRemoteRef() FetchAttempted = true, want false (no fetch needed)")
	}
}

func TestFetchRemoteRef_SkipsNonBranchRefs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		baseRef string
	}{
		{"flag injection attempt", "-c protocol.file.allow=always"},
		{"relative ref with tilde", "HEAD~3"},
		{"relative ref with caret", "main^2"},
		{"HEAD", "HEAD"},
		{"short commit SHA", "abc1234"},
		{"full commit SHA", "abc1234567890abcdef1234567890abcdef1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FetchRemoteRef(ctx, tt.baseRef, "")

			// These refs should be returned as-is without fetch attempt
			if result.ResolvedRef != tt.baseRef {
				t.Errorf("FetchRemoteRef(%q) ResolvedRef = %q, want %q", tt.baseRef, result.ResolvedRef, tt.baseRef)
			}
			if !result.RefResolved {
				t.Errorf("FetchRemoteRef(%q) RefResolved = false, want true", tt.baseRef)
			}
			if result.FetchAttempted {
				t.Errorf("FetchRemoteRef(%q) FetchAttempted = true, want false (skip fetch for non-branch refs)", tt.baseRef)
			}
		})
	}
}

func TestIsLikelyCommitSHA(t *testing.T) {
	tests := []struct {
		ref      string
		expected bool
	}{
		{"abc1234", true}, // 7 char short SHA
		{"abc1234567890abcdef1234567890abcdef1234", true}, // 40 char full SHA
		{"ABC1234", true}, // uppercase hex
		{"main", false},   // branch name
		{"HEAD~3", false}, // contains ~
		{"abc123", false}, // too short (6 chars)
		{"abc123456789012345678901234567890123456789", false}, // too long (41 chars)
		{"xyz1234", false}, // contains non-hex
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			result := isLikelyCommitSHA(tt.ref)
			if result != tt.expected {
				t.Errorf("isLikelyCommitSHA(%q) = %v, want %v", tt.ref, result, tt.expected)
			}
		})
	}
}

func TestFetchRemoteRef_NoRemote(t *testing.T) {
	// Create a temporary git repo for testing (no remote configured)
	tmpDir, err := os.MkdirTemp("", "git-fetch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	if err := runGit(tmpDir, "init"); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create initial commit on master/main
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit(tmpDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runGit(tmpDir, "commit", "-m", "initial"); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Get current branch name
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = tmpDir
	branchOutput, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get current branch: %v", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	// Fetch should fail (no remote), and fall back to local ref
	ctx := context.Background()
	result := FetchRemoteRef(ctx, branchName, tmpDir)

	if result.ResolvedRef != branchName {
		t.Errorf("FetchRemoteRef() ResolvedRef = %q, want %q (local fallback)", result.ResolvedRef, branchName)
	}
	if result.RefResolved {
		t.Error("FetchRemoteRef() RefResolved = true, want false (no remote)")
	}
	if !result.FetchAttempted {
		t.Error("FetchRemoteRef() FetchAttempted = false, want true")
	}
}

func TestFetchRemoteRef_WithRemote(t *testing.T) {
	// Create a temporary git repo for testing
	tmpDir, err := os.MkdirTemp("", "git-fetch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize git repo
	if err := runGit(tmpDir, "init"); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit(tmpDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runGit(tmpDir, "commit", "-m", "initial"); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Add self as remote (for testing purposes)
	if err := runGit(tmpDir, "remote", "add", "origin", tmpDir); err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	// Get current branch name
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = tmpDir
	branchOutput, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get current branch: %v", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	// Fetch should succeed
	ctx := context.Background()
	result := FetchRemoteRef(ctx, branchName, tmpDir)

	expectedRef := "origin/" + branchName
	if result.ResolvedRef != expectedRef {
		t.Errorf("FetchRemoteRef() ResolvedRef = %q, want %q", result.ResolvedRef, expectedRef)
	}
	if !result.RefResolved {
		t.Error("FetchRemoteRef() RefResolved = false, want true")
	}
	if !result.FetchAttempted {
		t.Error("FetchRemoteRef() FetchAttempted = false, want true")
	}
}

func TestBuildPromptWithDiff_EmptyDiff(t *testing.T) {
	prompt := "Review this code"
	result := BuildPromptWithDiff(prompt, "")
	expected := "Review this code\n\n(No changes detected)"
	if result != expected {
		t.Errorf("BuildPromptWithDiff() = %q, want %q", result, expected)
	}
}

func TestBuildPromptWithDiff_WithDiff(t *testing.T) {
	prompt := "Review this code"
	diff := "- old\n+ new"
	result := BuildPromptWithDiff(prompt, diff)
	if !strings.Contains(result, prompt) {
		t.Errorf("BuildPromptWithDiff() result doesn't contain prompt")
	}
	if !strings.Contains(result, "```diff") {
		t.Errorf("BuildPromptWithDiff() result doesn't contain diff block")
	}
	if !strings.Contains(result, diff) {
		t.Errorf("BuildPromptWithDiff() result doesn't contain diff")
	}
}

func TestIsRelativeRef(t *testing.T) {
	tests := []struct {
		ref      string
		expected bool
	}{
		{"HEAD", true},
		{"HEAD~3", true},
		{"HEAD~1", true},
		{"main^2", true},
		{"abc1234", true},  // commit SHA
		{"main", false},    // branch name
		{"develop", false}, // branch name
		{"origin/main", false},
		{"v1.0.0", false}, // tag
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			result := IsRelativeRef(tt.ref)
			if result != tt.expected {
				t.Errorf("IsRelativeRef(%q) = %v, want %v", tt.ref, result, tt.expected)
			}
		})
	}
}

func TestUpdateCurrentBranch_DetachedHEAD(t *testing.T) {
	tmpDir := createTestRepo(t)

	// Detach HEAD
	if err := runGit(tmpDir, "checkout", "--detach"); err != nil {
		t.Fatalf("Failed to detach HEAD: %v", err)
	}

	ctx := context.Background()
	result := UpdateCurrentBranch(ctx, tmpDir)

	if !result.Skipped {
		t.Error("UpdateCurrentBranch() Skipped = false, want true for detached HEAD")
	}
	if result.SkipReason != "detached HEAD" {
		t.Errorf("UpdateCurrentBranch() SkipReason = %q, want %q", result.SkipReason, "detached HEAD")
	}
	if result.BranchName != "" {
		t.Errorf("UpdateCurrentBranch() BranchName = %q, want empty", result.BranchName)
	}
}

func TestUpdateCurrentBranch_NoRemote(t *testing.T) {
	tmpDir := createTestRepo(t)

	ctx := context.Background()
	result := UpdateCurrentBranch(ctx, tmpDir)

	if result.Error == nil {
		t.Error("UpdateCurrentBranch() Error = nil, want error for no remote")
	}
	if result.Skipped {
		t.Error("UpdateCurrentBranch() Skipped = true, want false (branch exists, fetch should be attempted)")
	}
	if result.BranchName == "" {
		t.Error("UpdateCurrentBranch() BranchName is empty, want branch name")
	}
}

func TestUpdateCurrentBranch_AlreadyUpToDate(t *testing.T) {
	tmpDir := createTestRepo(t)

	// Add self as remote so fetch succeeds
	if err := runGit(tmpDir, "remote", "add", "origin", tmpDir); err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	ctx := context.Background()
	result := UpdateCurrentBranch(ctx, tmpDir)

	if result.Error != nil {
		t.Errorf("UpdateCurrentBranch() Error = %v, want nil", result.Error)
	}
	if !result.AlreadyCurrent {
		t.Error("UpdateCurrentBranch() AlreadyCurrent = false, want true")
	}
	if result.Updated {
		t.Error("UpdateCurrentBranch() Updated = true, want false")
	}
}

func TestUpdateCurrentBranch_FastForward(t *testing.T) {
	// Create the "origin" repo
	originDir := createTestRepo(t)

	// Clone it
	cloneDir, err := os.MkdirTemp("", "git-update-clone-*")
	if err != nil {
		t.Fatalf("Failed to create clone dir: %v", err)
	}
	defer os.RemoveAll(cloneDir)

	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if err := cloneCmd.Run(); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}
	if err := runGit(cloneDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email in clone: %v", err)
	}
	if err := runGit(cloneDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name in clone: %v", err)
	}

	// Add a new commit to the origin
	originFile := filepath.Join(originDir, "new.txt")
	if err := os.WriteFile(originFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("Failed to write file in origin: %v", err)
	}
	if err := runGit(originDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add in origin: %v", err)
	}
	if err := runGit(originDir, "commit", "-m", "new commit"); err != nil {
		t.Fatalf("Failed to git commit in origin: %v", err)
	}

	// The clone is now behind origin — UpdateCurrentBranch should fast-forward
	ctx := context.Background()
	result := UpdateCurrentBranch(ctx, cloneDir)

	if result.Error != nil {
		t.Errorf("UpdateCurrentBranch() Error = %v, want nil", result.Error)
	}
	if !result.Updated {
		t.Error("UpdateCurrentBranch() Updated = false, want true")
	}
	if result.AlreadyCurrent {
		t.Error("UpdateCurrentBranch() AlreadyCurrent = true, want false")
	}

	// Verify the new file exists in the clone
	if _, err := os.Stat(filepath.Join(cloneDir, "new.txt")); os.IsNotExist(err) {
		t.Error("Fast-forward did not bring new.txt into the working tree")
	}
}

func TestUpdateCurrentBranch_Diverged(t *testing.T) {
	// Create the "origin" repo
	originDir := createTestRepo(t)

	// Clone it
	cloneDir, err := os.MkdirTemp("", "git-update-diverged-*")
	if err != nil {
		t.Fatalf("Failed to create clone dir: %v", err)
	}
	defer os.RemoveAll(cloneDir)

	cloneCmd := exec.Command("git", "clone", originDir, cloneDir)
	if err := cloneCmd.Run(); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}
	if err := runGit(cloneDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email in clone: %v", err)
	}
	if err := runGit(cloneDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name in clone: %v", err)
	}

	// Add a commit to origin
	originFile := filepath.Join(originDir, "origin-change.txt")
	if err := os.WriteFile(originFile, []byte("origin"), 0644); err != nil {
		t.Fatalf("Failed to write file in origin: %v", err)
	}
	if err := runGit(originDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add in origin: %v", err)
	}
	if err := runGit(originDir, "commit", "-m", "origin commit"); err != nil {
		t.Fatalf("Failed to git commit in origin: %v", err)
	}

	// Add a different commit to clone (diverging)
	cloneFile := filepath.Join(cloneDir, "local-change.txt")
	if err := os.WriteFile(cloneFile, []byte("local"), 0644); err != nil {
		t.Fatalf("Failed to write file in clone: %v", err)
	}
	if err := runGit(cloneDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add in clone: %v", err)
	}
	if err := runGit(cloneDir, "commit", "-m", "local commit"); err != nil {
		t.Fatalf("Failed to git commit in clone: %v", err)
	}

	// Branches have diverged — --ff-only should fail gracefully
	ctx := context.Background()
	result := UpdateCurrentBranch(ctx, cloneDir)

	if result.Error == nil {
		t.Error("UpdateCurrentBranch() Error = nil, want error for diverged branches")
	}
	if result.Updated {
		t.Error("UpdateCurrentBranch() Updated = true, want false for diverged branches")
	}
	if result.BranchName == "" {
		t.Error("UpdateCurrentBranch() BranchName is empty, want branch name")
	}
}

// createTestRepo creates a temporary git repo with one commit and returns its path.
func createTestRepo(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	if err := runGit(tmpDir, "init"); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to set git email: %v", err)
	}
	if err := runGit(tmpDir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("Failed to set git name: %v", err)
	}
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := runGit(tmpDir, "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runGit(tmpDir, "commit", "-m", "initial"); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}
	return tmpDir
}

// runGit runs a git command in the specified directory
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
