package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testReviewEconomics(runID string) ReviewEconomicsV1 {
	return ReviewEconomicsV1{
		SchemaVersion:     CurrentSchemaVersion,
		RunID:             runID,
		ReviewerCallCount: 3,
		ModelCallCount:    5,
		Duration:          12 * time.Second,
		ProviderUsage: []ProviderUsageRecordV1{
			{
				Provider: "anthropic",
				Model:    "claude-opus",
				Usage:    ProviderUsageV1{Known: true, InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200, CostUSD: 0.42},
			},
			{
				Provider: "codex",
				Model:    "gpt",
				Usage:    ProviderUsageV1{Known: false},
			},
		},
	}
}

func TestFilesystemEconomicsStore_SaveAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()

	e1 := testReviewEconomics("run-1")
	e2 := testReviewEconomics("run-2")

	if _, err := store.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), e1); err != nil {
		t.Fatalf("save e1: %v", err)
	}
	if _, err := store.SaveEconomics(key, time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC), e2); err != nil {
		t.Fatalf("save e2: %v", err)
	}

	records, corrupt, err := store.ListEconomics(key)
	if err != nil {
		t.Fatalf("list economics: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(records) != 2 || records[0].Economics.RunID != "run-1" || records[1].Economics.RunID != "run-2" {
		t.Fatalf("expected chronological run-1, run-2, got %+v", records)
	}
	if !records[0].RecordedAt.Equal(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected record 1 recorded_at to round-trip, got %v", records[0].RecordedAt)
	}
	if !records[1].RecordedAt.Equal(time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected record 2 recorded_at to round-trip, got %v", records[1].RecordedAt)
	}
}

func TestFilesystemEconomicsStore_PreservesUnknownUsageThroughRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()

	economics := testReviewEconomics("run-unknown-usage")
	if _, err := store.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), economics); err != nil {
		t.Fatalf("save: %v", err)
	}

	records, _, err := store.ListEconomics(key)
	if err != nil {
		t.Fatalf("list economics: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	loaded := records[0].Economics
	if len(loaded.ProviderUsage) != 2 {
		t.Fatalf("expected 2 provider usage records, got %d", len(loaded.ProviderUsage))
	}
	unknown := loaded.ProviderUsage[1]
	if unknown.Usage.Known {
		t.Fatalf("expected codex usage to remain marked unknown after round trip, got %+v", unknown.Usage)
	}
	if unknown.Usage.InputTokens != 0 || unknown.Usage.CostUSD != 0 {
		t.Fatalf("unknown usage must not carry nonzero measurements, got %+v", unknown.Usage)
	}
	known := loaded.ProviderUsage[0]
	if !known.Usage.Known || known.Usage.InputTokens != 1000 {
		t.Fatalf("expected anthropic usage to remain known with its measurements intact, got %+v", known.Usage)
	}
}

func TestFilesystemEconomicsStore_RefusesDuplicateSave(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()
	economics := testReviewEconomics("run-dup")
	recordedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	if _, err := store.SaveEconomics(key, recordedAt, economics); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := store.SaveEconomics(key, recordedAt, economics); err == nil {
		t.Fatal("expected an error re-saving the same run id/timestamp")
	}
}

func TestFilesystemEconomicsStore_RejectsZeroRecordedAt(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()

	if _, err := store.SaveEconomics(key, time.Time{}, testReviewEconomics("run-no-time")); err == nil {
		t.Fatal("expected an error for a zero recorded_at")
	}
}

func TestFilesystemEconomicsStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	economics := testReviewEconomics("run-restart")

	firstProcessStore := NewFilesystemEconomicsStore(dir)
	if _, err := firstProcessStore.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), economics); err != nil {
		t.Fatalf("save: %v", err)
	}

	secondProcessStore := NewFilesystemEconomicsStore(dir)
	records, _, err := secondProcessStore.ListEconomics(key)
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(records) != 1 || records[0].Economics.RunID != "run-restart" {
		t.Fatalf("restart did not preserve economics history: got %+v", records)
	}
}

func TestFilesystemEconomicsStore_IsolatesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()

	good := testReviewEconomics("run-good")
	if _, err := store.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), good); err != nil {
		t.Fatalf("save good record: %v", err)
	}

	dirPath, err := economicsDir(dir, key)
	if err != nil {
		t.Fatalf("economicsDir: %v", err)
	}
	corruptPath := filepath.Join(dirPath, "20260722T130000.000000000Z-run-bad.json")
	if err := os.WriteFile(corruptPath, []byte(`not json at all`), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	records, corrupt, err := store.ListEconomics(key)
	if err != nil {
		t.Fatalf("list economics: %v", err)
	}
	if len(records) != 1 || records[0].Economics.RunID != "run-good" {
		t.Fatalf("expected the good record to remain readable, got %+v", records)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}
}

func TestFilesystemEconomicsStore_IsolatesRecordsWithUnparsableTimestampFilenames(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemEconomicsStore(dir)
	key := testPullRequestKey()

	good := testReviewEconomics("run-good")
	if _, err := store.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), good); err != nil {
		t.Fatalf("save good record: %v", err)
	}

	dirPath, err := economicsDir(dir, key)
	if err != nil {
		t.Fatalf("economicsDir: %v", err)
	}
	corruptPath := filepath.Join(dirPath, "not-a-timestamp-run-bad.json")
	data, err := os.ReadFile(filepath.Join(dirPath, mustSingleFileName(t, dirPath)))
	if err != nil {
		t.Fatalf("read seeded good record: %v", err)
	}
	if err := os.WriteFile(corruptPath, data, 0o644); err != nil {
		t.Fatalf("seed record with unparsable timestamp filename: %v", err)
	}

	records, corrupt, err := store.ListEconomics(key)
	if err != nil {
		t.Fatalf("list economics: %v", err)
	}
	if len(records) != 1 || records[0].Economics.RunID != "run-good" {
		t.Fatalf("expected the good record to remain readable, got %+v", records)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}
}

func mustSingleFileName(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one file in %s, got %d", dir, len(entries))
	}
	return entries[0].Name()
}
