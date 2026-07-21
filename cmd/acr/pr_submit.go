package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxDisplayedCIChecks = 5

type prContext struct {
	number       string
	isSelfReview bool
	err          error
}

func getPRContext(ctx context.Context, opts ReviewOpts) prContext {
	if opts.Local || !github.IsGHAvailable() {
		return prContext{}
	}

	if opts.PRNumber != "" {
		return prContext{
			number:       opts.PRNumber,
			isSelfReview: github.IsSelfReview(ctx, opts.PRNumber),
		}
	}

	foundPR, err := github.GetCurrentPRNumber(ctx, opts.WorktreeBranch)
	if err != nil {
		return prContext{err: err}
	}
	return prContext{
		number:       foundPR,
		isSelfReview: github.IsSelfReview(ctx, foundPR),
	}
}

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

var stdinReader = bufio.NewReader(os.Stdin)

func readUserInput() string {
	response, err := stdinReader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(response)) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(response))
}

func promptOptionalMessage() string {
	fmt.Print(formatPrompt("Add a note to the review?", "(press Enter to skip):"))
	msg, err := stdinReader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(msg)) == 0 {
		return ""
	}
	return strings.TrimSpace(msg)
}

func prependUserNote(body, note string) string {
	return fmt.Sprintf("**Reviewer's note:** %s\n\n---\n\n%s", note, body)
}

func formatPrompt(question, options string) string {
	return fmt.Sprintf("%s?%s %s %s%s%s ",
		terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
		question,
		terminal.Color(terminal.Dim), options, terminal.Color(terminal.Reset))
}

func formatPRRef(prNumber string) string {
	return fmt.Sprintf("%s#%s%s", terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))
}

func handleLGTM(ctx context.Context, opts ReviewOpts, allFindings []domain.Finding, aggregated []domain.AggregatedFinding, dispositions map[int]domain.Disposition, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {

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
	opts.record(OutcomeFindings)

	selectedFindings := grouped.Findings

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

	requestChanges := !pr.isSelfReview && !opts.ForcePostComment

	if !opts.AutoYes {

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

	if !opts.AutoYes && terminal.IsStdinTTY() {
		if note := promptOptionalMessage(); note != "" {
			body = prependUserNote(body, note)
		}
	}

	if headMovedSinceReview(ctx, opts, pr.number, logger) {
		opts.record(OutcomeStaleHead)
		return nil
	}

	if err := retrySubmission(ctx, func() error {
		return github.SubmitPRReview(ctx, pr.number, body, requestChanges)
	}, opts.Outcome != nil, logger); err != nil {
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

	if opts.Outcome != nil {
		opts.Outcome.LGTMBody = body
	}

	available, err := checkPRAvailable(pr, opts, logger)
	if err != nil {
		return err
	}
	if !available {
		opts.record(OutcomeLGTMSkipped)
		return nil
	}

	action := actionApprove
	if pr.isSelfReview || opts.ForcePostComment {
		action = actionComment
	}

	if !opts.AutoYes {
		if !terminal.IsStdinTTY() {
			logger.Log("Non-interactive mode without --yes flag; skipping LGTM.", terminal.StyleDim)
			opts.record(OutcomeLGTMSkipped)
			return nil
		}

		action = promptLGTMAction(pr)
		if action == actionSkip {
			logger.Log("Skipped posting LGTM.", terminal.StyleDim)
			opts.record(OutcomeLGTMDeclined)
			return nil
		}
	}

	if action == actionApprove {
		var err error
		action, err = checkCIAndMaybeDowngrade(ctx, pr.number, action, opts, logger)
		if err != nil {
			return err
		}
		if action == actionSkip {
			opts.record(OutcomeLGTMDeclined)
			return nil
		}
	}

	if !opts.AutoYes && terminal.IsStdinTTY() {
		if note := promptOptionalMessage(); note != "" {
			body = prependUserNote(body, note)
		}
	}

	if headMovedSinceReview(ctx, opts, pr.number, logger) {
		opts.record(OutcomeStaleHead)
		return nil
	}

	if err := retrySubmission(ctx, func() error {
		return executeLGTMAction(ctx, action, pr.number, body, logger)
	}, opts.Outcome != nil, logger); err != nil {
		return err
	}
	if action == actionApprove {
		opts.record(OutcomeLGTMApproved)
	} else {
		opts.record(OutcomeLGTMComment)
	}
	return nil
}

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

var submissionRetryDelay = 5 * time.Second

const submissionAttempts = 3

func retrySubmission(ctx context.Context, submit func() error, watchMode bool, logger *terminal.Logger) error {
	err := submit()
	if err == nil || !watchMode {
		return err
	}
	for attempt := 2; attempt <= submissionAttempts; attempt++ {
		if ctx.Err() != nil {
			return err
		}
		logger.Logf(terminal.StyleWarning, "Submission failed (%v); retrying (%d/%d)", err, attempt, submissionAttempts)
		timer := time.NewTimer(submissionRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		if err = submit(); err == nil {
			return nil
		}
	}
	return err
}

func headMovedSinceReview(ctx context.Context, opts ReviewOpts, prNumber string, logger *terminal.Logger) bool {
	if opts.ExpectedHeadSHA == "" {
		return false
	}
	st, err := github.GetPRWatchState(ctx, prNumber)
	if err != nil || st.HeadSHA == "" {
		return false
	}
	if strings.EqualFold(st.HeadSHA, opts.ExpectedHeadSHA) {
		return false
	}
	logger.Logf(terminal.StyleWarning, "PR head moved since the review; skipping post (the new head will be re-reviewed).")
	return true
}

func logCIChecks(logger *terminal.Logger, checks []string) {
	for i, check := range checks {
		if i >= maxDisplayedCIChecks {
			logger.Logf(terminal.StyleDim, "  ... and %d more", len(checks)-maxDisplayedCIChecks)
			break
		}
		logger.Logf(terminal.StyleDim, "  * %s", check)
	}
}

func checkCIAndMaybeDowngrade(ctx context.Context, prNum string, action lgtmAction, opts ReviewOpts, logger *terminal.Logger) (lgtmAction, error) {
	ciStatus := github.CheckCIStatus(ctx, prNum)

	if ciStatus.Error != "" {
		if opts.Outcome == nil {
			logger.Logf(terminal.StyleError, "Failed to check CI status: %s", ciStatus.Error)
			return actionSkip, fmt.Errorf("CI check failed: %s", ciStatus.Error)
		}
		if opts.AutoYes {
			logger.Logf(terminal.StyleWarning, "Failed to check CI status (%s); posting as comment and deferring approval.", ciStatus.Error)
			opts.Outcome.CIDowngraded = true
			return actionComment, nil
		}
		logger.Logf(terminal.StyleWarning, "Failed to check CI status (%s).", ciStatus.Error)
		if !terminal.IsStdinTTY() {
			return actionSkip, nil
		}
		fmt.Print(formatPrompt("Post as comment instead?", "[C]omment (default) / [S]kip:"))
		switch readUserInput() {
		case "", "c", "y", "yes":
			return actionComment, nil
		default:
			logger.Log("Skipped posting LGTM.", terminal.StyleDim)
			return actionSkip, nil
		}
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
		if opts.Outcome != nil {
			opts.Outcome.CIDowngraded = true
		}
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
