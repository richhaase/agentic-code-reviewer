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
	_, err := GetGitDiff(ctx, "", "", false)
	if err == nil {
		t.Error("GetGitDiff() should return error for empty baseRef")
	}
	if !strings.Contains(err.Error(), "base ref cannot be empty") {
		t.Errorf("GetGitDiff() error = %v, want error containing 'base ref cannot be empty'", err)
	}
}

func TestGetGitDiff_InvalidBaseRef(t *testing.T) {
	ctx := context.Background()
	_, err := GetGitDiff(ctx, "-invalidref", "", false)
	if err == nil {
		t.Error("GetGitDiff() should return error for baseRef starting with -")
	}
	if !strings.Contains(err.Error(), "must not start with -") {
		t.Errorf("GetGitDiff() error = %v, want error containing 'must not start with -'", err)
	}
}

func TestGetGitDiff_FetchDisabled(t *testing.T) {
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

	// Get diff without fetch (fetchRemote=false)
	ctx := context.Background()
	diff, err := GetGitDiff(ctx, "HEAD", tmpDir, false)
	if err != nil {
		t.Fatalf("GetGitDiff() error = %v", err)
	}

	// Verify diff contains the change
	if !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+modified") {
		t.Errorf("GetGitDiff() diff doesn't contain expected changes: %s", diff)
	}
}

func TestGetGitDiff_FetchEnabled_FallbackOnFailure(t *testing.T) {
	// Create a temporary git repo for testing (no remote configured)
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

	// Get diff with fetch enabled (should fall back to local since no remote)
	ctx := context.Background()
	diff, err := GetGitDiff(ctx, "HEAD", tmpDir, true)
	if err != nil {
		t.Fatalf("GetGitDiff() error = %v", err)
	}

	// Verify diff contains the change (fallback to local worked)
	if !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+modified") {
		t.Errorf("GetGitDiff() diff doesn't contain expected changes: %s", diff)
	}
}

func TestGetGitDiff_FetchEnabled_AlreadyHasOriginPrefix(t *testing.T) {
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

	// Add a fake origin remote
	if err := runGit(tmpDir, "remote", "add", "origin", tmpDir); err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	// Fetch to create origin refs
	if err := runGit(tmpDir, "fetch", "origin"); err != nil {
		t.Fatalf("Failed to fetch: %v", err)
	}

	// Get current branch name (could be main or master depending on git config)
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = tmpDir
	branchOutput, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get current branch: %v", err)
	}
	branchName := strings.TrimSpace(string(branchOutput))

	// Modify file
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Get diff with baseRef already containing "origin/" prefix
	// Should NOT prepend another "origin/" (i.e., should not become "origin/origin/<branch>")
	ctx := context.Background()
	diff, err := GetGitDiff(ctx, "origin/"+branchName, tmpDir, true)
	if err != nil {
		t.Fatalf("GetGitDiff() error = %v", err)
	}

	// Verify diff contains the change
	if !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+modified") {
		t.Errorf("GetGitDiff() diff doesn't contain expected changes: %s", diff)
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
