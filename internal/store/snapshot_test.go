package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func validSnapshot() PRSnapshotV1 {
	return PRSnapshotV1{
		SchemaVersion: CurrentSchemaVersion,
		PullRequest:   PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 196},
		URL:           "https://github.com/richhaase/agentic-code-reviewer/pull/196",
		Title:         "Define versioned persistence schemas",
		Author:        "richhaase",
		State:         PullRequestStateOpen,
		Draft:         false,
		HeadObjectID:  "headsha",
		BaseObjectID:  "basesha",
		ReviewRequests: []ReviewRequestV1{
			{Kind: ReviewRequestKindUser, Login: "reviewer1"},
			{Kind: ReviewRequestKindTeam, Login: "org/reviewers"},
		},
		ReviewDecision: "REVIEW_REQUIRED",
		LatestReviews: []LatestReviewV1{
			{Author: "reviewer2", State: "COMMENTED", SubmittedAt: time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)},
		},
		CheckRollupState: "PENDING",
		MergeState:       "CLEAN",
		UpdatedAt:        time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
		CapturedAt:       time.Date(2026, 7, 22, 9, 5, 0, 0, time.UTC),
	}
}

func TestPRSnapshotV1_RoundTrip(t *testing.T) {
	snapshot := validSnapshot()
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded PRSnapshotV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, snapshot) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, snapshot)
	}
}

func TestPRSnapshotV1_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(s *PRSnapshotV1)
		wantErr bool
	}{
		{name: "valid snapshot", mutate: func(s *PRSnapshotV1) {}, wantErr: false},
		{name: "unsupported schema version", mutate: func(s *PRSnapshotV1) { s.SchemaVersion = 99 }, wantErr: true},
		{name: "invalid pull request key", mutate: func(s *PRSnapshotV1) { s.PullRequest.Number = 0 }, wantErr: true},
		{name: "missing url", mutate: func(s *PRSnapshotV1) { s.URL = "" }, wantErr: true},
		{name: "unknown state", mutate: func(s *PRSnapshotV1) { s.State = "archived" }, wantErr: true},
		{name: "missing head object id", mutate: func(s *PRSnapshotV1) { s.HeadObjectID = "" }, wantErr: true},
		{name: "missing captured_at", mutate: func(s *PRSnapshotV1) { s.CapturedAt = time.Time{} }, wantErr: true},
		{name: "unknown review request kind", mutate: func(s *PRSnapshotV1) { s.ReviewRequests[0].Kind = "org" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshot()
			tt.mutate(&snapshot)
			err := snapshot.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
