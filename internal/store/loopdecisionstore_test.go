package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testLoopDecision(id string, decidedAt time.Time) LoopDecisionV1 {
	return LoopDecisionV1{
		SchemaVersion:  CurrentSchemaVersion,
		ID:             id,
		PullRequest:    testPullRequestKey(),
		RunID:          "run-1",
		Decision:       LoopDecisionContinue,
		Reason:         "not converged",
		IterationCount: 1,
		DecidedAt:      decidedAt,
	}
}

func TestFilesystemLoopDecisionStore_SaveAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemLoopDecisionStore(dir)
	key := testPullRequestKey()

	d1 := testLoopDecision("loop-decision-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	d2 := testLoopDecision("loop-decision-2", time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC))

	if _, err := store.SaveLoopDecision(d1); err != nil {
		t.Fatalf("save d1: %v", err)
	}
	if _, err := store.SaveLoopDecision(d2); err != nil {
		t.Fatalf("save d2: %v", err)
	}

	decisions, corrupt, err := store.ListLoopDecisions(key)
	if err != nil {
		t.Fatalf("list loop decisions: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %v", corrupt)
	}
	if len(decisions) != 2 || decisions[0].ID != "loop-decision-1" || decisions[1].ID != "loop-decision-2" {
		t.Fatalf("expected chronological loop-decision-1, loop-decision-2, got %+v", decisions)
	}
}

func TestFilesystemLoopDecisionStore_RefusesDuplicateSave(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemLoopDecisionStore(dir)
	decision := testLoopDecision("loop-decision-dup", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	if _, err := store.SaveLoopDecision(decision); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := store.SaveLoopDecision(decision); err == nil {
		t.Fatal("expected an error re-saving the same loop decision id/timestamp")
	}
}

func TestFilesystemLoopDecisionStore_RejectsInvalidDecision(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemLoopDecisionStore(dir)
	invalid := testLoopDecision("loop-decision-invalid", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	invalid.Reason = ""

	if _, err := store.SaveLoopDecision(invalid); err == nil {
		t.Fatal("expected an error for a missing reason")
	}
}

func TestFilesystemLoopDecisionStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	decision := testLoopDecision("loop-decision-restart", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))

	firstProcessStore := NewFilesystemLoopDecisionStore(dir)
	if _, err := firstProcessStore.SaveLoopDecision(decision); err != nil {
		t.Fatalf("save: %v", err)
	}

	secondProcessStore := NewFilesystemLoopDecisionStore(dir)
	decisions, _, err := secondProcessStore.ListLoopDecisions(key)
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ID != "loop-decision-restart" {
		t.Fatalf("restart did not preserve loop decision history: got %+v", decisions)
	}
}

func TestFilesystemLoopDecisionStore_IsolatesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemLoopDecisionStore(dir)
	key := testPullRequestKey()

	good := testLoopDecision("loop-decision-good", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := store.SaveLoopDecision(good); err != nil {
		t.Fatalf("save good decision: %v", err)
	}

	dirPath, err := loopDecisionsDir(dir, key)
	if err != nil {
		t.Fatalf("loopDecisionsDir: %v", err)
	}
	corruptPath := filepath.Join(dirPath, "20260722T130000.000000000Z-loop-decision-bad.json")
	if err := os.WriteFile(corruptPath, []byte(`not json at all`), 0o644); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	decisions, corrupt, err := store.ListLoopDecisions(key)
	if err != nil {
		t.Fatalf("list loop decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ID != "loop-decision-good" {
		t.Fatalf("expected the good decision to remain readable, got %+v", decisions)
	}
	if len(corrupt) != 1 || corrupt[0].Path != corruptPath {
		t.Fatalf("expected exactly one corrupt record reported for %s, got %+v", corruptPath, corrupt)
	}
}
