package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxDisplayedCIChecks = 5

func handleLGTM(ctx context.Context, allFindings []domain.Finding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	// Build reviewer comments
	reviewerComments := make(map[int]string)
	for _, f := range allFindings {
		reviewerComments[f.ReviewerID] = f.Text
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, reviewerComments)

	// Check for self-review (always when not local - needed for correct path even in autoNo mode)
	isSelfReview := false
	var prNumber string
	if !local && github.IsGHAvailable() {
		prNumber = github.GetCurrentPRNumber(ctx, worktreeBranch)
		if prNumber != "" {
			isSelfReview = github.IsSelfReview(ctx, prNumber)
		}
	}

	// Check CI status before approving (skip in autoNo mode since we won't approve anyway)
	if !local && !autoNo && prNumber != "" {
		ciStatus := github.CheckCIStatus(ctx, prNumber)

		if ciStatus.Error != "" {
			logger.Logf(terminal.StyleError, "Failed to check CI status: %s", ciStatus.Error)
			return domain.ExitError
		}

		if !ciStatus.AllPassed {
			logger.Logf(terminal.StyleSuccess, "%s%sLGTM%s - No issues found by reviewers.",
				terminal.Color(terminal.Green), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
			fmt.Println()

			if len(ciStatus.Failed) > 0 {
				logger.Logf(terminal.StyleError, "Cannot approve PR: %d CI check(s) failed", len(ciStatus.Failed))
				for i, check := range ciStatus.Failed {
					if i >= maxDisplayedCIChecks {
						logger.Logf(terminal.StyleDim, "  ... and %d more", len(ciStatus.Failed)-maxDisplayedCIChecks)
						break
					}
					logger.Logf(terminal.StyleDim, "  • %s", check)
				}
			}
			if len(ciStatus.Pending) > 0 {
				logger.Logf(terminal.StyleWarning, "Cannot approve PR: %d CI check(s) pending", len(ciStatus.Pending))
				for i, check := range ciStatus.Pending {
					if i >= maxDisplayedCIChecks {
						logger.Logf(terminal.StyleDim, "  ... and %d more", len(ciStatus.Pending)-maxDisplayedCIChecks)
						break
					}
					logger.Logf(terminal.StyleDim, "  • %s", check)
				}
			}

			return domain.ExitNoFindings
		}
	}

	if err := confirmAndSubmitLGTM(ctx, lgtmBody, isSelfReview, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

func handleFindings(ctx context.Context, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting (skip in local mode)
	if !local && !autoYes && !autoNo && terminal.IsStdoutTTY() {
		indices, canceled, err := terminal.RunSelector(grouped.Findings)
		if err != nil {
			logger.Logf(terminal.StyleError, "Selector error: %v", err)
			return domain.ExitError
		}
		if canceled {
			logger.Log("Skipped posting findings.", terminal.StyleDim)
			return domain.ExitFindings
		}
		selectedFindings = filterFindingsByIndices(grouped.Findings, indices)

		if len(selectedFindings) == 0 {
			logger.Log("No findings selected to post.", terminal.StyleDim)
			return domain.ExitFindings
		}
	}

	// Create filtered GroupedFindings for rendering
	filteredGrouped := domain.GroupedFindings{
		Findings: selectedFindings,
		Info:     grouped.Info,
	}

	reviewBody := runner.RenderCommentMarkdown(filteredGrouped, stats.TotalReviewers, aggregated)

	if err := confirmAndSubmitReview(ctx, reviewBody, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitFindings
}

func confirmAndSubmitReview(ctx context.Context, body string, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR review.", terminal.StyleDim)
		return nil
	}

	if autoNo {
		logger.Log("Skipped posting review.", terminal.StyleDim)
		return nil
	}

	// Preview
	fmt.Println()
	logger.Logf(terminal.StylePhase, "%sPR review preview%s",
		terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
	fmt.Println()

	width := terminal.ReportWidth()
	divider := terminal.Ruler(width, "━")
	fmt.Println(divider)
	fmt.Println(body)
	fmt.Println(divider)

	if err := github.CheckGHAvailable(); err != nil {
		return err
	}

	prNumber := github.GetCurrentPRNumber(ctx, worktreeBranch)
	if prNumber == "" {
		branchDesc := "current branch"
		if worktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", worktreeBranch)
		}
		logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
		return nil
	}

	// Determine review type
	requestChanges := true // default
	if !autoYes {
		fmt.Println()
		fmt.Printf("%s?%s Post review to PR %s#%s%s? %s[R]equest changes / [C]omment / [S]kip:%s ",
			terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
			terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset),
			terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		switch response {
		case "", "r":
			requestChanges = true
		case "c":
			requestChanges = false
		case "s", "n":
			logger.Log("Skipped posting review.", terminal.StyleDim)
			return nil
		default:
			logger.Log("Skipped posting review.", terminal.StyleDim)
			return nil
		}
	}

	// Execute
	if err := github.SubmitPRReview(ctx, prNumber, body, requestChanges); err != nil {
		logger.Logf(terminal.StyleError, "Failed: %v", err)
		return err
	}

	reviewType := "request changes"
	if !requestChanges {
		reviewType = "comment"
	}
	logger.Logf(terminal.StyleSuccess, "Posted %s review to PR #%s.", reviewType, prNumber)
	return nil
}

func confirmAndSubmitLGTM(ctx context.Context, body string, isSelfReview bool, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR approval.", terminal.StyleDim)
		return nil
	}

	if autoNo {
		logger.Log("Skipped posting LGTM.", terminal.StyleDim)
		return nil
	}

	// Preview
	fmt.Println()
	previewLabel := "LGTM approval preview"
	if isSelfReview {
		previewLabel = "LGTM review preview (self-review)"
	}
	logger.Logf(terminal.StylePhase, "%s%s%s",
		terminal.Color(terminal.Bold), previewLabel, terminal.Color(terminal.Reset))
	fmt.Println()

	width := terminal.ReportWidth()
	divider := terminal.Ruler(width, "━")
	fmt.Println(divider)
	fmt.Println(body)
	fmt.Println(divider)

	if err := github.CheckGHAvailable(); err != nil {
		return err
	}

	prNumber := github.GetCurrentPRNumber(ctx, worktreeBranch)
	if prNumber == "" {
		branchDesc := "current branch"
		if worktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", worktreeBranch)
		}
		logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
		return nil
	}

	// Determine action type
	type lgtmAction int
	const (
		actionApprove lgtmAction = iota
		actionComment
		actionSkip
	)

	action := actionApprove // default for non-self-review
	if isSelfReview {
		action = actionComment // default for self-review (can't approve own PR)
	}

	if !autoYes {
		fmt.Println()
		if isSelfReview {
			// Self-review: can only comment or skip
			fmt.Printf("%s?%s You cannot approve your own PR. Post LGTM review to PR %s#%s%s? %s[C]omment / [S]kip:%s ",
				terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
				terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		} else {
			// Non-self-review: approve, comment, or skip
			fmt.Printf("%s?%s Post LGTM to PR %s#%s%s? %s[A]pprove / [C]omment / [S]kip:%s ",
				terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
				terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		}

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if isSelfReview {
			switch response {
			case "", "c":
				action = actionComment
			case "s", "n":
				action = actionSkip
			default:
				action = actionSkip
			}
		} else {
			switch response {
			case "", "a":
				action = actionApprove
			case "c":
				action = actionComment
			case "s", "n":
				action = actionSkip
			default:
				action = actionSkip
			}
		}
	}

	// Execute
	switch action {
	case actionSkip:
		logger.Log("Skipped posting LGTM.", terminal.StyleDim)
		return nil
	case actionApprove:
		if err := github.ApprovePR(ctx, prNumber, body); err != nil {
			logger.Logf(terminal.StyleError, "Failed: %v", err)
			return err
		}
		logger.Logf(terminal.StyleSuccess, "Approved PR #%s.", prNumber)
	case actionComment:
		if err := github.SubmitPRReview(ctx, prNumber, body, false); err != nil {
			logger.Logf(terminal.StyleError, "Failed: %v", err)
			return err
		}
		logger.Logf(terminal.StyleSuccess, "Posted LGTM review to PR #%s.", prNumber)
	}

	return nil
}
