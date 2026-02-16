package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestCollectSourceIndices_DeduplicatesAcrossGroups(t *testing.T) {
	groups := []domain.FindingGroup{
		{Sources: []int{1, 2, 3}},
		{Sources: []int{2, 3, 4}}, // 2 and 3 are duplicates
		{Sources: []int{4, 5}},    // 4 is duplicate
	}

	indices := collectSourceIndices(groups)

	// Should have 5 unique indices: 1, 2, 3, 4, 5
	if len(indices) != 5 {
		t.Errorf("expected 5 unique indices, got %d: %v", len(indices), indices)
	}

	seen := make(map[int]bool)
	for _, idx := range indices {
		if seen[idx] {
			t.Errorf("duplicate index found: %d", idx)
		}
		seen[idx] = true
	}
}

func TestCollectSourceIndices_EmptyGroups(t *testing.T) {
	indices := collectSourceIndices(nil)
	if len(indices) != 0 {
		t.Errorf("expected empty for nil input, got %v", indices)
	}

	indices = collectSourceIndices([]domain.FindingGroup{})
	if len(indices) != 0 {
		t.Errorf("expected empty for empty input, got %v", indices)
	}
}

func TestFormatRawFindings_ReturnsEmptyForNoIndices(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "Finding 1", Reviewers: []int{1, 2}},
	}

	result := formatRawFindings(aggregated, nil, 5)
	if result != "" {
		t.Errorf("expected empty string for nil indices, got %q", result)
	}

	result = formatRawFindings(aggregated, []int{}, 5)
	if result != "" {
		t.Errorf("expected empty string for empty indices, got %q", result)
	}
}

func TestFormatRawFindings_SkipsOutOfBoundsIndices(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "Finding 0", Reviewers: []int{1}},
		{Text: "Finding 1", Reviewers: []int{2}},
	}

	// Index 5 is out of bounds, -1 is invalid
	result := formatRawFindings(aggregated, []int{0, 5, -1, 1}, 3)

	// Should only include indices 0 and 1
	if !strings.Contains(result, "Finding 0") {
		t.Error("expected 'Finding 0' in output")
	}
	if !strings.Contains(result, "Finding 1") {
		t.Error("expected 'Finding 1' in output")
	}
}

func TestFormatRawFindings_FormatsAsMarkdownCodeBlocks(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "This is a finding", Reviewers: []int{1, 2}},
	}

	result := formatRawFindings(aggregated, []int{0}, 5)

	if !strings.Contains(result, "```") {
		t.Error("expected markdown code blocks")
	}
	if !strings.Contains(result, "This is a finding") {
		t.Error("expected finding text in output")
	}
	if !strings.Contains(result, "(2/5 reviewers)") {
		t.Error("expected reviewer count in output")
	}
}

func TestFormatRawFindings_TrimsTrailingWhitespace(t *testing.T) {
	aggregated := []domain.AggregatedFinding{
		{Text: "Finding with trailing space   \n\n", Reviewers: []int{1}},
	}

	result := formatRawFindings(aggregated, []int{0}, 3)

	// Text should be trimmed of trailing whitespace
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.HasSuffix(line, "   ") {
			t.Errorf("line has trailing spaces: %q", line)
		}
	}
}

func TestRenderLGTMMarkdown_BasicFormat(t *testing.T) {
	result := RenderLGTMMarkdown(5, 5, nil, "dev")

	if !strings.Contains(result, "LGTM") {
		t.Error("expected 'LGTM' in output")
	}
	if !strings.Contains(result, "5 of 5 reviewers") {
		t.Error("expected reviewer count in output")
	}
	if !strings.Contains(result, "no issues") {
		t.Error("expected 'no issues' in output")
	}
}

func TestRenderLGTMMarkdown_WithComments(t *testing.T) {
	comments := map[int][]AnnotatedComment{
		2: {{Text: "Minor style note"}},
		1: {{Text: "Code looks clean"}},
	}

	result := RenderLGTMMarkdown(3, 3, comments, "dev")

	if !strings.Contains(result, "Reviewer comments") {
		t.Error("expected 'Reviewer comments' section")
	}
	if !strings.Contains(result, "<details>") {
		t.Error("expected collapsible details section")
	}
	if !strings.Contains(result, "Minor style note") {
		t.Error("expected reviewer 2 comment")
	}
	if !strings.Contains(result, "Code looks clean") {
		t.Error("expected reviewer 1 comment")
	}
}

func TestRenderLGTMMarkdown_SortsCommentsByReviewerID(t *testing.T) {
	comments := map[int][]AnnotatedComment{
		3: {{Text: "Third"}},
		1: {{Text: "First"}},
		2: {{Text: "Second"}},
	}

	result := RenderLGTMMarkdown(3, 3, comments, "dev")

	// Reviewer 1 should appear before Reviewer 2, which should appear before Reviewer 3
	idx1 := strings.Index(result, "Reviewer 1")
	idx2 := strings.Index(result, "Reviewer 2")
	idx3 := strings.Index(result, "Reviewer 3")

	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatal("missing reviewer entries")
	}
	if idx1 > idx2 || idx2 > idx3 {
		t.Error("reviewers should be sorted by ID")
	}
}

func TestRenderLGTMMarkdown_WithDispositionAnnotations(t *testing.T) {
	comments := map[int][]AnnotatedComment{
		1: {
			{
				Text:        "Possible null deref",
				Disposition: domain.Disposition{Kind: domain.DispositionInfo, GroupTitle: "Style"},
			},
		},
		2: {
			{
				Text:        "Missing error check",
				Disposition: domain.Disposition{Kind: domain.DispositionFilteredFP, FPScore: 82},
			},
		},
		3: {
			{
				Text:        "Unused import",
				Disposition: domain.Disposition{Kind: domain.DispositionFilteredExclude},
			},
		},
	}

	result := RenderLGTMMarkdown(3, 3, comments, "dev")

	if !strings.Contains(result, "Categorized as informational during summarization") {
		t.Error("expected info disposition annotation")
	}
	if !strings.Contains(result, "Filtered as likely false positive (score 82)") {
		t.Error("expected FP disposition annotation")
	}
	if !strings.Contains(result, "Filtered by exclude pattern") {
		t.Error("expected exclude disposition annotation")
	}
}

func TestRenderLGTMMarkdown_MultipleCommentsPerReviewer(t *testing.T) {
	comments := map[int][]AnnotatedComment{
		1: {
			{Text: "First comment"},
			{Text: "Second comment"},
		},
	}

	result := RenderLGTMMarkdown(3, 3, comments, "dev")

	if strings.Count(result, "Reviewer 1") != 2 {
		t.Errorf("expected 2 entries for Reviewer 1, got %d", strings.Count(result, "Reviewer 1"))
	}
	if !strings.Contains(result, "First comment") {
		t.Error("expected first comment")
	}
	if !strings.Contains(result, "Second comment") {
		t.Error("expected second comment")
	}
}

func TestRenderLGTMMarkdown_UnmappedDispositionNoAnnotation(t *testing.T) {
	comments := map[int][]AnnotatedComment{
		1: {
			{
				Text:        "Some comment",
				Disposition: domain.Disposition{Kind: domain.DispositionUnmapped},
			},
		},
	}

	result := RenderLGTMMarkdown(3, 3, comments, "dev")

	// Unmapped should render as plain comment without annotation
	if !strings.Contains(result, "- **Reviewer 1:** Some comment") {
		t.Error("expected plain comment without annotation")
	}
	// Should not contain any disposition annotations
	for _, annotation := range []string{"informational", "false positive", "exclude pattern", "Survived"} {
		if strings.Contains(result, annotation) {
			t.Errorf("unexpected annotation text %q in output", annotation)
		}
	}
}

func TestRenderCommentMarkdown_BasicFormat(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{
				Title:         "Security Issue",
				Summary:       "Found a potential SQL injection",
				ReviewerCount: 3,
			},
		},
	}

	result := RenderCommentMarkdown(grouped, 5, nil, "dev")

	if !strings.Contains(result, "## Findings") {
		t.Error("expected '## Findings' header")
	}
	if !strings.Contains(result, "Security Issue") {
		t.Error("expected finding title")
	}
	if !strings.Contains(result, "SQL injection") {
		t.Error("expected finding summary")
	}
	if !strings.Contains(result, "(3/5 reviewers)") {
		t.Error("expected reviewer count")
	}
}

func TestRenderCommentMarkdown_NumbersFindings(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "First"},
			{Title: "Second"},
			{Title: "Third"},
		},
	}

	result := RenderCommentMarkdown(grouped, 3, nil, "dev")

	if !strings.Contains(result, "1. **First**") {
		t.Error("expected first finding to be numbered 1")
	}
	if !strings.Contains(result, "2. **Second**") {
		t.Error("expected second finding to be numbered 2")
	}
	if !strings.Contains(result, "3. **Third**") {
		t.Error("expected third finding to be numbered 3")
	}
}

func TestRenderCommentMarkdown_UntitledFindingsFallback(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: ""}, // empty title
		},
	}

	result := RenderCommentMarkdown(grouped, 3, nil, "dev")

	if !strings.Contains(result, "**Untitled**") {
		t.Error("expected 'Untitled' fallback for empty title")
	}
}

func TestRenderCommentMarkdown_IncludesEvidence(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{
				Title:    "Issue",
				Messages: []string{"Evidence 1", "", "Evidence 2"}, // includes empty
			},
		},
	}

	result := RenderCommentMarkdown(grouped, 3, nil, "dev")

	if !strings.Contains(result, "Evidence:") {
		t.Error("expected 'Evidence:' label")
	}
	if !strings.Contains(result, "- Evidence 1") {
		t.Error("expected evidence item 1")
	}
	if !strings.Contains(result, "- Evidence 2") {
		t.Error("expected evidence item 2")
	}
	// Empty evidence should be skipped
	lineCount := strings.Count(result, "- Evidence")
	if lineCount != 2 {
		t.Errorf("expected 2 evidence items (empty skipped), got %d", lineCount)
	}
}

func TestRenderCommentMarkdown_IncludesRawSection(t *testing.T) {
	grouped := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Issue", Sources: []int{0}},
		},
	}
	aggregated := []domain.AggregatedFinding{
		{Text: "Raw finding text", Reviewers: []int{1, 2}},
	}

	result := RenderCommentMarkdown(grouped, 5, aggregated, "dev")

	if !strings.Contains(result, "<details>") {
		t.Error("expected collapsible details section")
	}
	if !strings.Contains(result, "Raw findings") {
		t.Error("expected 'Raw findings' in summary")
	}
	if !strings.Contains(result, "Raw finding text") {
		t.Error("expected raw finding text")
	}
}

func TestRenderReport_SummarizerError(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{}
		summaryResult := &summarizer.Result{
			ExitCode: 1,
			Stderr:   "something went wrong",
			RawOut:   "line1\nline2",
		}
		stats := domain.ReviewStats{}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "Summarizer Error") {
			t.Error("expected 'Summarizer Error' in output")
		}
		if !strings.Contains(result, "Exit code: 1") {
			t.Error("expected exit code in output")
		}
		if !strings.Contains(result, "something went wrong") {
			t.Error("expected stderr in output")
		}
		if !strings.Contains(result, "line1") {
			t.Error("expected raw output in output")
		}
	})
}

func TestRenderReport_LGTM(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{} // no findings
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "LGTM") {
			t.Error("expected 'LGTM' in output")
		}
	})
}

func TestRenderReport_WithWarnings(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{}
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{
			ParseErrors:       3,
			FailedReviewers:   []int{2, 4},
			TimedOutReviewers: []int{5},
			ReviewerAgentNames: map[int]string{
				2: "codex",
				4: "claude",
				5: "gemini",
			},
		}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "Warnings") {
			t.Error("expected 'Warnings' section")
		}
		if !strings.Contains(result, "JSONL parse errors: 3") {
			t.Error("expected parse error count")
		}
		if !strings.Contains(result, "Failed reviewers: #2 (codex), #4 (claude)") {
			t.Error("expected failed reviewers with agent names")
		}
		if !strings.Contains(result, "Timed out reviewers: #5 (gemini)") {
			t.Error("expected timed out reviewers with agent names")
		}
	})
}

func TestRenderReport_WithFindings(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{
			Findings: []domain.FindingGroup{
				{
					Title:         "Security Issue",
					Summary:       "Found a vulnerability",
					Messages:      []string{"Details here"},
					ReviewerCount: 3,
				},
			},
		}
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{TotalReviewers: 5}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "1 finding") {
			t.Error("expected '1 finding' header")
		}
		if !strings.Contains(result, "Security Issue") {
			t.Error("expected finding title")
		}
		if !strings.Contains(result, "Found a vulnerability") {
			t.Error("expected finding summary")
		}
		if !strings.Contains(result, "Evidence") {
			t.Error("expected evidence section")
		}
		if !strings.Contains(result, "(3/5 reviewers)") {
			t.Error("expected reviewer count")
		}
	})
}

func TestRenderReport_MultipleFindings(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{
			Findings: []domain.FindingGroup{
				{Title: "Issue 1"},
				{Title: "Issue 2"},
				{Title: "Issue 3"},
			},
		}
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "3 findings") {
			t.Error("expected '3 findings' header (plural)")
		}
		if !strings.Contains(result, "1.") || !strings.Contains(result, "2.") || !strings.Contains(result, "3.") {
			t.Error("expected numbered findings")
		}
	})
}

func TestRenderReport_UntitledFinding(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{
			Findings: []domain.FindingGroup{
				{Title: ""}, // empty title
			},
		}
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "Untitled") {
			t.Error("expected 'Untitled' fallback")
		}
	})
}

func TestRenderReport_WithTimingStats(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{
			Findings: []domain.FindingGroup{{Title: "Issue"}},
		}
		summaryResult := &summarizer.Result{
			ExitCode: 0,
			Duration: 5 * time.Second,
		}
		stats := domain.ReviewStats{
			WallClockDuration: 30 * time.Second,
			ReviewerDurations: map[int]time.Duration{
				1: 10 * time.Second,
				2: 20 * time.Second,
			},
		}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "Timing") {
			t.Error("expected 'Timing' section")
		}
		if !strings.Contains(result, "reviewers:") {
			t.Error("expected reviewers duration")
		}
		if !strings.Contains(result, "summarizer:") {
			t.Error("expected summarizer duration")
		}
		if !strings.Contains(result, "min") && !strings.Contains(result, "avg") && !strings.Contains(result, "max") {
			t.Error("expected min/avg/max stats")
		}
	})
}

func TestRenderReport_WithAuthFailedWarning(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		grouped := domain.GroupedFindings{}
		summaryResult := &summarizer.Result{ExitCode: 0}
		stats := domain.ReviewStats{
			AuthFailedReviewers: []int{2},
			ReviewerAgentNames: map[int]string{
				2: "gemini",
			},
		}

		result := RenderReport(grouped, summaryResult, stats)

		if !strings.Contains(result, "Warnings") {
			t.Error("expected 'Warnings' section")
		}
		if !strings.Contains(result, "Auth failed") {
			t.Error("expected 'Auth failed' in warnings")
		}
		if !strings.Contains(result, "#2 (gemini)") {
			t.Error("expected reviewer ID with agent name")
		}
	})
}

func TestRenderDismissedLGTMMarkdown_BasicFormat(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Potential nil dereference"},
		{Title: "Missing error check"},
	}
	stats := domain.ReviewStats{TotalReviewers: 5, SuccessfulReviewers: 3}

	result := RenderDismissedLGTMMarkdown(findings, stats, "dev")

	if !strings.Contains(result, "LGTM") {
		t.Error("expected 'LGTM' in output")
	}
	if !strings.Contains(result, "All findings dismissed after human review") {
		t.Error("expected 'All findings dismissed after human review' phrasing")
	}
	if !strings.Contains(result, "2 findings were reviewed and dismissed") {
		t.Error("expected dismissed count")
	}
	if !strings.Contains(result, "Potential nil dereference") {
		t.Error("expected first finding title")
	}
	if !strings.Contains(result, "Missing error check") {
		t.Error("expected second finding title")
	}
	if !strings.Contains(result, "<details>") {
		t.Error("expected collapsible details section")
	}
}

func TestRenderDismissedLGTMMarkdown_SingleFinding(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Minor style issue"},
	}
	stats := domain.ReviewStats{TotalReviewers: 3, SuccessfulReviewers: 3}

	result := RenderDismissedLGTMMarkdown(findings, stats, "dev")

	if !strings.Contains(result, "1 finding was reviewed and dismissed") {
		t.Error("expected singular 'finding was'")
	}
}

func TestRenderDismissedLGTMMarkdown_UntitledFallback(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: ""},
	}
	stats := domain.ReviewStats{TotalReviewers: 2, SuccessfulReviewers: 2}

	result := RenderDismissedLGTMMarkdown(findings, stats, "dev")

	if !strings.Contains(result, "Untitled") {
		t.Error("expected 'Untitled' fallback for empty title")
	}
}
