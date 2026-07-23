package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func buildTestReviewRunSchema(t *testing.T, id string, completedAt time.Time) ReviewRunV1 {
	t.Helper()
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	run.ID = id
	run.CompletedAt = completedAt
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{ReviewBody: "body for " + id})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	return schema
}

func TestFilesystemRunStore_SaveListLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	key := testPullRequestKey()

	run1 := buildTestReviewRunSchema(t, "run-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	run2 := buildTestReviewRunSchema(t, "run-2", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))

	if _, err := store.SaveRun(run1); err != nil {
		t.Fatalf("save run1: %v", err)
	}
	if _, err := store.SaveRun(run2); err != nil {
		t.Fatalf("save run2: %v", err)
	}

	runs, corrupt, err := store.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID != "run-1" || runs[1].ID != "run-2" {
		t.Fatalf("expected chronological order run-1, run-2, got %s, %s", runs[0].ID, runs[1].ID)
	}

	loaded, err := store.LoadRun(key, "run-2")
	if err != nil {
		t.Fatalf("load run2: %v", err)
	}
	if loaded.Rendered.ReviewBody != "body for run-2" {
		t.Fatalf("unexpected loaded run body: %q", loaded.Rendered.ReviewBody)
	}
}

func TestFilesystemRunStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	run := buildTestReviewRunSchema(t, "run-restart", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	firstProcessStore := NewFilesystemRunStore(dir)
	if _, err := firstProcessStore.SaveRun(run); err != nil {
		t.Fatalf("save: %v", err)
	}

	secondProcessStore := NewFilesystemRunStore(dir)
	loaded, err := secondProcessStore.LoadRun(key, "run-restart")
	if err != nil {
		t.Fatalf("load after restart: %v", err)
	}
	if loaded.ID != run.ID || loaded.ConfigurationFingerprint != run.ConfigurationFingerprint {
		t.Fatalf("restart did not preserve run data: got %+v", loaded)
	}
}

func TestFilesystemRunStore_RefusesDuplicateSave(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	run := buildTestReviewRunSchema(t, "run-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	if _, err := store.SaveRun(run); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := store.SaveRun(run); err == nil {
		t.Fatal("expected second save of the same run id/timestamp to fail")
	}
}

func TestFilesystemRunStore_RefusesDuplicateRunIDWithDifferentTimestamp(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	key := testPullRequestKey()

	first := buildTestReviewRunSchema(t, "run-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveRun(first); err != nil {
		t.Fatalf("first save: %v", err)
	}

	second := buildTestReviewRunSchema(t, "run-dup", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))
	if _, err := store.SaveRun(second); err == nil {
		t.Fatal("expected a second save of the same run id under a different timestamp to fail")
	}

	runs, corrupt, err := store.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one stored run for the duplicated id, got %d", len(runs))
	}
}

func TestFilesystemRunStore_IsolatesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	key := testPullRequestKey()

	good := buildTestReviewRunSchema(t, "run-good", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveRun(good); err != nil {
		t.Fatalf("save good run: %v", err)
	}

	runsDirPath, err := runsDir(dir, key)
	if err != nil {
		t.Fatalf("runsDir: %v", err)
	}
	corruptPath := filepath.Join(runsDirPath, "20260722T130000.000000000Z-run-bad.json")
	if err := os.WriteFile(corruptPath, []byte(`{"schema_version": 1, "id": "run-bad", "truncated`), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	runs, corrupt, err := store.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-good" {
		t.Fatalf("expected the good run to remain readable, got %+v", runs)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}

	loaded, err := store.LoadRun(key, "run-good")
	if err != nil {
		t.Fatalf("load good run despite corrupt sibling: %v", err)
	}
	if loaded.ID != "run-good" {
		t.Fatalf("unexpected loaded run: %+v", loaded)
	}
}

func TestFilesystemRunStore_IgnoresStrayTempFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	key := testPullRequestKey()

	good := buildTestReviewRunSchema(t, "run-good", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveRun(good); err != nil {
		t.Fatalf("save good run: %v", err)
	}

	runsDirPath, err := runsDir(dir, key)
	if err != nil {
		t.Fatalf("runsDir: %v", err)
	}
	strayPath := filepath.Join(runsDirPath, ".tmp-20260722T130000.000000000Z-run-killed.json-xyz")
	if err := os.WriteFile(strayPath, []byte(`{"schema_vers`), 0o644); err != nil {
		t.Fatalf("seed stray temp file: %v", err)
	}

	runs, corrupt, err := store.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-good" {
		t.Fatalf("expected only the good run, got %+v", runs)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected the stray temp file to be silently ignored, got %+v", corrupt)
	}
}

func TestFilesystemRunStore_LoadRunNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	key := testPullRequestKey()

	if _, err := store.LoadRun(key, "missing"); err == nil {
		t.Fatal("expected an error for a missing run")
	}
}

func TestFilesystemRunStore_SaveRunRejectsInvalidRecord(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)
	schema := buildTestReviewRunSchema(t, "run-invalid", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	schema.ConfigurationFingerprint = "sha256:corrupted-before-save"

	if _, err := store.SaveRun(schema); err == nil {
		t.Fatal("expected SaveRun to reject a record ListRuns would later consider corrupt")
	}

	key := testPullRequestKey()
	runs, corrupt, err := store.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 0 || len(corrupt) != 0 {
		t.Fatalf("expected nothing to have been written for a rejected save, got runs=%v corrupt=%v", runs, corrupt)
	}
}

func TestFilesystemRunStore_SaveRunRequiresPullRequestTarget(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemRunStore(dir)

	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	run.Target.PullRequest = nil
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}

	if _, err := store.SaveRun(schema); err == nil {
		t.Fatal("expected an error saving a run with no pull request target")
	}
}
