package desk

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

func testDeskPullRequestKey() store.PullRequestKeyV1 {
	return store.PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 198}
}

func testDeskReviewEvent(id string, key store.PullRequestKeyV1, eventType store.ReviewEventTypeV1, occurredAt time.Time, runID string) store.ReviewEventV1 {
	return store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            id,
		PullRequest:   key,
		Type:          eventType,
		OccurredAt:    occurredAt,
		RunID:         runID,
	}
}

func testDeskReviewRun(t *testing.T, id string, key store.PullRequestKeyV1, status domain.ReviewStatus, completedAt time.Time) store.ReviewRunV1 {
	t.Helper()
	pr := key.ToDomain()
	cfg, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         1,
		Concurrency:       1,
		Timeout:           time.Minute,
		ReviewerAgents:    []string{"codex"},
		SummarizerAgent:   "codex",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		FPThreshold:       75,
	})
	if err != nil {
		t.Fatalf("NewReviewConfiguration: %v", err)
	}
	run := domain.ReviewRun{
		ID:      id,
		Trigger: domain.ReviewTriggerDesk,
		Target: domain.ReviewTarget{
			RepositoryRoot: "/repo",
			WorktreeRoot:   "/repo",
			Revision: domain.RevisionEvidence{
				RequestedBaseRef: "main",
				ResolvedBaseRef:  "main",
				HeadObjectID:     "head-" + id,
				BaseObjectID:     "base",
			},
			PullRequest: &pr,
		},
		Engine:                   domain.ReviewEngine{Name: "acr", Version: "test"},
		StartedAt:                completedAt.Add(-time.Minute),
		CompletedAt:              completedAt,
		Configuration:            cfg,
		ConfigurationSource:      domain.ConfigurationSourceIdentity{Kind: "defaults"},
		ConfigurationFingerprint: cfg.Fingerprint(),
		Status:                   status,
	}
	switch status {
	case domain.ReviewStatusCompleted:
		run.Conclusion = domain.ReviewConclusionClean
	case domain.ReviewStatusFailed:
		run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: "all reviewers failed"}
	}
	schema, err := store.ToReviewRunSchema(run, store.RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	return schema
}

func testDeskAdjudication(id string, key store.PullRequestKeyV1, recordedAt time.Time, disposition store.AdjudicationDispositionV1, relation store.AdjudicationRelationV1, supersedes string) store.AdjudicationRecordV1 {
	return store.AdjudicationRecordV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            id,
		FindingRef:    store.AdjudicationFindingRefV1{FindingID: "finding-1"},
		Disposition:   disposition,
		DecidingActor: store.AdjudicationActorV1{Kind: store.AdjudicationActorHuman, Identity: "richhaase"},
		Rationale:     "test rationale",
		Scope: store.AdjudicationScopeV1{
			PullRequest:              key,
			HeadObjectID:             "headsha",
			ConfigurationFingerprint: "sha256:abc",
		},
		InvalidationConditions: []string{"head changes"},
		RecordedAt:             recordedAt,
		RelationToPrior:        relation,
		SupersedesRecordID:     supersedes,
	}
}

func testDeskLoopDecision(id string, key store.PullRequestKeyV1, decidedAt time.Time, decision store.LoopDecisionKindV1) store.LoopDecisionV1 {
	return store.LoopDecisionV1{
		SchemaVersion:  store.CurrentSchemaVersion,
		ID:             id,
		PullRequest:    key,
		RunID:          "run-1",
		Decision:       decision,
		Reason:         "test reason",
		IterationCount: 1,
		DecidedAt:      decidedAt,
	}
}

func testDeskEconomics(runID string) store.ReviewEconomicsV1 {
	return store.ReviewEconomicsV1{
		SchemaVersion:     store.CurrentSchemaVersion,
		RunID:             runID,
		ReviewerCallCount: 2,
		ModelCallCount:    3,
		Duration:          5 * time.Second,
		ProviderUsage: []store.ProviderUsageRecordV1{
			{Provider: "anthropic", Model: "claude-opus", Usage: store.ProviderUsageV1{Known: true, InputTokens: 500, TotalTokens: 600, CostUSD: 0.1}},
			{Provider: "codex", Model: "gpt", Usage: store.ProviderUsageV1{Known: false}},
		},
	}
}

func TestBuildTimeline_OrdersEntriesChronologicallyAcrossAllKinds(t *testing.T) {
	key := testDeskPullRequestKey()
	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	events := []store.ReviewEventV1{testDeskReviewEvent("event-1", key, store.EventTypePRDiscovered, base, "")}
	adjudications := []store.AdjudicationRecordV1{testDeskAdjudication("adjudication-1", key, base.Add(2*time.Minute), store.AdjudicationFalsePositive, store.AdjudicationRelationNone, "")}
	runs := []store.ReviewRunV1{}
	loopDecisions := []store.LoopDecisionV1{testDeskLoopDecision("loop-1", key, base.Add(4*time.Minute), store.LoopDecisionStop)}
	economics := []store.EconomicsRecordV1{{RecordedAt: base.Add(3 * time.Minute), Economics: testDeskEconomics("run-1")}}

	run := testDeskReviewRun(t, "run-1", key, domain.ReviewStatusCompleted, base.Add(1*time.Minute))
	runs = append(runs, run)

	entries := BuildTimeline(events, runs, adjudications, loopDecisions, economics)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	wantKinds := []TimelineEntryKind{
		TimelineEntryEvent,
		TimelineEntryRun,
		TimelineEntryAdjudication,
		TimelineEntryEconomics,
		TimelineEntryLoopDecision,
	}
	for i, want := range wantKinds {
		if entries[i].Kind != want {
			t.Fatalf("entry %d: expected kind %q, got %q (full: %+v)", i, want, entries[i].Kind, entries)
		}
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
			t.Fatalf("entries not chronologically ordered: %+v", entries)
		}
	}
}

func TestLoadHistory_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := testDeskPullRequestKey()
	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	firstProcessEvents := store.NewFilesystemEventStore(dir)
	firstProcessRuns := store.NewFilesystemRunStore(dir)
	firstProcessAdjudications := store.NewFilesystemAdjudicationStore(dir)
	firstProcessLoopDecisions := store.NewFilesystemLoopDecisionStore(dir)
	firstProcessEconomics := store.NewFilesystemEconomicsStore(dir)

	discovered := testDeskReviewEvent("event-discovered", key, store.EventTypePRDiscovered, base, "")
	if _, err := firstProcessEvents.AppendEvent(discovered); err != nil {
		t.Fatalf("append discovered event: %v", err)
	}
	queued := testDeskReviewEvent("event-queued", key, store.EventTypeReviewQueued, base.Add(time.Minute), "run-1")
	if _, err := firstProcessEvents.AppendEvent(queued); err != nil {
		t.Fatalf("append queued event: %v", err)
	}

	run := testDeskReviewRun(t, "run-1", key, domain.ReviewStatusCompleted, base.Add(2*time.Minute))
	if _, err := firstProcessRuns.SaveRun(run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	stale := testDeskReviewEvent("event-stale", key, store.EventTypeReviewStale, base.Add(3*time.Minute), "run-1")
	if _, err := firstProcessEvents.AppendEvent(stale); err != nil {
		t.Fatalf("append stale event: %v", err)
	}

	original := testDeskAdjudication("adjudication-1", key, base.Add(4*time.Minute), store.AdjudicationAcceptedRisk, store.AdjudicationRelationNone, "")
	if _, err := firstProcessAdjudications.SaveAdjudication(original); err != nil {
		t.Fatalf("save original adjudication: %v", err)
	}
	reopened := testDeskAdjudication("adjudication-2", key, base.Add(5*time.Minute), store.AdjudicationFalsePositive, store.AdjudicationRelationReopened, original.ID)
	if _, err := firstProcessAdjudications.SaveAdjudication(reopened); err != nil {
		t.Fatalf("save reopened adjudication: %v", err)
	}

	continueDecision := testDeskLoopDecision("loop-1", key, base.Add(6*time.Minute), store.LoopDecisionContinue)
	if _, err := firstProcessLoopDecisions.SaveLoopDecision(continueDecision); err != nil {
		t.Fatalf("save continue decision: %v", err)
	}
	stopDecision := testDeskLoopDecision("loop-2", key, base.Add(7*time.Minute), store.LoopDecisionStop)
	if _, err := firstProcessLoopDecisions.SaveLoopDecision(stopDecision); err != nil {
		t.Fatalf("save stop decision: %v", err)
	}

	economics := testDeskEconomics("run-1")
	if _, err := firstProcessEconomics.SaveEconomics(key, base.Add(8*time.Minute), economics); err != nil {
		t.Fatalf("save economics: %v", err)
	}

	history, err := LoadHistory(dir, key)
	if err != nil {
		t.Fatalf("load history after restart: %v", err)
	}
	if len(history.Corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %+v", history.Corrupt)
	}
	if len(history.Entries) != 9 {
		t.Fatalf("expected 9 timeline entries, got %d: %+v", len(history.Entries), history.Entries)
	}

	wantKinds := []TimelineEntryKind{
		TimelineEntryEvent,
		TimelineEntryEvent,
		TimelineEntryRun,
		TimelineEntryEvent,
		TimelineEntryAdjudication,
		TimelineEntryAdjudication,
		TimelineEntryLoopDecision,
		TimelineEntryLoopDecision,
		TimelineEntryEconomics,
	}
	for i, want := range wantKinds {
		if history.Entries[i].Kind != want {
			t.Fatalf("entry %d: expected kind %q, got %q", i, want, history.Entries[i].Kind)
		}
	}

	reopenedEntry := history.Entries[5].Adjudication
	if reopenedEntry.RelationToPrior != store.AdjudicationRelationReopened || reopenedEntry.SupersedesRecordID != "adjudication-1" {
		t.Fatalf("expected reopened adjudication to reference its prior, got %+v", reopenedEntry)
	}
	originalEntry := history.Entries[4].Adjudication
	if originalEntry.Disposition != store.AdjudicationAcceptedRisk || originalEntry.RelationToPrior != store.AdjudicationRelationNone {
		t.Fatalf("expected original adjudication to remain unmutated, got %+v", originalEntry)
	}

	secondLoad, err := LoadHistory(dir, key)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !reflect.DeepEqual(history.Entries, secondLoad.Entries) {
		t.Fatalf("expected a freshly constructed history to match the first load exactly:\nfirst:  %+v\nsecond: %+v", history.Entries, secondLoad.Entries)
	}
}

func TestLoadHistory_IsolatesCorruptRecordsAcrossKinds(t *testing.T) {
	dir := t.TempDir()
	key := testDeskPullRequestKey()
	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	run := testDeskReviewRun(t, "run-good", key, domain.ReviewStatusCompleted, base)
	if _, err := store.NewFilesystemRunStore(dir).SaveRun(run); err != nil {
		t.Fatalf("save good run: %v", err)
	}
	economics := testDeskEconomics("run-good")
	if _, err := store.NewFilesystemEconomicsStore(dir).SaveEconomics(key, base, economics); err != nil {
		t.Fatalf("save good economics: %v", err)
	}

	prDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, "198")
	corruptRunsDir := filepath.Join(prDir, "runs")
	if err := os.WriteFile(filepath.Join(corruptRunsDir, "20260722T130000.000000000Z-run-bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt run: %v", err)
	}
	corruptEconomicsDir := filepath.Join(prDir, "economics")
	if err := os.WriteFile(filepath.Join(corruptEconomicsDir, "20260722T130000.000000000Z-economics-bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt economics: %v", err)
	}

	history, err := LoadHistory(dir, key)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history.Entries) != 2 {
		t.Fatalf("expected the 2 healthy records to remain visible, got %d: %+v", len(history.Entries), history.Entries)
	}
	if len(history.Corrupt) != 2 {
		t.Fatalf("expected 2 corrupt records reported, got %d: %+v", len(history.Corrupt), history.Corrupt)
	}
}

func TestLoadHistory_RejectsInvalidKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadHistory(dir, store.PullRequestKeyV1{}); err == nil {
		t.Fatal("expected an error for an invalid pull request key")
	}
}

func TestLoadHistory_EmptyForUnknownPullRequest(t *testing.T) {
	dir := t.TempDir()
	key := testDeskPullRequestKey()

	history, err := LoadHistory(dir, key)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history.Entries) != 0 {
		t.Fatalf("expected no timeline entries, got %+v", history.Entries)
	}
	if len(history.Corrupt) != 0 {
		t.Fatalf("expected no corrupt records, got %+v", history.Corrupt)
	}
}
