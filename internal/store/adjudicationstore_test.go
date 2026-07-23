package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testAdjudicationRecord(id string, recordedAt time.Time) AdjudicationRecordV1 {
	return AdjudicationRecordV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            id,
		FindingRef:    AdjudicationFindingRefV1{FindingID: "finding-1"},
		Disposition:   AdjudicationAcceptedRisk,
		DecidingActor: AdjudicationActorV1{Kind: AdjudicationActorHuman, Identity: "richhaase"},
		Rationale:     "known limitation",
		Scope: AdjudicationScopeV1{
			PullRequest:              testPullRequestKey(),
			HeadObjectID:             "headsha",
			ConfigurationFingerprint: "sha256:abc",
		},
		RecordedAt: recordedAt,
	}
}

func TestFilesystemAdjudicationStore_SaveAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	key := testPullRequestKey()

	r1 := testAdjudicationRecord("adjudication-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	r2 := testAdjudicationRecord("adjudication-2", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))

	if _, err := store.SaveAdjudication(r1); err != nil {
		t.Fatalf("save r1: %v", err)
	}
	if _, err := store.SaveAdjudication(r2); err != nil {
		t.Fatalf("save r2: %v", err)
	}

	records, corrupt, err := store.ListAdjudications(key)
	if err != nil {
		t.Fatalf("list adjudications: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(records) != 2 || records[0].ID != "adjudication-1" || records[1].ID != "adjudication-2" {
		t.Fatalf("expected chronological adjudication-1, adjudication-2, got %+v", records)
	}
}

func TestFilesystemAdjudicationStore_RefusesDuplicateSave(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	record := testAdjudicationRecord("adjudication-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	if _, err := store.SaveAdjudication(record); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := store.SaveAdjudication(record); err == nil {
		t.Fatal("expected an error re-saving the same adjudication id/timestamp")
	}
}

func TestFilesystemAdjudicationStore_RefusesDuplicateIDWithDifferentTimestamp(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	key := testPullRequestKey()

	first := testAdjudicationRecord("adjudication-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveAdjudication(first); err != nil {
		t.Fatalf("first save: %v", err)
	}

	second := testAdjudicationRecord("adjudication-dup", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))
	if _, err := store.SaveAdjudication(second); err == nil {
		t.Fatal("expected an error re-saving the same adjudication id under a different timestamp")
	}

	records, corrupt, err := store.ListAdjudications(key)
	if err != nil {
		t.Fatalf("list adjudications: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly one stored record for the duplicated id, got %d", len(records))
	}
}

func TestFilesystemAdjudicationStore_RejectsInvalidRecord(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	invalid := testAdjudicationRecord("adjudication-invalid", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	invalid.Disposition = "wontfix"

	if _, err := store.SaveAdjudication(invalid); err == nil {
		t.Fatal("expected an error for an unknown disposition")
	}
}

func TestFilesystemAdjudicationStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	record := testAdjudicationRecord("adjudication-restart", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	firstProcessStore := NewFilesystemAdjudicationStore(dir)
	if _, err := firstProcessStore.SaveAdjudication(record); err != nil {
		t.Fatalf("save: %v", err)
	}

	secondProcessStore := NewFilesystemAdjudicationStore(dir)
	records, _, err := secondProcessStore.ListAdjudications(key)
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(records) != 1 || records[0].ID != "adjudication-restart" {
		t.Fatalf("restart did not preserve adjudication history: got %+v", records)
	}
}

func TestFilesystemAdjudicationStore_IsolatesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	key := testPullRequestKey()

	good := testAdjudicationRecord("adjudication-good", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveAdjudication(good); err != nil {
		t.Fatalf("save good record: %v", err)
	}

	dirPath, err := adjudicationsDir(dir, key)
	if err != nil {
		t.Fatalf("adjudicationsDir: %v", err)
	}
	corruptPath := filepath.Join(dirPath, "20260722T130000.000000000Z-adjudication-bad.json")
	if err := os.WriteFile(corruptPath, []byte(`not json at all`), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	records, corrupt, err := store.ListAdjudications(key)
	if err != nil {
		t.Fatalf("list adjudications: %v", err)
	}
	if len(records) != 1 || records[0].ID != "adjudication-good" {
		t.Fatalf("expected the good record to remain readable, got %+v", records)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}
}

func TestFilesystemAdjudicationStore_PreservesHistoryOnReopenCorrectSupersede(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAdjudicationStore(dir)
	key := testPullRequestKey()

	original := testAdjudicationRecord("adjudication-1", time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC))

	reopened := testAdjudicationRecord("adjudication-2", time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC))
	reopened.Disposition = AdjudicationDeferred
	reopened.RelationToPrior = AdjudicationRelationReopened
	reopened.SupersedesRecordID = original.ID

	corrected := testAdjudicationRecord("adjudication-3", time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC))
	corrected.Disposition = AdjudicationFalsePositive
	corrected.RelationToPrior = AdjudicationRelationCorrected
	corrected.SupersedesRecordID = reopened.ID

	for _, record := range []AdjudicationRecordV1{original, reopened, corrected} {
		if _, err := store.SaveAdjudication(record); err != nil {
			t.Fatalf("save %s: %v", record.ID, err)
		}
	}

	records, corrupt, err := store.ListAdjudications(key)
	if err != nil {
		t.Fatalf("list adjudications: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(records) != 3 {
		t.Fatalf("expected all 3 records preserved as separate history entries, got %d: %+v", len(records), records)
	}

	byID := make(map[string]AdjudicationRecordV1, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	restoredOriginal, ok := byID[original.ID]
	if !ok {
		t.Fatalf("original record %s missing from history", original.ID)
	}
	if restoredOriginal.Disposition != AdjudicationAcceptedRisk || restoredOriginal.RelationToPrior != AdjudicationRelationNone {
		t.Fatalf("original record was mutated by later reopen/correct events: %+v", restoredOriginal)
	}
}
