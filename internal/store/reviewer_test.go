package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestReviewerResultV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		result domain.ReviewerResult
	}{
		{
			name: "successful reviewer",
			result: domain.ReviewerResult{
				ReviewerID: 0,
				AgentName:  "claude",
				Findings:   []domain.Finding{{Text: "issue", ReviewerID: 0}},
				ExitCode:   0,
				Attempts:   1,
				Duration:   3 * time.Second,
			},
		},
		{
			name: "failed reviewer with warnings",
			result: domain.ReviewerResult{
				ReviewerID:  1,
				AgentName:   "codex",
				ExitCode:    1,
				Attempts:    2,
				ParseErrors: 1,
				TimedOut:    true,
				Duration:    30 * time.Second,
				Failure:     &domain.ReviewerFailure{Kind: domain.ReviewerFailureTimeout, Message: "deadline exceeded"},
				Warnings:    []domain.ReviewerWarning{{Kind: domain.ReviewerWarningCleanup, Message: "temp dir cleanup failed"}},
			},
		},
		{
			name: "interrupted reviewer retains agent identity",
			result: domain.ReviewerResult{
				ReviewerID: 2,
				AgentName:  "antigravity",
				Failure:    &domain.ReviewerFailure{Kind: domain.ReviewerFailureInterrupted, Message: "canceled"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ToReviewerResultSchema(tt.result)
			if err != nil {
				t.Fatalf("ToReviewerResultSchema: %v", err)
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ReviewerResultV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, err := decoded.ToDomain()
			if err != nil {
				t.Fatalf("ToDomain: %v", err)
			}
			if got.ReviewerID != tt.result.ReviewerID || got.AgentName != tt.result.AgentName {
				t.Fatalf("identity mismatch: got %+v, want %+v", got, tt.result)
			}
			if (got.Failure == nil) != (tt.result.Failure == nil) {
				t.Fatalf("failure presence mismatch: got %+v, want %+v", got.Failure, tt.result.Failure)
			}
			if got.Failure != nil && *got.Failure != *tt.result.Failure {
				t.Fatalf("failure mismatch: got %+v, want %+v", *got.Failure, *tt.result.Failure)
			}
			if len(got.Warnings) != len(tt.result.Warnings) {
				t.Fatalf("warnings length mismatch: got %d, want %d", len(got.Warnings), len(tt.result.Warnings))
			}
		})
	}
}

func TestReviewStatsV1_RoundTrip(t *testing.T) {
	stats := domain.ReviewStats{
		TotalReviewers:      3,
		SuccessfulReviewers: 2,
		FailedReviewers:     []int{2},
		TimedOutReviewers:   []int{},
		AuthFailedReviewers: []int{},
		ParseErrors:         1,
		ReviewerDurations:   map[int]time.Duration{0: time.Second, 1: 2 * time.Second},
		ReviewerAgentNames:  map[int]string{0: "claude", 1: "codex", 2: "antigravity"},
		WallClockDuration:   5 * time.Second,
		SummarizerDuration:  time.Second,
		FPFilterDuration:    time.Second,
		FPFilteredCount:     1,
	}

	schema := ToReviewStatsSchema(stats)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewStatsV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := decoded.ToDomain()
	if got.TotalReviewers != stats.TotalReviewers || got.ReviewerAgentNames[1] != "codex" || got.ReviewerDurations[0] != time.Second {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, stats)
	}
}
