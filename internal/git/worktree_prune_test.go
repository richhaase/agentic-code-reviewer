package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneStaleWorktrees_NoWorktreesDir(t *testing.T) {
	root := setupTestRepo(t)

	err := PruneStaleWorktrees(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneStaleWorktrees_SkipsNonReviewDirs(t *testing.T) {
	root := setupTestRepo(t)

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "my-custom-worktree")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	oldTime := time.Now().Add(-24 * time.Hour)
	os.Chtimes(testDir, oldTime, oldTime)

	err := PruneStaleWorktrees(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees removed a non-review directory")
	}
}

func TestPruneStaleWorktrees_SkipsRecentReviewDirs(t *testing.T) {
	root := setupTestRepo(t)

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "review-test-recent")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	err := PruneStaleWorktrees(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees removed a recent review directory")
	}
}

func TestPruneStaleWorktrees_RemovesOldReviewDirs(t *testing.T) {
	root := setupTestRepo(t)

	worktreesDir := filepath.Join(root, ".worktrees")
	testDir := filepath.Join(worktreesDir, "review-test-stale-abc123")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	oldTime := time.Now().Add(-3 * time.Hour)
	os.Chtimes(testDir, oldTime, oldTime)

	err := PruneStaleWorktrees(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("PruneStaleWorktrees did not remove stale review directory")
	}
}
