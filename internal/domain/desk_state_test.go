package domain

import (
	"testing"
	"time"
)

var baseNow = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func baseInput() ClassificationInput {
	return ClassificationInput{
		Snapshot: PullRequestSnapshot{
			PullRequest:  PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 1},
			URL:          "https://github.com/o/r/pull/1",
			Author:       "someone-else",
			State:        PullRequestStateOpen,
			HeadObjectID: "head-1",
			BaseObjectID: "base-1",
			UpdatedAt:    baseNow.Add(-time.Hour),
			CapturedAt:   baseNow,
		},
		RepositoryAvailable: true,
		ExpectedUser:        "me",
		ReviewOwnPRs:        false,
		SettleTime:          10 * time.Minute,
		Now:                 baseNow,
	}
}

func completedRun(head string, conclusion ReviewConclusion, findings []ReviewFinding) ReviewRun {
	return ReviewRun{
		ID:          "run-" + head,
		Target:      ReviewTarget{Revision: RevisionEvidence{HeadObjectID: head}},
		Status:      ReviewStatusCompleted,
		Conclusion:  conclusion,
		CompletedAt: baseNow.Add(-2 * time.Hour),
		Findings:    findings,
	}
}

func TestClassify_ReleasedTakesPrecedenceOverEverything(t *testing.T) {
	input := baseInput()
	input.Snapshot.State = PullRequestStateClosed
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventUserReleased, OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.Tracked {
		t.Fatalf("expected released PR to be untracked, got %+v", got)
	}
}

func TestClassify_ResumeReversesRelease(t *testing.T) {
	input := baseInput()
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventUserReleased, OccurredAt: baseNow.Add(-2 * time.Minute)},
		{Kind: LifecycleEventUserResumed, OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if !got.Tracked {
		t.Fatalf("expected resumed PR to be tracked again, got %+v", got)
	}
	if got.State != DeskStateNeedsReview {
		t.Errorf("expected needs_review after resume with no run history, got %q", got.State)
	}
}

func TestClassify_ClosedOrMergedIsResolved(t *testing.T) {
	for _, state := range []PullRequestState{PullRequestStateClosed, PullRequestStateMerged} {
		input := baseInput()
		input.Snapshot.State = state
		got := Classify(input)
		if got.State != DeskStateResolved || !got.Tracked {
			t.Errorf("state %q: expected resolved+tracked, got %+v", state, got)
		}
	}
}

func TestClassify_RepositoryUnavailable(t *testing.T) {
	input := baseInput()
	input.RepositoryAvailable = false

	got := Classify(input)
	if got.State != DeskStateRepositoryUnavailable {
		t.Errorf("expected repository_unavailable, got %q", got.State)
	}
}

func TestClassify_ClosedOrMergedBeatsRepositoryUnavailable(t *testing.T) {
	input := baseInput()
	input.Snapshot.State = PullRequestStateMerged
	input.RepositoryAvailable = false

	got := Classify(input)
	if got.State != DeskStateResolved {
		t.Errorf("expected a merged PR to report resolved even when the repo is unavailable, got %q", got.State)
	}
}

func TestClassify_OwnPRDisabledDefaultsToWaitingOnOthers(t *testing.T) {
	input := baseInput()
	input.Snapshot.Author = "me"
	input.ReviewOwnPRs = false

	got := Classify(input)
	if got.State != DeskStateWaitingOnOthers {
		t.Errorf("expected waiting_on_others for own PR with review disabled, got %q", got.State)
	}
}

func TestClassify_OwnPRDisabledIgnoresRunHistory(t *testing.T) {
	input := baseInput()
	input.Snapshot.Author = "me"
	input.ReviewOwnPRs = false
	run := completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}})
	input.Runs = []ReviewRun{run}

	got := Classify(input)
	if got.State != DeskStateWaitingOnOthers {
		t.Errorf("expected own-PR default to override run history, got %q", got.State)
	}
}

func TestClassify_OwnPRReviewEnabledUsesNormalPipeline(t *testing.T) {
	input := baseInput()
	input.Snapshot.Author = "me"
	input.ReviewOwnPRs = true

	got := Classify(input)
	if got.State != DeskStateNeedsReview {
		t.Errorf("expected needs_review for own PR when review_own_prs is enabled, got %q", got.State)
	}
}

func TestClassify_NeedsReview_NoRunHistory(t *testing.T) {
	input := baseInput()
	got := Classify(input)
	if got.State != DeskStateNeedsReview {
		t.Errorf("expected needs_review, got %q", got.State)
	}
}

func TestClassify_Queued(t *testing.T) {
	input := baseInput()
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventQueued, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-time.Minute)},
	}
	got := Classify(input)
	if got.State != DeskStateQueued {
		t.Errorf("expected queued, got %q", got.State)
	}
}

func TestClassify_Running(t *testing.T) {
	input := baseInput()
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventQueued, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-2 * time.Minute)},
		{Kind: LifecycleEventStarted, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-time.Minute)},
	}
	got := Classify(input)
	if got.State != DeskStateRunning {
		t.Errorf("expected running, got %q", got.State)
	}
}

func TestClassify_TerminatedRunDoesNotReportQueuedOrRunning(t *testing.T) {
	input := baseInput()
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventQueued, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-3 * time.Minute)},
		{Kind: LifecycleEventStarted, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-2 * time.Minute)},
		{Kind: LifecycleEventCompleted, RunID: "run-a", HeadObjectID: "head-1", OccurredAt: baseNow.Add(-time.Minute)},
	}
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}

	got := Classify(input)
	if got.State == DeskStateQueued || got.State == DeskStateRunning {
		t.Errorf("expected a terminated run to not report queued/running, got %q", got.State)
	}
}

func TestClassify_Failed(t *testing.T) {
	input := baseInput()
	run := completedRun("head-1", ReviewConclusionNone, nil)
	run.Status = ReviewStatusFailed
	input.Runs = []ReviewRun{run}

	got := Classify(input)
	if got.State != DeskStateFailed {
		t.Errorf("expected failed, got %q", got.State)
	}
}

func TestClassify_InterruptedNeedsReviewAgain(t *testing.T) {
	input := baseInput()
	run := completedRun("head-1", ReviewConclusionNone, nil)
	run.Status = ReviewStatusInterrupted
	input.Runs = []ReviewRun{run}

	got := Classify(input)
	if got.State != DeskStateNeedsReview {
		t.Errorf("expected an interrupted review to need review again, got %q", got.State)
	}
}

func TestClassify_FindingsReady(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}, {ID: "f2"}})}

	got := Classify(input)
	if got.State != DeskStateFindingsReady {
		t.Errorf("expected findings_ready, got %q", got.State)
	}
}

func TestClassify_DecisionReady_CleanRun(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}

	got := Classify(input)
	if got.State != DeskStateDecisionReady {
		t.Errorf("expected decision_ready for a clean run, got %q", got.State)
	}
}

func TestClassify_DecisionReady_AllFindingsDispositioned(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}, {ID: "f2"}})}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventFindingDismissed, FindingID: "f1", OccurredAt: baseNow.Add(-time.Minute)},
		{Kind: LifecycleEventFindingSelected, FindingID: "f2", OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateDecisionReady {
		t.Errorf("expected decision_ready once all findings are dispositioned, got %q", got.State)
	}
}

func TestClassify_FindingsReady_PartiallyDispositionedStillReady(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}, {ID: "f2"}})}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventFindingDismissed, FindingID: "f1", OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateFindingsReady {
		t.Errorf("expected findings_ready while any finding remains undispositioned, got %q", got.State)
	}
}

func TestClassify_ResolvedByUserEventForCurrentHead(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}})}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventUserResolved, HeadObjectID: "head-1", OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateResolved {
		t.Errorf("expected resolved via explicit user_resolved event, got %q", got.State)
	}
}

func TestClassify_ResolvedByActionPostedForCurrentHead(t *testing.T) {
	input := baseInput()
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}})}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventActionPosted, HeadObjectID: "head-1", OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateResolved {
		t.Errorf("expected resolved once a review action was posted for this head, got %q", got.State)
	}
}

func TestClassify_Stale_WithinSettleWindow(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	input.Snapshot.UpdatedAt = baseNow.Add(-5 * time.Minute)
	input.SettleTime = 10 * time.Minute
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}

	got := Classify(input)
	if got.State != DeskStateStale {
		t.Errorf("expected stale while settling, got %q", got.State)
	}
}

func TestClassify_NeedsRereview_AfterSettleWindow(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	input.Snapshot.UpdatedAt = baseNow.Add(-20 * time.Minute)
	input.SettleTime = 10 * time.Minute
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}

	got := Classify(input)
	if got.State != DeskStateNeedsRereview {
		t.Errorf("expected needs_rereview once settled, got %q", got.State)
	}
}

func TestClassify_Snoozed_SuppressesRereviewIndefinitely(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	input.Snapshot.UpdatedAt = baseNow.Add(-20 * time.Minute)
	input.SettleTime = 10 * time.Minute
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventUserSnoozed, OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateStale {
		t.Errorf("expected snoozed PR to stay stale rather than promote to needs_rereview, got %q", got.State)
	}
}

func TestClassify_OptedOut_SuppressesRereviewIndefinitely(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	input.Snapshot.UpdatedAt = baseNow.Add(-20 * time.Minute)
	input.SettleTime = 10 * time.Minute
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventUserOptedOut, OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateStale {
		t.Errorf("expected opted-out PR to stay stale rather than promote to needs_rereview, got %q", got.State)
	}
}

func TestClassify_ResumeReversesSnoozeAndOptOut(t *testing.T) {
	for _, suppressor := range []LifecycleEventKind{LifecycleEventUserSnoozed, LifecycleEventUserOptedOut} {
		input := baseInput()
		input.Snapshot.HeadObjectID = "head-2"
		input.Snapshot.UpdatedAt = baseNow.Add(-20 * time.Minute)
		input.SettleTime = 10 * time.Minute
		input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}
		input.Events = []LifecycleEvent{
			{Kind: suppressor, OccurredAt: baseNow.Add(-2 * time.Minute)},
			{Kind: LifecycleEventUserResumed, OccurredAt: baseNow.Add(-time.Minute)},
		}

		got := Classify(input)
		if got.State != DeskStateNeedsRereview {
			t.Errorf("suppressor %q: expected resume to restore needs_rereview eligibility, got %q", suppressor, got.State)
		}
	}
}

func TestClassify_MissingFindingAcrossRevisionsIsNotResolved(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	oldRun := completedRun("head-1", ReviewConclusionFindings, []ReviewFinding{{ID: "f1"}, {ID: "f2"}})
	newRun := completedRun("head-2", ReviewConclusionFindings, []ReviewFinding{{ID: "f3"}})
	input.Runs = []ReviewRun{oldRun, newRun}

	got := Classify(input)
	if got.State == DeskStateResolved {
		t.Fatalf("a finding disappearing between stochastic runs must never be classified as resolved, got %+v", got)
	}
	if got.State != DeskStateFindingsReady {
		t.Errorf("expected findings_ready driven only by the current head's own run, got %q", got.State)
	}
}

func TestClassify_ActiveRunForNewHeadTakesPrecedenceOverStale(t *testing.T) {
	input := baseInput()
	input.Snapshot.HeadObjectID = "head-2"
	input.Snapshot.UpdatedAt = baseNow.Add(-20 * time.Minute)
	input.SettleTime = 10 * time.Minute
	input.Runs = []ReviewRun{completedRun("head-1", ReviewConclusionClean, nil)}
	input.Events = []LifecycleEvent{
		{Kind: LifecycleEventQueued, RunID: "run-b", HeadObjectID: "head-2", OccurredAt: baseNow.Add(-time.Minute)},
	}

	got := Classify(input)
	if got.State != DeskStateQueued {
		t.Errorf("expected an active run for the new head to take precedence over stale, got %q", got.State)
	}
}
