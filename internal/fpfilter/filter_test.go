package fpfilter

import (
	"context"
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

func TestNew_ThresholdClamping(t *testing.T) {
	logger := terminal.NewLogger()
	tests := []struct {
		name              string
		threshold         int
		expectedThreshold int
	}{
		{"zero uses default", 0, DefaultThreshold},
		{"negative uses default", -5, DefaultThreshold},
		{"above 100 uses default", 101, DefaultThreshold},
		{"valid 50 keeps 50", 50, 50},
		{"minimum valid 1", 1, 1},
		{"maximum valid 100", 100, 100},
		{"mid-range 75", 75, 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("codex", tt.threshold, false, logger)
			if f.threshold != tt.expectedThreshold {
				t.Errorf("threshold = %d, want %d", f.threshold, tt.expectedThreshold)
			}
		})
	}
}

func TestApply_EmptyFindings(t *testing.T) {
	f := New("codex", 75, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
	}

	result := f.Apply(context.Background(), grouped, "", 0)

	if result == nil {
		t.Fatal("Apply returned nil")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Grouped.Findings))
	}
	if result.RemovedCount != 0 {
		t.Errorf("expected 0 removed, got %d", result.RemovedCount)
	}
	if result.Skipped {
		t.Error("expected Skipped to be false for empty findings")
	}
	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestApply_EmptyFindings_WithTotalReviewers(t *testing.T) {
	f := New("codex", 75, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
	}
	result := f.Apply(context.Background(), grouped, "", 5)
	if result == nil {
		t.Fatal("Apply returned nil")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Grouped.Findings))
	}
}

func TestApply_EmptyFindingsPreservesInfo(t *testing.T) {
	f := New("codex", 75, false, terminal.NewLogger())
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
		Info: []domain.FindingGroup{
			{Title: "info 1", Summary: "informational note"},
			{Title: "info 2", Summary: "another note"},
		},
	}

	result := f.Apply(context.Background(), grouped, "", 0)

	if len(result.Grouped.Info) != 2 {
		t.Errorf("expected 2 info items preserved, got %d", len(result.Grouped.Info))
	}
	if result.Grouped.Info[0].Title != "info 1" {
		t.Errorf("info[0].Title = %q, want %q", result.Grouped.Info[0].Title, "info 1")
	}
}

func TestEvaluationRequest_Marshal(t *testing.T) {
	req := evaluationRequest{
		Findings: []findingInput{
			{ID: 0, Title: "Bug A", Summary: "null ptr", Messages: []string{"fix this"}, ReviewerCount: 3},
			{ID: 1, Title: "Bug B", Summary: "race condition", Messages: []string{"msg1", "msg2"}, ReviewerCount: 1},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	findings, ok := parsed["findings"].([]interface{})
	if !ok {
		t.Fatal("findings field missing or wrong type")
	}
	if len(findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(findings))
	}

	first := findings[0].(map[string]interface{})
	if first["title"] != "Bug A" {
		t.Errorf("first finding title = %q, want %q", first["title"], "Bug A")
	}
	if int(first["reviewer_count"].(float64)) != 3 {
		t.Errorf("first finding reviewer_count = %v, want 3", first["reviewer_count"])
	}
}

func TestEvaluationResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantEvalCount  int
		wantFirstID    int
		wantFirstScore int
		wantErr        bool
	}{
		{
			name:           "valid response with multiple evaluations",
			input:          `{"evaluations":[{"id":0,"fp_score":80,"reasoning":"likely false positive"},{"id":1,"fp_score":30,"reasoning":"real issue"}]}`,
			wantEvalCount:  2,
			wantFirstID:    0,
			wantFirstScore: 80,
		},
		{
			name:          "empty evaluations array",
			input:         `{"evaluations":[]}`,
			wantEvalCount: 0,
		},
		{
			name:           "missing fields default to zero",
			input:          `{"evaluations":[{"id":0}]}`,
			wantEvalCount:  1,
			wantFirstID:    0,
			wantFirstScore: 0,
		},
		{
			name:    "invalid JSON",
			input:   `{not valid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp evaluationResponse
			err := json.Unmarshal([]byte(tt.input), &resp)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Evaluations) != tt.wantEvalCount {
				t.Fatalf("got %d evaluations, want %d", len(resp.Evaluations), tt.wantEvalCount)
			}
			if tt.wantEvalCount > 0 {
				if resp.Evaluations[0].ID != tt.wantFirstID {
					t.Errorf("first eval ID = %d, want %d", resp.Evaluations[0].ID, tt.wantFirstID)
				}
				if resp.Evaluations[0].FPScore != tt.wantFirstScore {
					t.Errorf("first eval FPScore = %d, want %d", resp.Evaluations[0].FPScore, tt.wantFirstScore)
				}
			}
		})
	}
}

func TestAgreementBonus(t *testing.T) {
	tests := []struct {
		name           string
		reviewerCount  int
		totalReviewers int
		wantBonus      int
	}{
		// < 20% agreement — strong penalty (+15)
		{"1 of 6 reviewers (17%)", 1, 6, 15},
		{"1 of 8 reviewers (13%)", 1, 8, 15},
		{"1 of 10 reviewers (10%)", 1, 10, 15},
		{"2 of 12 reviewers (17%)", 2, 12, 15},
		{"1 of 20 reviewers (5%)", 1, 20, 15},

		// 20-39% agreement — moderate penalty (+10)
		{"1 of 5 reviewers (20%)", 1, 5, 10},
		{"1 of 3 reviewers (33%)", 1, 3, 10},
		{"1 of 4 reviewers (25%)", 1, 4, 10},
		{"2 of 6 reviewers (33%)", 2, 6, 10},
		{"3 of 10 reviewers (30%)", 3, 10, 10},
		{"2 of 8 reviewers (25%)", 2, 8, 10},

		// >= 40% agreement — no penalty
		{"2 of 5 reviewers (40%)", 2, 5, 0},
		{"2 of 4 reviewers (50%)", 2, 4, 0},
		{"3 of 6 reviewers (50%)", 3, 6, 0},
		{"4 of 10 reviewers (40%)", 4, 10, 0},
		{"5 of 6 reviewers (83%)", 5, 6, 0},
		{"6 of 6 reviewers (100%)", 6, 6, 0},
		{"1 of 2 reviewers (50%)", 1, 2, 0},
		{"2 of 3 reviewers (67%)", 2, 3, 0},
		{"2 of 2 reviewers (100%)", 2, 2, 0},

		// Edge cases — no penalty
		{"1 of 1 reviewer", 1, 1, 0},
		{"0 totalReviewers", 1, 0, 0},
		{"0 reviewerCount", 0, 6, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agreementBonus(tt.reviewerCount, tt.totalReviewers)
			if got != tt.wantBonus {
				t.Errorf("agreementBonus(%d, %d) = %d, want %d",
					tt.reviewerCount, tt.totalReviewers, got, tt.wantBonus)
			}
		})
	}
}

func TestFilteringLogic(t *testing.T) {
	// This test simulates the core filtering logic from Apply() without
	// needing an external agent. We replicate the evalMap + threshold logic.
	threshold := 75

	findings := []domain.FindingGroup{
		{Title: "Finding 0", Summary: "At threshold", ReviewerCount: 2},       // id=0, score=75 -> removed
		{Title: "Finding 1", Summary: "Below threshold", ReviewerCount: 1},    // id=1, score=74 -> kept
		{Title: "Finding 2", Summary: "Above threshold", ReviewerCount: 1},    // id=2, score=90 -> removed
		{Title: "Finding 3", Summary: "Missing evaluation", ReviewerCount: 3}, // id=3, no eval -> kept + error
		{Title: "Finding 4", Summary: "Well below", ReviewerCount: 2},         // id=4, score=10 -> kept
	}

	infoItems := []domain.FindingGroup{
		{Title: "Info 1", Summary: "informational"},
	}

	response := evaluationResponse{
		Evaluations: []findingEvaluation{
			{ID: 0, FPScore: 75, Reasoning: "exactly at threshold"},
			{ID: 1, FPScore: 74, Reasoning: "just below threshold"},
			{ID: 2, FPScore: 90, Reasoning: "clearly false positive"},
			// ID 3 intentionally missing
			{ID: 4, FPScore: 10, Reasoning: "very likely real issue"},
		},
	}

	// Build evalMap (same as Apply)
	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	evalErrors := 0

	for i, finding := range findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			evalErrors++
			continue
		}
		if eval.FPScore >= threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   eval.FPScore,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	// Verify kept findings
	if len(kept) != 3 {
		t.Fatalf("expected 3 kept findings, got %d", len(kept))
	}
	keptTitles := map[string]bool{}
	for _, f := range kept {
		keptTitles[f.Title] = true
	}
	if !keptTitles["Finding 1"] {
		t.Error("Finding 1 (below threshold) should be kept")
	}
	if !keptTitles["Finding 3"] {
		t.Error("Finding 3 (missing eval) should be kept")
	}
	if !keptTitles["Finding 4"] {
		t.Error("Finding 4 (well below) should be kept")
	}

	// Verify removed findings
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed findings, got %d", len(removed))
	}
	removedTitles := map[string]bool{}
	for _, ef := range removed {
		removedTitles[ef.Finding.Title] = true
	}
	if !removedTitles["Finding 0"] {
		t.Error("Finding 0 (at threshold) should be removed")
	}
	if !removedTitles["Finding 2"] {
		t.Error("Finding 2 (above threshold) should be removed")
	}

	// Verify eval errors
	if evalErrors != 1 {
		t.Errorf("expected 1 eval error, got %d", evalErrors)
	}

	// Verify info items are always preserved (they never go through filtering)
	result := &Result{
		Grouped: domain.GroupedFindings{
			Findings: kept,
			Info:     infoItems,
		},
		Removed:      removed,
		RemovedCount: len(removed),
		EvalErrors:   evalErrors,
	}

	if len(result.Grouped.Info) != 1 {
		t.Errorf("expected 1 info item preserved, got %d", len(result.Grouped.Info))
	}
	if result.Grouped.Info[0].Title != "Info 1" {
		t.Errorf("info title = %q, want %q", result.Grouped.Info[0].Title, "Info 1")
	}
}
