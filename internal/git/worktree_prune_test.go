package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneStaleWorktrees_NoWorktreesDir(t *testing.T) {
	// Should not error when .worktrees/ doesn't exist
	err := PruneStaleWorktrees()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneStaleWorktrees_SkipsNonReviewDirs(t *testing.T) {
	root, err := GetRoot()
	if err != nil {
		t.Skip("not in a git repo")
	}

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "my-custom-worktree")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Set old modification time
	oldTime := time.Now().Add(-24 * time.Hour)
	os.Chtimes(testDir, oldTime, oldTime)

	err = PruneStaleWorktrees()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the non-review directory was NOT removed
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees removed a non-review directory")
	}
}

func TestPruneStaleWorktrees_SkipsRecentReviewDirs(t *testing.T) {
	root, err := GetRoot()
	if err != nil {
		t.Skip("not in a git repo")
	}

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "review-test-recent")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Recent — should not be removed
	err = PruneStaleWorktrees()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees removed a recent review directory")
	}

	// Clean up
	os.RemoveAll(testDir)
}

func TestPruneStaleWorktrees_RemovesOldReviewDirs(t *testing.T) {
	root, err := GetRoot()
	if err != nil {
		t.Skip("not in a git repo")
	}

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "review-test-stale-abc123")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	// Set old modification time (older than staleWorktreeAge)
	oldTime := time.Now().Add(-3 * time.Hour)
	os.Chtimes(testDir, oldTime, oldTime)

	err = PruneStaleWorktrees()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the stale review directory was removed
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees did not remove stale review directory")
		os.RemoveAll(testDir) // cleanup
	}
}
