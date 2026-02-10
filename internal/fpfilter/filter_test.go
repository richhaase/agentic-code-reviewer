package fpfilter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestFilter_New(t *testing.T) {
	f := New("codex", 75, false, terminal.NewLogger())
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

func TestSkippedResult(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "finding 1", Summary: "summary 1"},
			{Title: "finding 2", Summary: "summary 2"},
		},
		Info: []domain.FindingGroup{
			{Title: "info 1"},
		},
	}

	tests := []struct {
		name   string
		reason string
	}{
		{"agent creation failed", "agent creation failed: codex not found"},
		{"request marshal failed", "request marshal failed: json error"},
		{"LLM execution failed", "LLM execution failed: timeout"},
		{"response read failed", "response read failed: io error"},
		{"response parse failed", "response parse failed: invalid json"},
		{"context canceled", "context canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			result := skippedResult(grouped, start, tt.reason)
			if !result.Skipped {
				t.Error("expected Skipped to be true")
			}
			if result.SkipReason != tt.reason {
				t.Errorf("SkipReason = %q, want %q", result.SkipReason, tt.reason)
			}
			if len(result.Grouped.Findings) != len(grouped.Findings) {
				t.Errorf("expected %d findings passed through, got %d", len(grouped.Findings), len(result.Grouped.Findings))
			}
			if len(result.Grouped.Info) != len(grouped.Info) {
				t.Errorf("expected %d info items passed through, got %d", len(grouped.Info), len(result.Grouped.Info))
			}
			if result.RemovedCount != 0 {
				t.Errorf("expected RemovedCount 0, got %d", result.RemovedCount)
			}
			if result.Duration < 0 {
				t.Error("expected non-negative Duration")
			}
		})
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

func TestBuildPromptWithStructuredFeedback(t *testing.T) {
	feedback := `- DISMISSED: "Non-atomic merge of shared map" -- protected by caller mutex (by @alice)
- FIXED: "Unchecked error from db.Connect()" -- fixed in commit abc123 (by @bob)
- INTENTIONAL: "Graph writes outside SQL transaction" -- intentional ordering (by @alice)`

	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	// Structured content preserved
	if !strings.Contains(prompt, "DISMISSED") {
		t.Error("prompt should contain DISMISSED status")
	}
	if !strings.Contains(prompt, "Non-atomic merge") {
		t.Error("prompt should preserve specific finding description")
	}

	// Matching instructions present
	if !strings.Contains(prompt, "semantic match") {
		t.Error("prompt should contain semantic matching guidance")
	}
	if !strings.Contains(prompt, "fp_score 90-100") {
		t.Error("prompt should specify fp_score range for DISMISSED matches")
	}
}

func TestFPPrompt_IncludesPriorFeedbackCheck(t *testing.T) {
	if !strings.Contains(fpEvaluationPrompt, "previously discussed") {
		t.Error("base prompt should reference checking prior feedback")
	}
}
