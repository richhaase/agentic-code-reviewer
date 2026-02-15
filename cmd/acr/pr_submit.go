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
// If --pr flag was used, uses that PR number directly instead of looking it up.
func getPRContext(ctx context.Context, opts ReviewOpts) prContext {
	if opts.Local || !github.IsGHAvailable() {
		return prContext{}
	}

	// If --pr flag was used, we already have the PR number
	// This is important for detached worktrees where branch lookup would fail
	if opts.PRNumber != "" {
		return prContext{
			number:       opts.PRNumber,
			isSelfReview: github.IsSelfReview(ctx, opts.PRNumber),
		}
	}

	// Otherwise, look up PR from branch
	foundPR, err := github.GetCurrentPRNumber(ctx, opts.WorktreeBranch)
	if err != nil {
		return prContext{err: err}
	}
	return prContext{
		number:       foundPR,
		isSelfReview: github.IsSelfReview(ctx, foundPR),
	}
}

// checkPRAvailable verifies gh CLI is available and PR exists.
// Returns error if gh CLI unavailable or auth failed, true if PR exists, false if no PR found.
func checkPRAvailable(pr prContext, opts ReviewOpts, logger *terminal.Logger) (bool, error) {
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
			if opts.WorktreeBranch != "" {
				branchDesc = fmt.Sprintf("branch '%s'", opts.WorktreeBranch)
			}
			logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
			return false, nil
		}
		logger.Logf(terminal.StyleError, "Failed to check PR: %v", pr.err)
		return false, pr.err
	}

	if pr.number == "" {
		branchDesc := "current branch"
		if opts.WorktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", opts.WorktreeBranch)
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

func handleLGTM(ctx context.Context, opts ReviewOpts, allFindings []domain.Finding, aggregated []domain.AggregatedFinding, dispositions map[int]domain.Disposition, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	// Build a text→aggregated index lookup for mapping raw findings to dispositions
	textToIndex := make(map[string]int, len(aggregated))
	for i, af := range aggregated {
		textToIndex[af.Text] = i
	}

	annotatedComments := make(map[int][]runner.AnnotatedComment)
	for _, f := range allFindings {
		if f.Text == "" {
			continue
		}
		ac := runner.AnnotatedComment{Text: f.Text}
		if idx, ok := textToIndex[f.Text]; ok {
			if d, ok := dispositions[idx]; ok {
				ac.Disposition = d
			}
		}
		annotatedComments[f.ReviewerID] = append(annotatedComments[f.ReviewerID], ac)
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, annotatedComments, version)
	pr := getPRContext(ctx, opts)

	if err := confirmAndSubmitLGTM(ctx, lgtmBody, pr, opts, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

func handleFindings(ctx context.Context, opts ReviewOpts, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting (skip in local mode)
	if !opts.Local && !opts.AutoYes && terminal.IsStdoutTTY() {
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

			lgtmBody := runner.RenderDismissedLGTMMarkdown(grouped.Findings, stats, version)
			pr := getPRContext(ctx, opts)
			// Best-effort: LGTM posting is optional when dismissing findings.
			// Auth/network errors should not fail the run.
			_ = confirmAndSubmitLGTM(ctx, lgtmBody, pr, opts, logger)
			return domain.ExitNoFindings
		}
	}

	pr := getPRContext(ctx, opts)

	filteredGrouped := domain.GroupedFindings{
		Findings: selectedFindings,
		Info:     grouped.Info,
	}
	reviewBody := runner.RenderCommentMarkdown(filteredGrouped, stats.TotalReviewers, aggregated, version)

	if err := confirmAndSubmitReview(ctx, reviewBody, pr, opts, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitFindings
}

func confirmAndSubmitReview(ctx context.Context, body string, pr prContext, opts ReviewOpts, logger *terminal.Logger) error {
	if opts.Local {
		logger.Log("Local mode enabled; skipping PR review.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, opts, logger)
	if err != nil {
		return err
	}
	if !available {
		return nil
	}

	// Determine review type (self-review can only comment)
	requestChanges := !pr.isSelfReview

	if !opts.AutoYes {
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
				"[C]omment (default) / [S]kip:"))
		} else {
			fmt.Print(formatPrompt(
				"Post review to PR "+prRef+"?",
				"[R]equest changes (default) / [C]omment / [S]kip:"))
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

func confirmAndSubmitLGTM(ctx context.Context, body string, pr prContext, opts ReviewOpts, logger *terminal.Logger) error {
	if opts.Local {
		logger.Log("Local mode enabled; skipping PR approval.", terminal.StyleDim)
		return nil
	}

	available, err := checkPRAvailable(pr, opts, logger)
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

	if !opts.AutoYes {
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
		action, err = checkCIAndMaybeDowngrade(ctx, pr.number, action, opts, logger)
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
			"[C]omment (default) / [S]kip:"))
	} else {
		fmt.Print(formatPrompt(
			"Post LGTM to PR "+prRef+"?",
			"[A]pprove (default) / [C]omment / [S]kip:"))
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
func checkCIAndMaybeDowngrade(ctx context.Context, prNum string, action lgtmAction, opts ReviewOpts, logger *terminal.Logger) (lgtmAction, error) {
	ciStatus := github.CheckCIStatus(ctx, prNum)

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

	if opts.AutoYes {
		logger.Log("CI not green; posting as comment instead of approval.", terminal.StyleDim)
		return actionComment, nil
	}

	if !terminal.IsStdinTTY() {
		logger.Log("CI not green and non-interactive; skipping LGTM.", terminal.StyleDim)
		return actionSkip, nil
	}

	fmt.Print(formatPrompt("Post as comment instead?", "[C]omment (default) / [S]kip:"))
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
