package store

import (
	"os"
	"testing"
	"time"
)

func testOtherPullRequestKey() PullRequestKeyV1 {
	key := testPullRequestKey()
	key.Number = key.Number + 1
	return key
}

func testPRSnapshot(key PullRequestKeyV1) PRSnapshotV1 {
	return PRSnapshotV1{
		SchemaVersion: CurrentSchemaVersion,
		PullRequest:   key,
		URL:           "https://github.com/richhaase/agentic-code-reviewer/pull/196",
		Title:         "Test snapshot",
		Author:        "richhaase",
		State:         PullRequestStateOpen,
		HeadObjectID:  "headsha",
		BaseObjectID:  "basesha",
		CapturedAt:    time.Date(2026, 7, 22, 9, 5, 0, 0, time.UTC),
	}
}

func TestForgetPullRequest_RemovesExactlyTheRequestedHistory(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()
	other := testOtherPullRequestKey()

	runStore := NewFilesystemRunStore(dir)
	eventStore := NewFilesystemEventStore(dir)
	adjudicationStore := NewFilesystemAdjudicationStore(dir)
	loopDecisionStore := NewFilesystemLoopDecisionStore(dir)
	economicsStore := NewFilesystemEconomicsStore(dir)
	snapshotStore := NewFilesystemSnapshotStore(dir)

	run := buildTestReviewRunSchema(t, "run-forget-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := runStore.SaveRun(run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	event := testReviewEvent("event-forget-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := eventStore.AppendEvent(event); err != nil {
		t.Fatalf("append event: %v", err)
	}
	adjudication := testAdjudicationRecord("adjudication-forget-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := adjudicationStore.SaveAdjudication(adjudication); err != nil {
		t.Fatalf("save adjudication: %v", err)
	}
	decision := testLoopDecision("loop-decision-forget-1", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := loopDecisionStore.SaveLoopDecision(decision); err != nil {
		t.Fatalf("save loop decision: %v", err)
	}
	economics := testReviewEconomics("run-forget-1")
	if _, err := economicsStore.SaveEconomics(key, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC), economics); err != nil {
		t.Fatalf("save economics: %v", err)
	}
	if err := snapshotStore.SaveSnapshot(testPRSnapshot(key)); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	otherRun := buildTestReviewRunSchema(t, "run-forget-other", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	otherRun.Target.PullRequest = &other
	if _, err := runStore.SaveRun(otherRun); err != nil {
		t.Fatalf("save other run: %v", err)
	}

	report, err := ForgetPullRequest(dir, key)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !report.Existed {
		t.Fatal("expected report to indicate the pull request history existed")
	}
	if !report.SnapshotRemoved {
		t.Fatal("expected the snapshot to be reported as removed")
	}
	if report.RunsRemoved != 1 || report.EventsRemoved != 1 || report.AdjudicationsRemoved != 1 ||
		report.LoopDecisionsRemoved != 1 || report.EconomicsRemoved != 1 {
		t.Fatalf("unexpected removal counts: %+v", report)
	}

	runs, _, err := runStore.ListRuns(key)
	if err != nil {
		t.Fatalf("list runs after forget: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs remaining for forgotten pull request, got %+v", runs)
	}
	events, _, err := eventStore.ListEvents(key)
	if err != nil {
		t.Fatalf("list events after forget: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events remaining for forgotten pull request, got %+v", events)
	}
	if _, err := snapshotStore.LoadSnapshot(key); err == nil {
		t.Fatal("expected an error loading the snapshot of a forgotten pull request")
	}

	otherRuns, _, err := runStore.ListRuns(other)
	if err != nil {
		t.Fatalf("list other runs: %v", err)
	}
	if len(otherRuns) != 1 || otherRuns[0].ID != "run-forget-other" {
		t.Fatalf("expected the other pull request's history to survive forget, got %+v", otherRuns)
	}
}

func TestForgetPullRequest_NoStoredHistoryIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()

	report, err := ForgetPullRequest(dir, key)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if report.Existed {
		t.Fatalf("expected Existed to be false for a pull request with no stored history, got %+v", report)
	}
	if report.RunsRemoved != 0 || report.EventsRemoved != 0 {
		t.Fatalf("expected zero removal counts, got %+v", report)
	}
}

func TestForgetPullRequest_RejectsInvalidKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := ForgetPullRequest(dir, PullRequestKeyV1{}); err == nil {
		t.Fatal("expected an error for an invalid pull request key")
	}
}

func TestForgetPullRequest_LeavesDataDirIntact(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()

	eventStore := NewFilesystemEventStore(dir)
	event := testReviewEvent("event-forget-2", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	if _, err := eventStore.AppendEvent(event); err != nil {
		t.Fatalf("append event: %v", err)
	}

	if _, err := ForgetPullRequest(dir, key); err != nil {
		t.Fatalf("forget: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected the data directory itself to remain, got %v", err)
	}
}
