package store

import (
	"encoding/json"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestPullRequestKeyV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		key  domain.PullRequestKey
	}{
		{
			name: "github.com PR",
			key:  domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 196},
		},
		{
			name: "enterprise host",
			key:  domain.PullRequestKey{Host: "github.example.com", Owner: "org", Repository: "repo", Number: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := ToPullRequestKeySchema(tt.key)

			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded PullRequestKeyV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded != schema {
				t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, schema)
			}
			if got := decoded.ToDomain(); got != tt.key {
				t.Fatalf("ToDomain mismatch: got %+v, want %+v", got, tt.key)
			}
			if err := decoded.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestPullRequestKeyV1_ValidateRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		key  PullRequestKeyV1
	}{
		{name: "empty host", key: PullRequestKeyV1{Owner: "o", Repository: "r", Number: 1}},
		{name: "host with port", key: PullRequestKeyV1{Host: "github.com:443", Owner: "o", Repository: "r", Number: 1}},
		{name: "zero number", key: PullRequestKeyV1{Host: "github.com", Owner: "o", Repository: "r", Number: 0}},
		{name: "negative number", key: PullRequestKeyV1{Host: "github.com", Owner: "o", Repository: "r", Number: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.key.Validate(); err == nil {
				t.Fatalf("expected validation error for %+v", tt.key)
			}
		})
	}
}

func TestReviewTargetV1_RoundTrip(t *testing.T) {
	pr := domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 196}
	target := domain.ReviewTarget{
		RepositoryRoot: "/repo",
		WorktreeRoot:   "/worktree",
		Revision: domain.RevisionEvidence{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "refs/remotes/origin/main",
			HeadObjectID:     "headsha",
			BaseObjectID:     "basesha",
		},
		PullRequest: &pr,
	}

	schema := ToReviewTargetSchema(target)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewTargetV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := decoded.ToDomain()
	if got.RepositoryRoot != target.RepositoryRoot ||
		got.WorktreeRoot != target.WorktreeRoot ||
		got.Revision != target.Revision ||
		got.PullRequest == nil || *got.PullRequest != *target.PullRequest {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, target)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestReviewTargetV1_RoundTripWithoutPullRequest(t *testing.T) {
	target := domain.ReviewTarget{
		RepositoryRoot: "/repo",
		WorktreeRoot:   "/worktree",
		Revision: domain.RevisionEvidence{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "refs/remotes/origin/main",
			HeadObjectID:     "headsha",
			BaseObjectID:     "basesha",
		},
	}

	schema := ToReviewTargetSchema(target)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewTargetV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PullRequest != nil {
		t.Fatalf("expected nil pull request, got %+v", decoded.PullRequest)
	}
	got := decoded.ToDomain()
	if got.PullRequest != nil {
		t.Fatalf("expected nil pull request in domain target, got %+v", got.PullRequest)
	}
}
