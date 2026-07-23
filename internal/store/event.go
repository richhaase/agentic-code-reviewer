package store

import (
	"fmt"
	"time"
)

// ReviewEventTypeV1 enumerates the append-only pull-request history event
// vocabulary from epic #191's Core Domain Model / ReviewEvent section.
type ReviewEventTypeV1 string

const (
	EventTypePRDiscovered ReviewEventTypeV1 = "pr_discovered"
	EventTypePRRefreshed  ReviewEventTypeV1 = "pr_refreshed"

	EventTypeReviewQueued      ReviewEventTypeV1 = "review_queued"
	EventTypeReviewStarted     ReviewEventTypeV1 = "review_started"
	EventTypeReviewCompleted   ReviewEventTypeV1 = "review_completed"
	EventTypeReviewFailed      ReviewEventTypeV1 = "review_failed"
	EventTypeReviewInterrupted ReviewEventTypeV1 = "review_interrupted"
	EventTypeReviewSuperseded  ReviewEventTypeV1 = "review_superseded"
	EventTypeReviewStale       ReviewEventTypeV1 = "review_stale"

	EventTypeFindingSelected  ReviewEventTypeV1 = "finding_selected"
	EventTypeFindingDismissed ReviewEventTypeV1 = "finding_dismissed"
	EventTypeFindingPosted    ReviewEventTypeV1 = "finding_posted"

	EventTypeActionCommentPosted        ReviewEventTypeV1 = "action_comment_posted"
	EventTypeActionRequestChangesPosted ReviewEventTypeV1 = "action_request_changes_posted"
	EventTypeActionApprovalPosted       ReviewEventTypeV1 = "action_approval_posted"

	EventTypePRClosed ReviewEventTypeV1 = "pr_closed"
	EventTypePRMerged ReviewEventTypeV1 = "pr_merged"

	EventTypeUserDeferred ReviewEventTypeV1 = "user_deferred"
	EventTypeUserSnoozed  ReviewEventTypeV1 = "user_snoozed"
	EventTypeUserRetried  ReviewEventTypeV1 = "user_retried"
	EventTypeUserResolved ReviewEventTypeV1 = "user_resolved"
)

func (t ReviewEventTypeV1) Validate() error {
	switch t {
	case EventTypePRDiscovered, EventTypePRRefreshed,
		EventTypeReviewQueued, EventTypeReviewStarted, EventTypeReviewCompleted,
		EventTypeReviewFailed, EventTypeReviewInterrupted, EventTypeReviewSuperseded, EventTypeReviewStale,
		EventTypeFindingSelected, EventTypeFindingDismissed, EventTypeFindingPosted,
		EventTypeActionCommentPosted, EventTypeActionRequestChangesPosted, EventTypeActionApprovalPosted,
		EventTypePRClosed, EventTypePRMerged,
		EventTypeUserDeferred, EventTypeUserSnoozed, EventTypeUserRetried, EventTypeUserResolved:
		return nil
	default:
		return fmt.Errorf("unknown review event type %q", t)
	}
}

func (t ReviewEventTypeV1) requiresRunID() bool {
	switch t {
	case EventTypeReviewQueued, EventTypeReviewStarted, EventTypeReviewCompleted,
		EventTypeReviewFailed, EventTypeReviewInterrupted, EventTypeReviewSuperseded, EventTypeReviewStale:
		return true
	default:
		return false
	}
}

func (t ReviewEventTypeV1) requiresFindingID() bool {
	switch t {
	case EventTypeFindingSelected, EventTypeFindingDismissed, EventTypeFindingPosted:
		return true
	default:
		return false
	}
}

func (t ReviewEventTypeV1) requiresActor() bool {
	switch t {
	case EventTypeActionCommentPosted, EventTypeActionRequestChangesPosted, EventTypeActionApprovalPosted,
		EventTypeUserDeferred, EventTypeUserSnoozed, EventTypeUserRetried, EventTypeUserResolved:
		return true
	default:
		return false
	}
}

// ReviewEventV1 is one immutable, append-only entry in a pull request's local
// history. Fields are a flat superset across the event vocabulary; only the
// fields relevant to a given Type are required. Events are never rewritten in
// place: a correction or later observation is recorded as an additional
// event, preserving the original.
type ReviewEventV1 struct {
	SchemaVersion     int               `json:"schema_version"`
	ID                string            `json:"id"`
	PullRequest       PullRequestKeyV1  `json:"pull_request"`
	Type              ReviewEventTypeV1 `json:"type"`
	OccurredAt        time.Time         `json:"occurred_at"`
	RunID             string            `json:"run_id,omitempty"`
	HeadObjectID      string            `json:"head_object_id,omitempty"`
	PriorHeadObjectID string            `json:"prior_head_object_id,omitempty"`
	FindingID         string            `json:"finding_id,omitempty"`
	Actor             string            `json:"actor,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	Message           string            `json:"message,omitempty"`
}

func (e ReviewEventV1) Validate() error {
	if err := validateSchemaVersion("review event", e.SchemaVersion); err != nil {
		return err
	}
	if err := validateNonEmpty("review event id", e.ID); err != nil {
		return err
	}
	if err := e.PullRequest.Validate(); err != nil {
		return err
	}
	if err := e.Type.Validate(); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		return fmt.Errorf("review event occurred_at is required")
	}
	if e.Type.requiresRunID() {
		if err := validateNonEmpty(fmt.Sprintf("review event %q run_id", e.Type), e.RunID); err != nil {
			return err
		}
	}
	if e.Type.requiresFindingID() {
		if err := validateNonEmpty(fmt.Sprintf("review event %q finding_id", e.Type), e.FindingID); err != nil {
			return err
		}
	}
	if e.Type.requiresActor() {
		if err := validateNonEmpty(fmt.Sprintf("review event %q actor", e.Type), e.Actor); err != nil {
			return err
		}
	}
	return nil
}
