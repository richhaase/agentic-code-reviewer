package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemSnapshotStore_SaveLoadOverwrite(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemSnapshotStore(dir)

	snapshot := validSnapshot()
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.LoadSnapshot(snapshot.PullRequest)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Title != snapshot.Title {
		t.Fatalf("unexpected loaded snapshot: %+v", loaded)
	}

	snapshot.Title = "Updated title"
	snapshot.CapturedAt = snapshot.CapturedAt.Add(time.Hour)
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	loaded, err = store.LoadSnapshot(snapshot.PullRequest)
	if err != nil {
		t.Fatalf("load after overwrite: %v", err)
	}
	if loaded.Title != "Updated title" {
		t.Fatalf("expected latest snapshot to win, got %q", loaded.Title)
	}
}

func TestFilesystemSnapshotStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	snapshot := validSnapshot()

	firstProcessStore := NewFilesystemSnapshotStore(dir)
	if err := firstProcessStore.SaveSnapshot(snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}

	secondProcessStore := NewFilesystemSnapshotStore(dir)
	loaded, err := secondProcessStore.LoadSnapshot(snapshot.PullRequest)
	if err != nil {
		t.Fatalf("load after restart: %v", err)
	}
	if loaded.HeadObjectID != snapshot.HeadObjectID {
		t.Fatalf("restart did not preserve snapshot data: got %+v", loaded)
	}
}

func TestFilesystemSnapshotStore_CorruptSnapshotReportedNotPanicked(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemSnapshotStore(dir)
	key := testPullRequestKey()

	path, err := snapshotFilePath(dir, key)
	if err != nil {
		t.Fatalf("snapshotFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed corrupt snapshot: %v", err)
	}

	if _, err := store.LoadSnapshot(key); err == nil {
		t.Fatal("expected an error loading a corrupt snapshot")
	}
}

func TestFilesystemSnapshotStore_LoadMissingReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemSnapshotStore(dir)

	if _, err := store.LoadSnapshot(testPullRequestKey()); err == nil {
		t.Fatal("expected an error loading a snapshot that was never saved")
	}
}

func TestPRSnapshotV1_Age(t *testing.T) {
	snapshot := validSnapshot()
	now := snapshot.CapturedAt.Add(5 * time.Minute)

	age := snapshot.Age(now)
	if age != 5*time.Minute {
		t.Fatalf("expected age of 5m, got %v", age)
	}

	if got := snapshot.Age(snapshot.CapturedAt.Add(-time.Minute)); got != 0 {
		t.Fatalf("expected non-negative age when now precedes captured_at, got %v", got)
	}

	var zero PRSnapshotV1
	if got := zero.Age(now); got != 0 {
		t.Fatalf("expected zero age for a snapshot with no captured_at, got %v", got)
	}
}
