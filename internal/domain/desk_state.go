package domain

import (
	"slices"
	"strings"
	"time"
)

type DeskState string

const (
	DeskStateNeedsReview           DeskState = "needs_review"
	DeskStateNeedsRereview         DeskState = "needs_rereview"
	DeskStateQueued                DeskState = "queued"
	DeskStateRunning               DeskState = "running"
	DeskStateFindingsReady         DeskState = "findings_ready"
	DeskStateDecisionReady         DeskState = "decision_ready"
	DeskStateStale                 DeskState = "stale"
	DeskStateWaitingOnOthers       DeskState = "waiting_on_others"
	DeskStateRepositoryUnavailable DeskState = "repository_unavailable"
	DeskStateFailed                DeskState = "failed"
	DeskStateResolved              DeskState = "resolved"
)

type LifecycleEventKind string

const (
	LifecycleEventQueued           LifecycleEventKind = "queued"
	LifecycleEventStarted          LifecycleEventKind = "started"
	LifecycleEventCompleted        LifecycleEventKind = "completed"
	LifecycleEventFailed           LifecycleEventKind = "failed"
	LifecycleEventInterrupted      LifecycleEventKind = "interrupted"
	LifecycleEventSuperseded       LifecycleEventKind = "superseded"
	LifecycleEventFindingSelected  LifecycleEventKind = "finding_selected"
	LifecycleEventFindingDismissed LifecycleEventKind = "finding_dismissed"
	LifecycleEventActionPosted     LifecycleEventKind = "action_posted"
	LifecycleEventUserResolved     LifecycleEventKind = "user_resolved"
	LifecycleEventUserReleased     LifecycleEventKind = "user_released"
	LifecycleEventUserSnoozed      LifecycleEventKind = "user_snoozed"
	LifecycleEventUserOptedOut     LifecycleEventKind = "user_opted_out"
	LifecycleEventUserResumed      LifecycleEventKind = "user_resumed"
)

type LifecycleEvent struct {
	Kind         LifecycleEventKind
	OccurredAt   time.Time
	RunID        string
	HeadObjectID string
	FindingID    string
}

type ClassificationInput struct {
	Snapshot            PullRequestSnapshot
	RepositoryAvailable bool
	ExpectedUser        string
	ReviewOwnPRs        bool
	SettleTime          time.Duration
	Now                 time.Time
	Runs                []ReviewRun
	Events              []LifecycleEvent
}

type Classification struct {
	State   DeskState
	Tracked bool
	Reason  string
}

func Classify(input ClassificationInput) Classification {
	if isReleased(input.Events) {
		return Classification{Tracked: false, Reason: "released by user"}
	}

	switch input.Snapshot.State {
	case PullRequestStateClosed, PullRequestStateMerged:
		return Classification{State: DeskStateResolved, Tracked: true, Reason: "pull request closed or merged"}
	}

	if !input.RepositoryAvailable {
		return Classification{State: DeskStateRepositoryUnavailable, Tracked: true, Reason: "repository not available locally"}
	}

	if isOwnPR(input.Snapshot, input.ExpectedUser) && !input.ReviewOwnPRs {
		return Classification{State: DeskStateWaitingOnOthers, Tracked: true, Reason: "own pull request; automatic review is disabled"}
	}

	head := input.Snapshot.HeadObjectID

	if resolvedForHead(input.Events, head) {
		return Classification{State: DeskStateResolved, Tracked: true, Reason: "user marked this head resolved"}
	}
	if actionPostedForHead(input.Events, head) {
		return Classification{State: DeskStateResolved, Tracked: true, Reason: "a review action was already posted for this head"}
	}

	if queued, running := activeRunState(input.Events, head); running {
		return Classification{State: DeskStateRunning, Tracked: true, Reason: "a review is running for this head"}
	} else if queued {
		return Classification{State: DeskStateQueued, Tracked: true, Reason: "a review is queued for this head"}
	}

	if run := runForHead(input.Runs, head); run != nil {
		switch run.Status {
		case ReviewStatusFailed:
			return Classification{State: DeskStateFailed, Tracked: true, Reason: "the review for this head failed"}
		case ReviewStatusInterrupted:
			return Classification{State: DeskStateNeedsReview, Tracked: true, Reason: "the review for this head was interrupted and needs to run again"}
		case ReviewStatusCompleted:
			if run.Conclusion == ReviewConclusionFindings && !allFindingsDispositioned(run, input.Events) {
				return Classification{State: DeskStateFindingsReady, Tracked: true, Reason: "the review completed with findings awaiting triage"}
			}
			return Classification{State: DeskStateDecisionReady, Tracked: true, Reason: "the review completed and is ready for a decision"}
		}
	}

	latestRun := mostRecentRun(input.Runs)
	if latestRun == nil {
		return Classification{State: DeskStateNeedsReview, Tracked: true, Reason: "no review has run for this pull request yet"}
	}

	if isReReviewSuppressed(input.Events) {
		return Classification{State: DeskStateStale, Tracked: true, Reason: "head changed since the last review; automatic re-review is snoozed or opted out"}
	}

	settledAt := input.Snapshot.UpdatedAt.Add(input.SettleTime)
	if input.Now.Before(settledAt) {
		return Classification{State: DeskStateStale, Tracked: true, Reason: "head changed since the last review; settling before re-review"}
	}
	return Classification{State: DeskStateNeedsRereview, Tracked: true, Reason: "head changed since the last review and has settled"}
}

func isOwnPR(snapshot PullRequestSnapshot, expectedUser string) bool {
	return expectedUser != "" && strings.EqualFold(snapshot.Author, expectedUser)
}

func latestLifecycleEvent(events []LifecycleEvent, kinds ...LifecycleEventKind) *LifecycleEvent {
	var latest *LifecycleEvent
	for i := range events {
		event := events[i]
		if !slices.Contains(kinds, event.Kind) {
			continue
		}
		if latest == nil || event.OccurredAt.After(latest.OccurredAt) {
			latest = &event
		}
	}
	return latest
}

func isReleased(events []LifecycleEvent) bool {
	latest := latestLifecycleEvent(events, LifecycleEventUserReleased, LifecycleEventUserResumed)
	return latest != nil && latest.Kind == LifecycleEventUserReleased
}

func isReReviewSuppressed(events []LifecycleEvent) bool {
	latest := latestLifecycleEvent(events, LifecycleEventUserSnoozed, LifecycleEventUserOptedOut, LifecycleEventUserResumed)
	return latest != nil && (latest.Kind == LifecycleEventUserSnoozed || latest.Kind == LifecycleEventUserOptedOut)
}

func resolvedForHead(events []LifecycleEvent, head string) bool {
	for _, event := range events {
		if event.Kind == LifecycleEventUserResolved && event.HeadObjectID == head {
			return true
		}
	}
	return false
}

func actionPostedForHead(events []LifecycleEvent, head string) bool {
	for _, event := range events {
		if event.Kind == LifecycleEventActionPosted && event.HeadObjectID == head {
			return true
		}
	}
	return false
}

func activeRunState(events []LifecycleEvent, head string) (queued, running bool) {
	type progress struct {
		queuedAt   time.Time
		startedAt  time.Time
		terminated bool
	}
	byRun := map[string]*progress{}

	for _, event := range events {
		if event.HeadObjectID != head || event.RunID == "" {
			continue
		}
		p, ok := byRun[event.RunID]
		if !ok {
			p = &progress{}
			byRun[event.RunID] = p
		}
		switch event.Kind {
		case LifecycleEventQueued:
			p.queuedAt = event.OccurredAt
		case LifecycleEventStarted:
			p.startedAt = event.OccurredAt
		case LifecycleEventCompleted, LifecycleEventFailed, LifecycleEventInterrupted, LifecycleEventSuperseded:
			p.terminated = true
		}
	}

	for _, p := range byRun {
		if p.terminated {
			continue
		}
		if !p.startedAt.IsZero() {
			return false, true
		}
		if !p.queuedAt.IsZero() {
			queued = true
		}
	}
	return queued, false
}

func mostRecentRun(runs []ReviewRun) *ReviewRun {
	var latest *ReviewRun
	for i := range runs {
		run := runs[i]
		ts := runTimestamp(run)
		if latest == nil || ts.After(runTimestamp(*latest)) {
			latest = &run
		}
	}
	return latest
}

func runForHead(runs []ReviewRun, head string) *ReviewRun {
	var latest *ReviewRun
	for i := range runs {
		run := runs[i]
		if run.Target.Revision.HeadObjectID != head {
			continue
		}
		if latest == nil || runTimestamp(run).After(runTimestamp(*latest)) {
			latest = &run
		}
	}
	return latest
}

func runTimestamp(run ReviewRun) time.Time {
	if !run.CompletedAt.IsZero() {
		return run.CompletedAt
	}
	return run.StartedAt
}

func allFindingsDispositioned(run *ReviewRun, events []LifecycleEvent) bool {
	if run == nil || len(run.Findings) == 0 {
		return true
	}
	dispositioned := map[string]bool{}
	for _, event := range events {
		if event.Kind == LifecycleEventFindingSelected || event.Kind == LifecycleEventFindingDismissed {
			dispositioned[event.FindingID] = true
		}
	}
	for _, finding := range run.Findings {
		if !dispositioned[finding.ID] {
			return false
		}
	}
	return true
}
