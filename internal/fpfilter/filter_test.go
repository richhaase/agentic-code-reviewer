package fpfilter

import (
	"encoding/json"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestFindingInputConstruction(t *testing.T) {
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

			// Construct findingInput the same way Apply() does
			input := findingInput{
				ID:            0,
				Title:         finding.Title,
				Summary:       finding.Summary,
				Messages:      finding.Messages,
				ReviewerCount: finding.ReviewerCount,
			}

			if input.ReviewerCount != tt.reviewerCount {
				t.Errorf("ReviewerCount = %d, want %d", input.ReviewerCount, tt.reviewerCount)
			}

			// Verify it serializes correctly to JSON
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var parsed findingInput
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if parsed.ReviewerCount != tt.reviewerCount {
				t.Errorf("after JSON round-trip: ReviewerCount = %d, want %d", parsed.ReviewerCount, tt.reviewerCount)
			}
		})
	}
}

func TestNewFilter(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		want      int
	}{
		{"valid threshold", 50, 50},
		{"zero uses default", 0, DefaultThreshold},
		{"negative uses default", -1, DefaultThreshold},
		{"above 100 uses default", 101, DefaultThreshold},
		{"exactly 100", 100, 100},
		{"exactly 1", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("claude", tt.threshold)
			if f.threshold != tt.want {
				t.Errorf("threshold = %d, want %d", f.threshold, tt.want)
			}
		})
	}
}
