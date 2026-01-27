package runner

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxRawOutputLines = 10

// RenderReport renders a terminal report for the review results.
func RenderReport(
	grouped domain.GroupedFindings,
	summaryResult *summarizer.Result,
	stats domain.ReviewStats,
) string {
	width := terminal.ReportWidth()

	var lines []string

	// Handle summarizer errors
	if summaryResult.ExitCode != 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s✗ Summarizer Error%s", terminal.Color(terminal.Red), terminal.Color(terminal.Reset)))
		lines = append(lines, terminal.Ruler(width, "─"))
		lines = append(lines, fmt.Sprintf("  Exit code: %d", summaryResult.ExitCode))
		if summaryResult.Stderr != "" {
			lines = append(lines, fmt.Sprintf("  Stderr: %s", summaryResult.Stderr))
		}
		if summaryResult.RawOut != "" {
			lines = append(lines, fmt.Sprintf("\n  %sRaw output:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
			rawLines := strings.Split(summaryResult.RawOut, "\n")
			for i, line := range rawLines {
				if i >= maxRawOutputLines {
					break
				}
				lines = append(lines, fmt.Sprintf("  %s%s%s", terminal.Color(terminal.Dim), line, terminal.Color(terminal.Reset)))
			}
		}
		return strings.Join(lines, "\n")
	}

	// Warnings
	var warnings []string
	if stats.ParseErrors > 0 {
		warnings = append(warnings, fmt.Sprintf("JSONL parse errors: %d", stats.ParseErrors))
	}
	if len(stats.FailedReviewers) > 0 {
		warnings = append(warnings, fmt.Sprintf("Failed reviewers: %s", formatReviewersWithAgents(stats.FailedReviewers, stats.ReviewerAgentNames)))
	}
	if len(stats.TimedOutReviewers) > 0 {
		warnings = append(warnings, fmt.Sprintf("Timed out reviewers: %s", formatReviewersWithAgents(stats.TimedOutReviewers, stats.ReviewerAgentNames)))
	}

	if len(warnings) > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s⚠ Warnings%s", terminal.Color(terminal.Yellow), terminal.Color(terminal.Reset)))
		lines = append(lines, terminal.Ruler(width, "─"))
		for _, w := range warnings {
			lines = append(lines, fmt.Sprintf("  %s•%s %s", terminal.Color(terminal.Yellow), terminal.Color(terminal.Reset), w))
		}
		lines = append(lines, "")
	}

	// No findings case
	if !grouped.HasFindings() {
		lines = append(lines, fmt.Sprintf("%s✓%s %s%sLGTM%s %s(%d/%d reviewers)%s",
			terminal.Color(terminal.Green), terminal.Color(terminal.Reset),
			terminal.Color(terminal.Green), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset),
			terminal.Color(terminal.Dim), stats.SuccessfulReviewers, stats.TotalReviewers, terminal.Color(terminal.Reset)))
		return strings.Join(lines, "\n")
	}

	// Findings header
	lines = append(lines, "")
	findingWord := "finding"
	if len(grouped.Findings) != 1 {
		findingWord = "findings"
	}
	lines = append(lines, fmt.Sprintf("%s%s📋 %d %s%s",
		terminal.Color(terminal.Cyan), terminal.Color(terminal.Bold), len(grouped.Findings), findingWord, terminal.Color(terminal.Reset)))
	lines = append(lines, terminal.Ruler(width, "━"))

	// Render each finding
	for idx, finding := range grouped.Findings {
		title := finding.Title
		if title == "" {
			title = "Untitled"
		}

		lines = append(lines, "")
		confidence := ""
		if stats.TotalReviewers > 0 && finding.ReviewerCount > 0 {
			confidence = fmt.Sprintf(" %s(%d/%d reviewers)%s",
				terminal.Color(terminal.Dim), finding.ReviewerCount, stats.TotalReviewers, terminal.Color(terminal.Reset))
		}
		lines = append(lines, fmt.Sprintf("%s%s%d.%s %s%s%s%s",
			terminal.Color(terminal.Yellow), terminal.Color(terminal.Bold), idx+1, terminal.Color(terminal.Reset),
			terminal.Color(terminal.Bold), title, terminal.Color(terminal.Reset), confidence))
		lines = append(lines, terminal.Ruler(width, "─"))

		if finding.Summary != "" {
			wrapped := terminal.WrapText(finding.Summary, width-3, "   ")
			lines = append(lines, wrapped)
		}

		if len(finding.Messages) > 0 {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("   %sEvidence:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
			for _, msg := range finding.Messages {
				if msg != "" {
					wrapped := terminal.WrapText(msg, width-5, fmt.Sprintf("   %s•%s ", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))
					lines = append(lines, wrapped)
				}
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, terminal.Ruler(width, "━"))

	if stats.FPFilteredCount > 0 {
		findingWord := "finding"
		positiveWord := "positive"
		if stats.FPFilteredCount != 1 {
			findingWord = "findings"
			positiveWord = "positives"
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%sℹ %d %s filtered as likely false %s%s",
			terminal.Color(terminal.Dim), stats.FPFilteredCount, findingWord, positiveWord, terminal.Color(terminal.Reset)))
	}

	if stats.IgnoredCount > 0 {
		findingWord := "finding"
		if stats.IgnoredCount != 1 {
			findingWord = "findings"
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%sℹ %d %s skipped from .acr/ignore%s",
			terminal.Color(terminal.Dim), stats.IgnoredCount, findingWord, terminal.Color(terminal.Reset)))
	}

	if stats.WallClockDuration > 0 || len(stats.ReviewerDurations) > 0 || summaryResult.Duration > 0 {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%sTiming:%s", terminal.Color(terminal.Dim), terminal.Color(terminal.Reset)))

		if stats.WallClockDuration > 0 {
			lines = append(lines, fmt.Sprintf("  %sreviewers: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(stats.WallClockDuration), terminal.Color(terminal.Reset)))
		}

		if len(stats.ReviewerDurations) > 0 {
			durations := make([]float64, 0, len(stats.ReviewerDurations))
			for _, d := range stats.ReviewerDurations {
				durations = append(durations, d.Seconds())
			}
			slices.Sort(durations)

			var sum float64
			for _, d := range durations {
				sum += d
			}
			avg := sum / float64(len(durations))
			min := durations[0]
			max := durations[len(durations)-1]

			lines = append(lines, fmt.Sprintf("  %s  min %.1fs / avg %.1fs / max %.1fs%s",
				terminal.Color(terminal.Dim), min, avg, max, terminal.Color(terminal.Reset)))
		}

		if summaryResult.Duration > 0 {
			lines = append(lines, fmt.Sprintf("  %ssummarizer: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(summaryResult.Duration), terminal.Color(terminal.Reset)))
		}

		if stats.FPFilterDuration > 0 {
			lines = append(lines, fmt.Sprintf("  %sfp-filter: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(stats.FPFilterDuration), terminal.Color(terminal.Reset)))
		}

		if stats.WallClockDuration > 0 && summaryResult.Duration > 0 {
			total := stats.WallClockDuration + summaryResult.Duration + stats.FPFilterDuration
			lines = append(lines, fmt.Sprintf("  %stotal: %s%s",
				terminal.Color(terminal.Dim), terminal.FormatDuration(total), terminal.Color(terminal.Reset)))
		}
	}

	return strings.Join(lines, "\n")
}

// RenderCommentMarkdown renders GitHub comment markdown for findings.
func RenderCommentMarkdown(
	grouped domain.GroupedFindings,
	totalReviewers int,
	aggregated []domain.AggregatedFinding,
) string {
	var lines []string
	lines = append(lines, "## Findings")

	for idx, finding := range grouped.Findings {
		title := finding.Title
		if title == "" {
			title = "Untitled"
		}

		confidence := ""
		if finding.ReviewerCount > 0 {
			confidence = fmt.Sprintf(" (%d/%d reviewers)", finding.ReviewerCount, totalReviewers)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%d. **%s**%s", idx+1, title, confidence))

		if finding.Summary != "" {
			lines = append(lines, "")
			lines = append(lines, finding.Summary)
		}

		if len(finding.Messages) > 0 {
			lines = append(lines, "")
			lines = append(lines, "Evidence:")
			for _, msg := range finding.Messages {
				if msg != "" {
					lines = append(lines, fmt.Sprintf("- %s", msg))
				}
			}
		}
	}

	// Raw findings section
	rawIndices := collectSourceIndices(grouped.Findings)
	rawSection := formatRawFindings(aggregated, rawIndices, totalReviewers)
	if rawSection != "" {
		lines = append(lines, "")
		lines = append(lines, "_Expand for verbatim findings._")
		lines = append(lines, "<details>")
		lines = append(lines, "<summary>Raw findings (verbatim)</summary>")
		lines = append(lines, "")
		lines = append(lines, rawSection)
		lines = append(lines, "</details>")
	}

	return strings.Join(lines, "\n")
}

// RenderLGTMMarkdown renders approval comment markdown.
func RenderLGTMMarkdown(totalReviewers, successfulReviewers int, reviewerComments map[int]string) string {
	var lines []string
	lines = append(lines, "## LGTM :white_check_mark:")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("**%d of %d reviewers found no issues.**", successfulReviewers, totalReviewers))

	if len(reviewerComments) > 0 {
		lines = append(lines, "")
		lines = append(lines, "<details>")
		lines = append(lines, "<summary>Reviewer comments</summary>")
		lines = append(lines, "")

		keys := make([]int, 0, len(reviewerComments))
		for k := range reviewerComments {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, id := range keys {
			lines = append(lines, fmt.Sprintf("- **Reviewer %d:** %s", id, reviewerComments[id]))
		}
		lines = append(lines, "")
		lines = append(lines, "</details>")
	}

	return strings.Join(lines, "\n")
}

func collectSourceIndices(groups []domain.FindingGroup) []int {
	seen := make(map[int]bool)
	var indices []int
	for _, g := range groups {
		for _, src := range g.Sources {
			if !seen[src] {
				seen[src] = true
				indices = append(indices, src)
			}
		}
	}
	return indices
}

func formatRawFindings(aggregated []domain.AggregatedFinding, indices []int, totalReviewers int) string {
	if len(indices) == 0 {
		return ""
	}

	var lines []string
	for idx, src := range indices {
		if src < 0 || src >= len(aggregated) {
			continue
		}
		entry := aggregated[src]
		reviewerCount := len(entry.Reviewers)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%d. (%d/%d reviewers)", idx+1, reviewerCount, totalReviewers))
		lines = append(lines, "```")
		lines = append(lines, strings.TrimRight(entry.Text, " \n"))
		lines = append(lines, "```")
	}

	return strings.Join(lines, "\n")
}

func joinInts(nums []int) string {
	strs := make([]string, len(nums))
	for i, n := range nums {
		strs[i] = strconv.Itoa(n)
	}
	return strings.Join(strs, ", ")
}

// formatReviewersWithAgents formats reviewer IDs with their agent names.
// Example: "#1 (codex), #3 (claude)"
func formatReviewersWithAgents(reviewerIDs []int, agentNames map[int]string) string {
	strs := make([]string, len(reviewerIDs))
	for i, id := range reviewerIDs {
		if name, ok := agentNames[id]; ok && name != "" {
			strs[i] = fmt.Sprintf("#%d (%s)", id, name)
		} else {
			strs[i] = fmt.Sprintf("#%d", id)
		}
	}
	return strings.Join(strs, ", ")
}
