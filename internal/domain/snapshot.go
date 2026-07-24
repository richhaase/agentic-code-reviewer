package domain

import (
	"fmt"
	"strings"
	"time"
)

type PullRequestState string

const (
	PullRequestStateOpen   PullRequestState = "open"
	PullRequestStateClosed PullRequestState = "closed"
	PullRequestStateMerged PullRequestState = "merged"
)

func (s PullRequestState) Validate() error {
	switch s {
	case PullRequestStateOpen, PullRequestStateClosed, PullRequestStateMerged:
		return nil
	default:
		return fmt.Errorf("unknown pull request state %q", s)
	}
}

type ReviewRequestKind string

const (
	ReviewRequestKindUser ReviewRequestKind = "user"
	ReviewRequestKindTeam ReviewRequestKind = "team"
)

func (k ReviewRequestKind) Validate() error {
	switch k {
	case ReviewRequestKindUser, ReviewRequestKindTeam:
		return nil
	default:
		return fmt.Errorf("unknown review request kind %q", k)
	}
}

type ReviewRequest struct {
	Kind  ReviewRequestKind
	Login string
}

func (r ReviewRequest) Validate() error {
	if err := r.Kind.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.Login) == "" {
		return fmt.Errorf("review request login is required")
	}
	return nil
}

type LatestReview struct {
	Author      string
	State       string
	SubmittedAt time.Time
}

func (r LatestReview) Validate() error {
	if strings.TrimSpace(r.Author) == "" {
		return fmt.Errorf("review author is required")
	}
	if strings.TrimSpace(r.State) == "" {
		return fmt.Errorf("review state is required")
	}
	return nil
}

type PullRequestSnapshot struct {
	PullRequest      PullRequestKey
	URL              string
	Title            string
	Author           string
	State            PullRequestState
	Draft            bool
	HeadObjectID     string
	BaseObjectID     string
	ReviewRequests   []ReviewRequest
	ReviewDecision   string
	LatestReviews    []LatestReview
	CheckRollupState string
	MergeState       string
	UpdatedAt        time.Time
	CapturedAt       time.Time
	Stale            bool
}

func (s PullRequestSnapshot) Age(now time.Time) time.Duration {
	if s.CapturedAt.IsZero() {
		return 0
	}
	age := now.Sub(s.CapturedAt)
	if age < 0 {
		return 0
	}
	return age
}

func (s PullRequestSnapshot) Validate() error {
	if err := s.PullRequest.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("pull request snapshot url is required")
	}
	if err := s.State.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.HeadObjectID) == "" {
		return fmt.Errorf("pull request snapshot head object id is required")
	}
	if strings.TrimSpace(s.BaseObjectID) == "" {
		return fmt.Errorf("pull request snapshot base object id is required")
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
