package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testReviewEvent(id string, occurredAt time.Time) ReviewEventV1 {
	return ReviewEventV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            id,
		PullRequest:   testPullRequestKey(),
		Type:          EventTypePRDiscovered,
		OccurredAt:    occurredAt,
	}
}

func TestFilesystemEventStore_AppendAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEventStore(dir)
	key := testPullRequestKey()

	e1 := testReviewEvent("event-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	e2 := testReviewEvent("event-2", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))

	if _, err := store.AppendEvent(e1); err != nil {
		t.Fatalf("append e1: %v", err)
	}
	if _, err := store.AppendEvent(e2); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	events, corrupt, err := store.ListEvents(key)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt events, got %v", corrupt)
	}
	if len(events) != 2 || events[0].ID != "event-1" || events[1].ID != "event-2" {
		t.Fatalf("expected chronological event-1, event-2, got %+v", events)
	}
}

func TestFilesystemEventStore_RejectsInvalidEvent(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEventStore(dir)

	invalid := testReviewEvent("event-invalid", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	invalid.Type = EventTypeReviewCompleted

	if _, err := store.AppendEvent(invalid); err == nil {
		t.Fatal("expected an error for a review_completed event with no run_id")
	}
}

func TestFilesystemEventStore_RefusesDuplicateAppend(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEventStore(dir)
	event := testReviewEvent("event-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	if _, err := store.AppendEvent(event); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if _, err := store.AppendEvent(event); err == nil {
		t.Fatal("expected an error re-appending the same event id/timestamp")
	}
}

func TestFilesystemEventStore_RefusesDuplicateIDWithDifferentTimestamp(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEventStore(dir)
	key := testPullRequestKey()

	first := testReviewEvent("event-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.AppendEvent(first); err != nil {
		t.Fatalf("first append: %v", err)
	}

	second := testReviewEvent("event-dup", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))
	if _, err := store.AppendEvent(second); err == nil {
		t.Fatal("expected an error re-appending the same event id under a different timestamp")
	}

	events, corrupt, err := store.ListEvents(key)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt events, got %v", corrupt)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one stored event for the duplicated id, got %d", len(events))
	}
}

func TestFilesystemEventStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	event := testReviewEvent("event-restart", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	firstProcessStore := NewFilesystemEventStore(dir)
	if _, err := firstProcessStore.AppendEvent(event); err != nil {
		t.Fatalf("append: %v", err)
	}

	secondProcessStore := NewFilesystemEventStore(dir)
	events, _, err := secondProcessStore.ListEvents(key)
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-restart" {
		t.Fatalf("restart did not preserve event history: got %+v", events)
	}
}

func TestFilesystemEventStore_IsolatesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEventStore(dir)
	key := testPullRequestKey()

	good := testReviewEvent("event-good", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.AppendEvent(good); err != nil {
		t.Fatalf("append good event: %v", err)
	}

	eventsDirPath, err := eventsDir(dir, key)
	if err != nil {
		t.Fatalf("eventsDir: %v", err)
	}
	corruptPath := filepath.Join(eventsDirPath, "20260722T130000.000000000Z-event-bad.json")
	if err := os.WriteFile(corruptPath, []byte(`not json at all`), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	events, corrupt, err := store.ListEvents(key)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-good" {
		t.Fatalf("expected the good event to remain readable, got %+v", events)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}
}
