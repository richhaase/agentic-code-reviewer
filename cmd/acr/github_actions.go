package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxDisplayedCIChecks = 5

// prContext holds PR number and self-review status for GitHub operations.
type prContext struct {
	number       string
	isSelfReview bool
	err          error // non-nil if PR lookup failed (distinguishes auth errors from "no PR")
}

// getPRContext retrieves PR number and self-review status for the current branch.
func getPRContext(ctx context.Context) prContext {
	if local || !github.IsGHAvailable() {
		return prContext{}
	}
	prNumber, err := github.GetCurrentPRNumber(ctx, worktreeBranch)
	if err != nil {
		return prContext{err: err}
	}
	return prContext{
		number:       prNumber,
		isSelfReview: github.IsSelfReview(ctx, prNumber),
	}
}

// checkPRAvailable verifies gh CLI is available and PR exists.
// Returns error if gh CLI unavailable or auth failed, true if PR exists, false if no PR found.
func checkPRAvailable(pr prContext, logger *terminal.Logger) (bool, error) {
	if err := github.CheckGHAvailable(); err != nil {
		return false, err
	}

	if pr.err != nil {
		if errors.Is(pr.err, github.ErrAuthFailed) {
			logger.Logf(terminal.StyleError, "GitHub authentication failed. Run 'gh auth login' to authenticate.")
			return false, pr.err
		}
		if errors.Is(pr.err, github.ErrNoPRFound) {
			branchDesc := "current branch"
			if worktreeBranch != "" {
				branchDesc = fmt.Sprintf("branch '%s'", worktreeBranch)
			}
			logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
			return false, nil
		}
		logger.Logf(terminal.StyleError, "Failed to check PR: %v", pr.err)
		return false, pr.err
	}

	if pr.number == "" {
		branchDesc := "current branch"
		if worktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", worktreeBranch)
		}
		logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
		return false, nil
	}
	return true, nil
}

// readUserInput reads a line from stdin, returning empty string on error.
func readUserInput() string {
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(response))
}

// formatPrompt creates a colored prompt string for user input.
func formatPrompt(question, options string) string {
	return fmt.Sprintf("%s?%s %s %s%s%s ",
		terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
		question,
		terminal.Color(terminal.Dim), options, terminal.Color(terminal.Reset))
}

// formatPRRef creates a bold PR reference like "#123".
func formatPRRef(prNumber string) string {
	return fmt.Sprintf("%s#%s%s", terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))
}

func handleLGTM(ctx context.Context, allFindings []domain.Finding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	reviewerComments := make(map[int]string)
	for _, f := range allFindings {
		reviewerComments[f.ReviewerID] = f.Text
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, reviewerComments)
	pr := getPRContext(ctx)

	if err := confirmAndSubmitLGTM(ctx, lgtmBody, pr, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

func handleFindings(ctx context.Context, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting (skip in local mode)
	if !local && !autoYes && terminal.IsStdoutTTY() {
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

	pr := getPRContext(ctx)

	filteredGrouped := domain.GroupedFindings{
		Findings: selectedFindings,
		Info:     grouped.Info,
	}
	reviewBody := runner.RenderCommentMarkdown(filteredGrouped, stats.TotalReviewers, aggregated)

	if err := confirmAndSubmitReview(ctx, reviewBody, pr, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitFindings
}

func confirmAndSubmitReview(ctx context.Context, body string, pr prContext, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR review.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, logger)
	if err != nil {
		return err
	}
	if !available {
		return nil
	}

	// Determine review type (self-review can only comment)
	requestChanges := !pr.isSelfReview

	if !autoYes {
		// Check if stdin is a TTY before prompting to avoid hanging in CI
		if !terminal.IsStdinTTY() {
			logger.Log("Non-interactive mode without --yes flag; skipping PR review.", terminal.StyleDim)
			return nil
		}

		fmt.Println()
		prRef := formatPRRef(pr.number)
		if pr.isSelfReview {
			fmt.Print(formatPrompt(
				"You cannot request changes on your own PR. Post review to PR "+prRef+"?",
				"[C]omment / [S]kip:"))
		} else {
			fmt.Print(formatPrompt(
				"Post review to PR "+prRef+"?",
				"[R]equest changes / [C]omment / [S]kip:"))
		}

		response := readUserInput()

		if pr.isSelfReview {
			switch response {
			case "s", "n", "no":
				logger.Log("Skipped posting review.", terminal.StyleDim)
				return nil
			default:
				requestChanges = false
			}
		} else {
			switch response {
			case "c":
				requestChanges = false
			case "s", "n", "no":
				logger.Log("Skipped posting review.", terminal.StyleDim)
				return nil
			default:
				requestChanges = true
			}
		}
	}

	if err := github.SubmitPRReview(ctx, pr.number, body, requestChanges); err != nil {
		logger.Logf(terminal.StyleError, "Failed: %v", err)
		return err
	}

	reviewType := "request changes"
	if !requestChanges {
		reviewType = "comment"
	}
	logger.Logf(terminal.StyleSuccess, "Posted %s review to PR #%s.", reviewType, pr.number)
	return nil
}

// lgtmAction represents the action to take for an LGTM review.
type lgtmAction int

const (
	actionApprove lgtmAction = iota
	actionComment
	actionSkip
)

func confirmAndSubmitLGTM(ctx context.Context, body string, pr prContext, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR approval.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, logger)
	if err != nil {
		return err
	}
	if !available {
		return nil
	}

	action := actionApprove
	if pr.isSelfReview {
		action = actionComment
	}

	if !autoYes {
		if !terminal.IsStdinTTY() {
			logger.Log("Non-interactive mode without --yes flag; skipping LGTM.", terminal.StyleDim)
			return nil
		}

		action = promptLGTMAction(pr)
		if action == actionSkip {
			logger.Log("Skipped posting LGTM.", terminal.StyleDim)
			return nil
		}
	}

	// Check CI status before approving
	if action == actionApprove {
		var err error
		action, err = checkCIAndMaybeDowngrade(ctx, pr.number, action, logger)
		if err != nil {
			return err
		}
		if action == actionSkip {
			return nil
		}
	}

	return executeLGTMAction(ctx, action, pr.number, body, logger)
}

// promptLGTMAction prompts the user for LGTM action choice.
func promptLGTMAction(pr prContext) lgtmAction {
	fmt.Println()
	prRef := formatPRRef(pr.number)

	if pr.isSelfReview {
		fmt.Print(formatPrompt(
			"You cannot approve your own PR. Post LGTM review to PR "+prRef+"?",
			"[C]omment / [S]kip:"))
	} else {
		fmt.Print(formatPrompt(
			"Post LGTM to PR "+prRef+"?",
			"[A]pprove / [C]omment / [S]kip:"))
	}

	response := readUserInput()

	if pr.isSelfReview {
		if response == "s" || response == "n" || response == "no" {
			return actionSkip
		}
		return actionComment
	}

	switch response {
	case "c":
		return actionComment
	case "s", "n", "no":
		return actionSkip
	default:
		return actionApprove
	}
}

// logCIChecks logs a list of CI checks with truncation.
func logCIChecks(logger *terminal.Logger, checks []string) {
	for i, check := range checks {
		if i >= maxDisplayedCIChecks {
			logger.Logf(terminal.StyleDim, "  ... and %d more", len(checks)-maxDisplayedCIChecks)
			break
		}
		logger.Logf(terminal.StyleDim, "  * %s", check)
	}
}

// checkCIAndMaybeDowngrade checks CI status and downgrades to comment if CI is not green.
// Returns error if CI status check fails (network/auth issues).
func checkCIAndMaybeDowngrade(ctx context.Context, prNumber string, action lgtmAction, logger *terminal.Logger) (lgtmAction, error) {
	ciStatus := github.CheckCIStatus(ctx, prNumber)

	if ciStatus.Error != "" {
		logger.Logf(terminal.StyleError, "Failed to check CI status: %s", ciStatus.Error)
		return actionSkip, fmt.Errorf("CI check failed: %s", ciStatus.Error)
	}

	if ciStatus.AllPassed {
		return action, nil
	}

	if len(ciStatus.Failed) > 0 {
		logger.Logf(terminal.StyleError, "Cannot approve PR: %d CI check(s) failed", len(ciStatus.Failed))
		logCIChecks(logger, ciStatus.Failed)
	}
	if len(ciStatus.Pending) > 0 {
		logger.Logf(terminal.StyleWarning, "Cannot approve PR: %d CI check(s) pending", len(ciStatus.Pending))
		logCIChecks(logger, ciStatus.Pending)
	}

	if autoYes {
		logger.Log("CI not green; posting as comment instead of approval.", terminal.StyleDim)
		return actionComment, nil
	}

	if !terminal.IsStdinTTY() {
		logger.Log("CI not green and non-interactive; skipping LGTM.", terminal.StyleDim)
		return actionSkip, nil
	}

	fmt.Print(formatPrompt("Post as comment instead?", "[C]omment / [S]kip:"))
	response := readUserInput()

	switch response {
	case "", "c", "y", "yes":
		return actionComment, nil
	default:
		logger.Log("Skipped posting LGTM.", terminal.StyleDim)
		return actionSkip, nil
	}
}

// executeLGTMAction executes the chosen LGTM action.
func executeLGTMAction(ctx context.Context, action lgtmAction, prNumber, body string, logger *terminal.Logger) error {
	switch action {
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
