package store

import (
	"encoding/json"
	"testing"
	"time"
)

func testPullRequestKey() PullRequestKeyV1 {
	return PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 196}
}

func TestReviewEventV1_AllVocabularyTypesRoundTripAndValidate(t *testing.T) {
	occurredAt := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		event ReviewEventV1
	}{
		{name: "pr discovered", event: ReviewEventV1{Type: EventTypePRDiscovered}},
		{name: "pr refreshed", event: ReviewEventV1{Type: EventTypePRRefreshed}},
		{name: "review queued", event: ReviewEventV1{Type: EventTypeReviewQueued, RunID: "run-1"}},
		{name: "review started", event: ReviewEventV1{Type: EventTypeReviewStarted, RunID: "run-1"}},
		{name: "review completed", event: ReviewEventV1{Type: EventTypeReviewCompleted, RunID: "run-1"}},
		{name: "review failed", event: ReviewEventV1{Type: EventTypeReviewFailed, RunID: "run-1", Reason: "all reviewers failed"}},
		{name: "review interrupted", event: ReviewEventV1{Type: EventTypeReviewInterrupted, RunID: "run-1"}},
		{name: "review superseded", event: ReviewEventV1{Type: EventTypeReviewSuperseded, RunID: "run-1"}},
		{name: "review stale", event: ReviewEventV1{Type: EventTypeReviewStale, RunID: "run-1", HeadObjectID: "new-head", PriorHeadObjectID: "old-head"}},
		{name: "finding selected", event: ReviewEventV1{Type: EventTypeFindingSelected, FindingID: "finding-1"}},
		{name: "finding dismissed", event: ReviewEventV1{Type: EventTypeFindingDismissed, FindingID: "finding-1"}},
		{name: "finding posted", event: ReviewEventV1{Type: EventTypeFindingPosted, FindingID: "finding-1"}},
		{name: "action comment posted", event: ReviewEventV1{Type: EventTypeActionCommentPosted, Actor: "richhaase"}},
		{name: "action request changes posted", event: ReviewEventV1{Type: EventTypeActionRequestChangesPosted, Actor: "richhaase"}},
		{name: "action approval posted", event: ReviewEventV1{Type: EventTypeActionApprovalPosted, Actor: "richhaase"}},
		{name: "pr closed", event: ReviewEventV1{Type: EventTypePRClosed}},
		{name: "pr merged", event: ReviewEventV1{Type: EventTypePRMerged}},
		{name: "user deferred", event: ReviewEventV1{Type: EventTypeUserDeferred, Actor: "richhaase"}},
		{name: "user snoozed", event: ReviewEventV1{Type: EventTypeUserSnoozed, Actor: "richhaase"}},
		{name: "user retried", event: ReviewEventV1{Type: EventTypeUserRetried, Actor: "richhaase"}},
		{name: "user resolved", event: ReviewEventV1{Type: EventTypeUserResolved, Actor: "richhaase"}},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tt.event
			event.SchemaVersion = CurrentSchemaVersion
			event.ID = "event-" + string(rune('a'+i))
			event.PullRequest = testPullRequestKey()
			event.OccurredAt = occurredAt

			if err := event.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			data, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ReviewEventV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded != event {
				t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, event)
			}
		})
	}
}

func TestReviewEventV1_Validate_RequiredFieldsPerType(t *testing.T) {
	base := func() ReviewEventV1 {
		return ReviewEventV1{
			SchemaVersion: CurrentSchemaVersion,
			ID:            "event-1",
			PullRequest:   testPullRequestKey(),
			OccurredAt:    time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
		}
	}

	tests := []struct {
		name  string
		event ReviewEventV1
	}{
		{name: "review completed missing run id", event: func() ReviewEventV1 { e := base(); e.Type = EventTypeReviewCompleted; return e }()},
		{name: "finding selected missing finding id", event: func() ReviewEventV1 { e := base(); e.Type = EventTypeFindingSelected; return e }()},
		{name: "action approval posted missing actor", event: func() ReviewEventV1 { e := base(); e.Type = EventTypeActionApprovalPosted; return e }()},
		{name: "user resolved missing actor", event: func() ReviewEventV1 { e := base(); e.Type = EventTypeUserResolved; return e }()},
		{name: "unsupported schema version", event: func() ReviewEventV1 { e := base(); e.Type = EventTypePRDiscovered; e.SchemaVersion = 99; return e }()},
		{name: "unknown event type", event: func() ReviewEventV1 { e := base(); e.Type = "something_else"; return e }()},
		{name: "zero occurred at", event: func() ReviewEventV1 {
			e := base()
			e.Type = EventTypePRDiscovered
			e.OccurredAt = time.Time{}
			return e
		}()},
		{name: "empty id", event: func() ReviewEventV1 { e := base(); e.Type = EventTypePRDiscovered; e.ID = ""; return e }()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); err == nil {
				t.Fatalf("expected a validation error for %+v", tt.event)
			}
		})
	}
}
