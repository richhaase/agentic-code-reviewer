package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListTrackedPullRequests_EmptyDataDir(t *testing.T) {
	dir := t.TempDir()
	keys, err := ListTrackedPullRequests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no tracked pull requests, got %+v", keys)
	}
}

func TestListTrackedPullRequests_FindsSavedSnapshots(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemSnapshotStore(dir)

	first := validSnapshot()
	if err := store.SaveSnapshot(first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	second := validSnapshot()
	second.PullRequest.Number = 5
	second.PullRequest.Owner = "other-owner"
	if err := store.SaveSnapshot(second); err != nil {
		t.Fatalf("save second: %v", err)
	}

	keys, err := ListTrackedPullRequests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 tracked pull requests, got %+v", keys)
	}
	if keys[0].Owner != "other-owner" || keys[1] != first.PullRequest {
		t.Errorf("unexpected sort order or keys: %+v", keys)
	}
}

func TestListTrackedPullRequests_SkipsMalformedEntries(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, prsDirName, "github.com", "owner", "repo", "not-a-number")
	if err := os.MkdirAll(malformed, 0o755); err != nil {
		t.Fatal(err)
	}

	store := NewFilesystemSnapshotStore(dir)
	valid := validSnapshot()
	if err := store.SaveSnapshot(valid); err != nil {
		t.Fatalf("save: %v", err)
	}

	keys, err := ListTrackedPullRequests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 || keys[0] != valid.PullRequest {
		t.Fatalf("expected only the valid entry, got %+v", keys)
	}
}

func TestListTrackedPullRequests_MissingDataDirIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	keys, err := ListTrackedPullRequests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no tracked pull requests, got %+v", keys)
	}
}
