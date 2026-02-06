package fpfilter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFilter_New(t *testing.T) {
	f := New("codex", 75, false)
	if f == nil {
		t.Fatal("New returned nil")
	}
}

func TestBuildPromptWithFeedback(t *testing.T) {
	feedback := "User said the null check is intentional"
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	if !strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should contain Prior Feedback Context section")
	}
	if !strings.Contains(prompt, feedback) {
		t.Error("prompt should contain the feedback text")
	}
}

func TestBuildPromptWithoutFeedback(t *testing.T) {
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, "")

	if strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should not contain Prior Feedback Context when feedback is empty")
	}
}

func TestFPPrompt_IncludesReviewerCountGuidance(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "reviewer_count") {
		t.Error("prompt should reference reviewer_count field")
	}
	if !strings.Contains(fpEvaluationPrompt, "Reviewer Agreement") {
		t.Error("prompt should contain Reviewer Agreement section")
	}
}

func TestFindingInput_IncludesReviewerCount(t *testing.T) {
	tests := []struct {
		name          string
		reviewerCount int
	}{
		{"zero reviewers", 0},
		{"single reviewer", 1},
		{"multiple reviewers", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := findingInput{
				ID:            0,
				Title:         "Test",
				Summary:       "Summary",
				Messages:      []string{"msg"},
				ReviewerCount: tt.reviewerCount,
			}

			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var parsed map[string]any
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			rc, ok := parsed["reviewer_count"]
			if !ok {
				t.Fatal("reviewer_count field missing from JSON output")
			}
			if int(rc.(float64)) != tt.reviewerCount {
				t.Errorf("reviewer_count = %v, want %d", rc, tt.reviewerCount)
			}
		})
	}
}
