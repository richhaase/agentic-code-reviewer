package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
	"github.com/anthropics/agentic-code-reviewer/internal/github"
	"github.com/anthropics/agentic-code-reviewer/internal/runner"
	"github.com/anthropics/agentic-code-reviewer/internal/terminal"
)

const maxDisplayedCIChecks = 5

type prAction struct {
	body            string
	previewLabel    string
	promptTemplate  string
	successTemplate string
	skipMessage     string
	execute         func(context.Context, string, string) error
}

func handleLGTM(ctx context.Context, allFindings []domain.Finding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	// Build reviewer comments
	reviewerComments := make(map[int]string)
	for _, f := range allFindings {
		reviewerComments[f.ReviewerID] = f.Text
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, reviewerComments)

	// Check CI status and self-review before approving
	isSelfReview := false
	if !local && !autoNo {
		if !github.IsGHAvailable() {
			return domain.ExitError
		}

		prNumber := github.GetCurrentPRNumber(ctx, worktreeBranch)
		if prNumber != "" {
			// Check if this is a self-review
			isSelfReview = github.IsSelfReview(ctx, prNumber)

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
	}

	// Handle self-review: offer to post as comment instead of approving
	if isSelfReview {
		if err := confirmAndExecutePRAction(ctx, prAction{
			body:            lgtmBody,
			previewLabel:    "LGTM comment preview (self-review)",
			promptTemplate:  "You cannot approve your own PR. Post LGTM as a comment to PR #%s?",
			successTemplate: "Posted LGTM comment to PR #%s.",
			skipMessage:     "Skipped posting LGTM comment.",
			execute:         github.PostPRComment,
		}, logger); err != nil {
			return domain.ExitError
		}
		return domain.ExitNoFindings
	}

	// Preview and confirm approval (non-self-review)
	if err := confirmAndExecutePRAction(ctx, prAction{
		body:            lgtmBody,
		previewLabel:    "Approval comment preview",
		promptTemplate:  "Approve PR #%s?",
		successTemplate: "Approved PR #%s.",
		skipMessage:     "Skipped approving PR.",
		execute:         github.ApprovePR,
	}, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

func handleFindings(ctx context.Context, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting
	if !autoYes && !autoNo && terminal.IsStdoutTTY() {
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

	commentBody := runner.RenderCommentMarkdown(filteredGrouped, stats.TotalReviewers, aggregated)

	if err := confirmAndExecutePRAction(ctx, prAction{
		body:            commentBody,
		previewLabel:    "PR comment preview",
		promptTemplate:  "Post findings to PR #%s?",
		successTemplate: "Posted findings to PR #%s.",
		skipMessage:     "Skipped posting findings.",
		execute:         github.PostPRComment,
	}, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitFindings
}

func confirmAndExecutePRAction(ctx context.Context, action prAction, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR action.", terminal.StyleDim)
		return nil
	}

	if autoNo {
		logger.Log(action.skipMessage, terminal.StyleDim)
		return nil
	}

	// Preview
	fmt.Println()
	logger.Logf(terminal.StylePhase, "%s%s%s",
		terminal.Color(terminal.Bold), action.previewLabel, terminal.Color(terminal.Reset))
	fmt.Println()

	width := terminal.ReportWidth()
	divider := terminal.Ruler(width, "━")
	fmt.Println(divider)
	fmt.Println(action.body)
	fmt.Println(divider)

	if !github.IsGHAvailable() {
		return fmt.Errorf("gh not available")
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

	// Confirm
	confirmed := autoYes
	if !autoYes {
		fmt.Println()
		prompt := fmt.Sprintf(action.promptTemplate,
			fmt.Sprintf("%s#%s%s", terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset)))
		fmt.Printf("%s?%s %s %s[Y/n]:%s ",
			terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
			prompt,
			terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		confirmed = response == "" || response == "y" || response == "yes"
	}

	if !confirmed {
		logger.Log(action.skipMessage, terminal.StyleDim)
		return nil
	}

	// Execute
	if err := action.execute(ctx, prNumber, action.body); err != nil {
		logger.Logf(terminal.StyleError, "Failed: %v", err)
		return err
	}

	logger.Log(fmt.Sprintf(action.successTemplate, "#"+prNumber), terminal.StyleSuccess)
	return nil
}
