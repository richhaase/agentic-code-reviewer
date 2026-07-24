package domain

import (
	"strings"
	"testing"
	"time"
)

func validSnapshot() PullRequestSnapshot {
	return PullRequestSnapshot{
		PullRequest:  PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 202},
		URL:          "https://github.com/richhaase/agentic-code-reviewer/pull/202",
		Title:        "Implement GitHub candidate discovery and PR enrichment",
		Author:       "richhaase",
		State:        PullRequestStateOpen,
		Draft:        false,
		HeadObjectID: "headsha",
		BaseObjectID: "basesha",
		ReviewRequests: []ReviewRequest{
			{Kind: ReviewRequestKindUser, Login: "reviewer1"},
			{Kind: ReviewRequestKindTeam, Login: "org/reviewers"},
		},
		ReviewDecision: "REVIEW_REQUIRED",
		LatestReviews: []LatestReview{
			{Author: "reviewer2", State: "COMMENTED", SubmittedAt: time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)},
		},
		CheckRollupState: "PENDING",
		MergeState:       "CLEAN",
		UpdatedAt:        time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		CapturedAt:       time.Date(2026, 7, 24, 9, 5, 0, 0, time.UTC),
	}
}

func TestPullRequestSnapshot_ValidateAccepts(t *testing.T) {
	if err := validSnapshot().Validate(); err != nil {
		t.Fatalf("expected valid snapshot, got %v", err)
	}
}

func TestPullRequestSnapshot_Validate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(s *PullRequestSnapshot)
	}{
		{"missing url", func(s *PullRequestSnapshot) { s.URL = "" }},
		{"invalid pull request key", func(s *PullRequestSnapshot) { s.PullRequest.Number = 0 }},
		{"invalid state", func(s *PullRequestSnapshot) { s.State = "unknown" }},
		{"missing head object id", func(s *PullRequestSnapshot) { s.HeadObjectID = "" }},
		{"missing base object id", func(s *PullRequestSnapshot) { s.BaseObjectID = "" }},
		{"invalid review request kind", func(s *PullRequestSnapshot) {
			s.ReviewRequests = []ReviewRequest{{Kind: "bogus", Login: "x"}}
		}},
		{"missing review request login", func(s *PullRequestSnapshot) {
			s.ReviewRequests = []ReviewRequest{{Kind: ReviewRequestKindUser, Login: ""}}
		}},
		{"missing latest review author", func(s *PullRequestSnapshot) {
			s.LatestReviews = []LatestReview{{Author: "", State: "APPROVED"}}
		}},
		{"missing latest review state", func(s *PullRequestSnapshot) {
			s.LatestReviews = []LatestReview{{Author: "reviewer", State: ""}}
		}},
		{"missing captured at", func(s *PullRequestSnapshot) { s.CapturedAt = time.Time{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshot()
			tt.mutate(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestPullRequestState_Validate(t *testing.T) {
	valid := []PullRequestState{PullRequestStateOpen, PullRequestStateClosed, PullRequestStateMerged}
	for _, s := range valid {
		if err := s.Validate(); err != nil {
			t.Errorf("expected %q to be valid, got %v", s, err)
		}
	}
	if err := PullRequestState("bogus").Validate(); err == nil {
		t.Error("expected error for unknown state")
	}
}

func TestPullRequestSnapshot_Age(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	snapshot := validSnapshot()
	snapshot.CapturedAt = now.Add(-5 * time.Minute)
	if got := snapshot.Age(now); got != 5*time.Minute {
		t.Errorf("expected 5m age, got %v", got)
	}

	snapshot.CapturedAt = time.Time{}
	if got := snapshot.Age(now); got != 0 {
		t.Errorf("expected zero age for zero captured_at, got %v", got)
	}

	snapshot.CapturedAt = now.Add(time.Minute)
	if got := snapshot.Age(now); got != 0 {
		t.Errorf("expected zero age for future captured_at, got %v", got)
	}
}

func TestReviewRequest_Validate(t *testing.T) {
	if err := (ReviewRequest{Kind: ReviewRequestKindUser, Login: "x"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := (ReviewRequest{Kind: "bogus", Login: "x"}).Validate(); err == nil {
		t.Error("expected error for unknown kind")
	}
	if err := (ReviewRequest{Kind: ReviewRequestKindUser, Login: " "}).Validate(); err == nil {
		t.Error("expected error for blank login")
	}
}

func TestPullRequestSnapshot_ValidateErrorMentionsField(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.HeadObjectID = ""
	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "head object id") {
		t.Fatalf("expected error mentioning head object id, got %v", err)
	}
}
