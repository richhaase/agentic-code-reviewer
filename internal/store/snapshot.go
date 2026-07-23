package store

import (
	"fmt"
	"time"
)

type PullRequestStateV1 string

const (
	PullRequestStateOpen   PullRequestStateV1 = "open"
	PullRequestStateClosed PullRequestStateV1 = "closed"
	PullRequestStateMerged PullRequestStateV1 = "merged"
)

func (s PullRequestStateV1) Validate() error {
	switch s {
	case PullRequestStateOpen, PullRequestStateClosed, PullRequestStateMerged:
		return nil
	default:
		return fmt.Errorf("unknown pull request state %q", s)
	}
}

type ReviewRequestKindV1 string

const (
	ReviewRequestKindUser ReviewRequestKindV1 = "user"
	ReviewRequestKindTeam ReviewRequestKindV1 = "team"
)

func (k ReviewRequestKindV1) Validate() error {
	switch k {
	case ReviewRequestKindUser, ReviewRequestKindTeam:
		return nil
	default:
		return fmt.Errorf("unknown review request kind %q", k)
	}
}

type ReviewRequestV1 struct {
	Kind  ReviewRequestKindV1 `json:"kind"`
	Login string              `json:"login"`
}

func (r ReviewRequestV1) Validate() error {
	if err := r.Kind.Validate(); err != nil {
		return err
	}
	return validateNonEmpty("review request login", r.Login)
}

type LatestReviewV1 struct {
	Author      string    `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (r LatestReviewV1) Validate() error {
	if err := validateNonEmpty("review author", r.Author); err != nil {
		return err
	}
	return validateNonEmpty("review state", r.State)
}

type PRSnapshotV1 struct {
	SchemaVersion    int                `json:"schema_version"`
	PullRequest      PullRequestKeyV1   `json:"pull_request"`
	URL              string             `json:"url"`
	Title            string             `json:"title"`
	Author           string             `json:"author"`
	State            PullRequestStateV1 `json:"state"`
	Draft            bool               `json:"draft"`
	HeadObjectID     string             `json:"head_object_id"`
	BaseObjectID     string             `json:"base_object_id"`
	ReviewRequests   []ReviewRequestV1  `json:"review_requests"`
	ReviewDecision   string             `json:"review_decision"`
	LatestReviews    []LatestReviewV1   `json:"latest_reviews"`
	CheckRollupState string             `json:"check_rollup_state"`
	MergeState       string             `json:"merge_state"`
	UpdatedAt        time.Time          `json:"updated_at"`
	CapturedAt       time.Time          `json:"captured_at"`
}

func (s PRSnapshotV1) Age(now time.Time) time.Duration {
	if s.CapturedAt.IsZero() {
		return 0
	}
	age := now.Sub(s.CapturedAt)
	if age < 0 {
		return 0
	}
	return age
}

func (s PRSnapshotV1) Validate() error {
	if err := validateSchemaVersion("pull request snapshot", s.SchemaVersion); err != nil {
		return err
	}
	if err := s.PullRequest.Validate(); err != nil {
		return err
	}
	if err := validateNonEmpty("pull request url", s.URL); err != nil {
		return err
	}
	if err := s.State.Validate(); err != nil {
		return err
	}
	if err := validateNonEmpty("pull request head object id", s.HeadObjectID); err != nil {
		return err
	}
	if err := validateNonEmpty("pull request base object id", s.BaseObjectID); err != nil {
		return err
	}
	for i, request := range s.ReviewRequests {
		if err := request.Validate(); err != nil {
			return fmt.Errorf("review request %d: %w", i, err)
		}
	}
	for i, review := range s.LatestReviews {
		if err := review.Validate(); err != nil {
			return fmt.Errorf("latest review %d: %w", i, err)
		}
	}
	if s.CapturedAt.IsZero() {
		return fmt.Errorf("pull request snapshot captured_at is required")
	}
	return nil
}
