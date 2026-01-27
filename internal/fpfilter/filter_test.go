package fpfilter

import (
	"encoding/json"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestFindingEvaluationConstruction(t *testing.T) {
	tests := []struct {
		name          string
		reviewerCount int
	}{
		{"zero passed through", 0},
		{"negative passed through", -5},
		{"positive value passed through", 5},
		{"one passed through", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := domain.FindingGroup{
				Title:         "Test",
				Summary:       "Test summary",
				Messages:      []string{"msg1"},
				ReviewerCount: tt.reviewerCount,
			}

			// Construct FindingEvaluation the same way Apply() does
			input := FindingEvaluation{
				ID:              0,
				Title:           finding.Title,
				Summary:         finding.Summary,
				Messages:        finding.Messages,
				ReviewerCount:   finding.ReviewerCount,
				IsFalsePositive: nil,
				Reasoning:       nil,
			}

			if input.ReviewerCount != tt.reviewerCount {
				t.Errorf("ReviewerCount = %d, want %d", input.ReviewerCount, tt.reviewerCount)
			}

			// Verify it serializes correctly to JSON with null fields
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var parsed FindingEvaluation
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if parsed.ReviewerCount != tt.reviewerCount {
				t.Errorf("after JSON round-trip: ReviewerCount = %d, want %d", parsed.ReviewerCount, tt.reviewerCount)
			}

			// Verify null fields remain null after round-trip
			if parsed.IsFalsePositive != nil {
				t.Errorf("IsFalsePositive should be nil, got %v", *parsed.IsFalsePositive)
			}
			if parsed.Reasoning != nil {
				t.Errorf("Reasoning should be nil, got %q", *parsed.Reasoning)
			}
		})
	}
}
