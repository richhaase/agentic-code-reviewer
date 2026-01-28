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

// runGit runs a git command in the specified directory
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
