package runner

import (
	"testing"
	"time"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

func TestBuildStats_CategorizesResults(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Duration: 10 * time.Second},
		{ReviewerID: 2, ExitCode: 1, Duration: 15 * time.Second},
		{ReviewerID: 3, TimedOut: true, ExitCode: -1, Duration: 30 * time.Second},
		{ReviewerID: 4, ExitCode: 0, Duration: 12 * time.Second, ParseErrors: 2},
	}

	stats := BuildStats(results, 4, 35*time.Second)

	if stats.SuccessfulReviewers != 2 {
		t.Errorf("expected 2 successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.FailedReviewers) != 1 || stats.FailedReviewers[0] != 2 {
		t.Errorf("expected FailedReviewers=[2], got %v", stats.FailedReviewers)
	}
	if len(stats.TimedOutReviewers) != 1 || stats.TimedOutReviewers[0] != 3 {
		t.Errorf("expected TimedOutReviewers=[3], got %v", stats.TimedOutReviewers)
	}
	if stats.ParseErrors != 2 {
		t.Errorf("expected 2 parse errors, got %d", stats.ParseErrors)
	}
}

func TestBuildStats_TracksReviewerDurations(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Duration: 10 * time.Second},
		{ReviewerID: 2, ExitCode: 0, Duration: 20 * time.Second},
	}

	stats := BuildStats(results, 2, 25*time.Second)

	if len(stats.ReviewerDurations) != 2 {
		t.Fatalf("expected 2 duration entries, got %d", len(stats.ReviewerDurations))
	}
	if stats.ReviewerDurations[1] != 10*time.Second {
		t.Errorf("reviewer 1 duration: expected 10s, got %v", stats.ReviewerDurations[1])
	}
	if stats.ReviewerDurations[2] != 20*time.Second {
		t.Errorf("reviewer 2 duration: expected 20s, got %v", stats.ReviewerDurations[2])
	}
	if stats.WallClockDuration != 25*time.Second {
		t.Errorf("wall clock: expected 25s, got %v", stats.WallClockDuration)
	}
}

func TestBuildStats_AggregatesParseErrors(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, ParseErrors: 3},
		{ReviewerID: 2, ExitCode: 0, ParseErrors: 5},
		{ReviewerID: 3, ExitCode: 0, ParseErrors: 0},
	}

	stats := BuildStats(results, 3, time.Second)

	if stats.ParseErrors != 8 {
		t.Errorf("expected total 8 parse errors, got %d", stats.ParseErrors)
	}
}

func TestBuildStats_EmptyResults(t *testing.T) {
	stats := BuildStats(nil, 0, 0)

	if stats.SuccessfulReviewers != 0 {
		t.Errorf("expected 0 successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.FailedReviewers) != 0 {
		t.Errorf("expected no failures, got %v", stats.FailedReviewers)
	}
	if len(stats.TimedOutReviewers) != 0 {
		t.Errorf("expected no timeouts, got %v", stats.TimedOutReviewers)
	}
}

func TestBuildStats_TimeoutTakesPrecedenceOverExitCode(t *testing.T) {
	// When TimedOut is true, the reviewer should be categorized as timed out
	// regardless of exit code
	results := []domain.ReviewerResult{
		{ReviewerID: 1, TimedOut: true, ExitCode: 0}, // timed out but exit 0
		{ReviewerID: 2, TimedOut: true, ExitCode: 1}, // timed out with non-zero
	}

	stats := BuildStats(results, 2, time.Second)

	if stats.SuccessfulReviewers != 0 {
		t.Errorf("timed out reviewers should not count as successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.TimedOutReviewers) != 2 {
		t.Errorf("expected 2 timed out, got %v", stats.TimedOutReviewers)
	}
}

func TestCollectFindings_FlattensFromAllReviewers(t *testing.T) {
	results := []domain.ReviewerResult{
		{
			ReviewerID: 1,
			Findings: []domain.Finding{
				{Text: "Issue A", ReviewerID: 1},
				{Text: "Issue B", ReviewerID: 1},
			},
		},
		{
			ReviewerID: 2,
			Findings: []domain.Finding{
				{Text: "Issue C", ReviewerID: 2},
			},
		},
		{
			ReviewerID: 3,
			Findings:   nil, // no findings
		},
	}

	findings := CollectFindings(results)

	if len(findings) != 3 {
		t.Fatalf("expected 3 total findings, got %d", len(findings))
	}

	texts := map[string]bool{}
	for _, f := range findings {
		texts[f.Text] = true
	}
	if !texts["Issue A"] || !texts["Issue B"] || !texts["Issue C"] {
		t.Errorf("missing expected findings, got %v", findings)
	}
}

func TestCollectFindings_EmptyResults(t *testing.T) {
	findings := CollectFindings(nil)
	if len(findings) != 0 {
		t.Errorf("expected empty findings for nil input, got %d", len(findings))
	}

	findings = CollectFindings([]domain.ReviewerResult{})
	if len(findings) != 0 {
		t.Errorf("expected empty findings for empty input, got %d", len(findings))
	}
}

func TestCollectFindings_PreservesReviewerIDs(t *testing.T) {
	results := []domain.ReviewerResult{
		{
			ReviewerID: 5,
			Findings: []domain.Finding{
				{Text: "Finding from 5", ReviewerID: 5},
			},
		},
		{
			ReviewerID: 10,
			Findings: []domain.Finding{
				{Text: "Finding from 10", ReviewerID: 10},
			},
		},
	}

	findings := CollectFindings(results)

	reviewerIDs := map[int]bool{}
	for _, f := range findings {
		reviewerIDs[f.ReviewerID] = true
	}
	if !reviewerIDs[5] || !reviewerIDs[10] {
		t.Errorf("reviewer IDs not preserved, found: %v", reviewerIDs)
	}
}
