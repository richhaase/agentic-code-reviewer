package domain

import (
	"testing"
)

func TestAggregateFindings_DeduplicatesByText(t *testing.T) {
	findings := []Finding{
		{Text: "Issue A", ReviewerID: 1},
		{Text: "Issue B", ReviewerID: 2},
		{Text: "Issue A", ReviewerID: 3}, // duplicate text
	}

	result := AggregateFindings(findings)

	if len(result) != 2 {
		t.Fatalf("expected 2 unique findings, got %d", len(result))
	}

	// Check that Issue A has both reviewers
	var issueA *AggregatedFinding
	for i := range result {
		if result[i].Text == "Issue A" {
			issueA = &result[i]
			break
		}
	}
	if issueA == nil {
		t.Fatal("expected to find Issue A")
	}
	if len(issueA.Reviewers) != 2 {
		t.Errorf("expected Issue A to have 2 reviewers, got %d", len(issueA.Reviewers))
	}
}

func TestAggregateFindings_PreservesInsertionOrder(t *testing.T) {
	findings := []Finding{
		{Text: "First", ReviewerID: 1},
		{Text: "Second", ReviewerID: 1},
		{Text: "Third", ReviewerID: 1},
		{Text: "First", ReviewerID: 2}, // duplicate - should not change order
	}

	result := AggregateFindings(findings)

	if len(result) != 3 {
		t.Fatalf("expected 3 unique findings, got %d", len(result))
	}
	if result[0].Text != "First" {
		t.Errorf("expected first finding to be 'First', got %q", result[0].Text)
	}
	if result[1].Text != "Second" {
		t.Errorf("expected second finding to be 'Second', got %q", result[1].Text)
	}
	if result[2].Text != "Third" {
		t.Errorf("expected third finding to be 'Third', got %q", result[2].Text)
	}
}

func TestAggregateFindings_SortsReviewerIDs(t *testing.T) {
	findings := []Finding{
		{Text: "Issue", ReviewerID: 5},
		{Text: "Issue", ReviewerID: 2},
		{Text: "Issue", ReviewerID: 8},
		{Text: "Issue", ReviewerID: 1},
	}

	result := AggregateFindings(findings)

	if len(result) != 1 {
		t.Fatalf("expected 1 aggregated finding, got %d", len(result))
	}
	expected := []int{1, 2, 5, 8}
	if len(result[0].Reviewers) != 4 {
		t.Fatalf("expected 4 reviewers, got %d", len(result[0].Reviewers))
	}
	for i, want := range expected {
		if result[0].Reviewers[i] != want {
			t.Errorf("reviewer[%d]: expected %d, got %d", i, want, result[0].Reviewers[i])
		}
	}
}

func TestAggregateFindings_IgnoresDuplicateReviewerIDs(t *testing.T) {
	findings := []Finding{
		{Text: "Issue", ReviewerID: 1},
		{Text: "Issue", ReviewerID: 1}, // same reviewer, same finding
		{Text: "Issue", ReviewerID: 1}, // third time
	}

	result := AggregateFindings(findings)

	if len(result) != 1 {
		t.Fatalf("expected 1 aggregated finding, got %d", len(result))
	}
	if len(result[0].Reviewers) != 1 {
		t.Errorf("expected 1 unique reviewer, got %d", len(result[0].Reviewers))
	}
}

func TestAggregateFindings_SkipsEmptyText(t *testing.T) {
	findings := []Finding{
		{Text: "", ReviewerID: 1},
		{Text: "Valid", ReviewerID: 2},
		{Text: "", ReviewerID: 3},
	}

	result := AggregateFindings(findings)

	if len(result) != 1 {
		t.Fatalf("expected 1 finding (empty skipped), got %d", len(result))
	}
	if result[0].Text != "Valid" {
		t.Errorf("expected 'Valid', got %q", result[0].Text)
	}
}

func TestAggregateFindings_EmptyInput(t *testing.T) {
	result := AggregateFindings(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}

	result = AggregateFindings([]Finding{})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(result))
	}
}

func TestGroupedFindings_HasFindings(t *testing.T) {
	empty := GroupedFindings{}
	if empty.HasFindings() {
		t.Error("expected HasFindings() to be false for empty GroupedFindings")
	}

	withFindings := GroupedFindings{
		Findings: []FindingGroup{{Title: "test"}},
	}
	if !withFindings.HasFindings() {
		t.Error("expected HasFindings() to be true when findings exist")
	}
}

func TestGroupedFindings_TotalGroups(t *testing.T) {
	g := GroupedFindings{
		Findings: []FindingGroup{{Title: "f1"}, {Title: "f2"}},
		Info:     []FindingGroup{{Title: "i1"}},
	}
	if g.TotalGroups() != 3 {
		t.Errorf("expected TotalGroups() = 3, got %d", g.TotalGroups())
	}
}

func TestReviewStats_AllFailed(t *testing.T) {
	tests := []struct {
		name     string
		stats    ReviewStats
		expected bool
	}{
		{
			name: "all successful",
			stats: ReviewStats{
				TotalReviewers:      3,
				SuccessfulReviewers: 3,
			},
			expected: false,
		},
		{
			name: "all failed",
			stats: ReviewStats{
				TotalReviewers:  3,
				FailedReviewers: []int{1, 2, 3},
			},
			expected: true,
		},
		{
			name: "all timed out",
			stats: ReviewStats{
				TotalReviewers:    3,
				TimedOutReviewers: []int{1, 2, 3},
			},
			expected: true,
		},
		{
			name: "mixed failures covering all",
			stats: ReviewStats{
				TotalReviewers:    3,
				FailedReviewers:   []int{1},
				TimedOutReviewers: []int{2, 3},
			},
			expected: true,
		},
		{
			name: "partial failure",
			stats: ReviewStats{
				TotalReviewers:      3,
				SuccessfulReviewers: 1,
				FailedReviewers:     []int{2, 3},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.AllFailed(); got != tt.expected {
				t.Errorf("AllFailed() = %v, want %v", got, tt.expected)
			}
		})
	}
}
