package main

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const geminiDeprecationWarning = "Gemini CLI is deprecated for consumer use. ACR still supports gemini for enterprise Gemini CLI users, but use agy for new or non-enterprise Google-agent usage. Google says individual Gemini CLI requests stop serving on June 18, 2026: https://developers.googleblog.com/an-important-update-transitioning-gemini-cli-to-antigravity-cli/"

func executeReview(ctx context.Context, opts ReviewOpts, logger *terminal.Logger) domain.ExitCode {
	if opts.Concurrency < opts.Reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), opts.Reviewers, opts.Concurrency, opts.Base, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), opts.Reviewers, opts.Base, terminal.Color(terminal.Reset))
	}

	if usesGeminiAgent(opts) {
		logger.Logf(terminal.StyleWarning, "%s", geminiDeprecationWarning)
	}

	if err := agent.ValidateAgentNames(opts.ReviewerAgents); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	reviewAgents, err := agent.CreateAgentsWithModel(opts.ReviewerAgents, opts.ReviewerModel)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	summarizerAgent, err := agent.NewAgentWithModel(opts.SummarizerAgent, opts.SummarizerModel)
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return domain.ExitError
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", opts.SummarizerAgent, err)
		return domain.ExitError
	}

	if len(reviewAgents) > 1 {
		distribution := agent.FormatDistribution(reviewAgents, opts.Reviewers)
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distribution, terminal.Color(terminal.Reset))
	} else if opts.Verbose && len(opts.ReviewerAgents) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), opts.ReviewerAgents[0], terminal.Color(terminal.Reset))
	}

	resolvedBaseRef := opts.Base
	if opts.Fetch {

		if !git.IsRelativeRef(opts.Base) {
			branchResult := git.UpdateCurrentBranch(ctx, opts.WorkDir)
			if branchResult.Updated && opts.Verbose {
				logger.Logf(terminal.StyleDim, "Updated branch %s from origin", branchResult.BranchName)
			}
			if branchResult.Error != nil {
				logger.Logf(terminal.StyleWarning, "Could not update %s from origin: %v (reviewing local state)", branchResult.BranchName, branchResult.Error)
			}
		}

		result := git.FetchRemoteRef(ctx, opts.Base, opts.WorkDir)
		resolvedBaseRef = result.ResolvedRef
		if result.FetchAttempted && !result.RefResolved {
			logger.Logf(terminal.StyleWarning, "Failed to fetch %s from origin, comparing against local %s (may be stale)", opts.Base, resolvedBaseRef)
		} else if opts.Verbose && result.FetchAttempted && result.RefResolved {
			logger.Logf(terminal.StyleDim, "Comparing against %s (fetched from origin)", resolvedBaseRef)
		}
	}

	diff, err := git.GetDiff(ctx, resolvedBaseRef, opts.WorkDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to get diff: %v", err)
		return domain.ExitError
	}

	if diff == "" {
		logger.Logf(terminal.StyleSuccess, "No changes detected between HEAD and %s. Nothing to review.", resolvedBaseRef)
		opts.record(OutcomeNoChanges)
		return domain.ExitNoFindings
	}

	if opts.Verbose {
		logger.Logf(terminal.StyleDim, "Diff size: %d bytes", len(diff))
	}

	diffPrecomputed := agent.AgentsNeedDiff(reviewAgents)

	if opts.Verbose && opts.UseRefFile {
		logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
	}

	r, err := runner.New(runner.Config{
		Reviewers:       opts.Reviewers,
		Concurrency:     opts.Concurrency,
		BaseRef:         resolvedBaseRef,
		Timeout:         opts.Timeout,
		Retries:         opts.Retries,
		Verbose:         opts.Verbose,
		WorkDir:         opts.WorkDir,
		Guidance:        opts.Guidance,
		UseRefFile:      opts.UseRefFile,
		Diff:            diff,
		DiffPrecomputed: diffPrecomputed,
	}, reviewAgents, logger)
	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
	}

	var priorFeedback string
	var feedbackWg sync.WaitGroup
	if opts.PRFeedbackEnabled && opts.DetectedPR != "" && opts.FPFilterEnabled {
		logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
			opts.DetectedPR, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		feedbackWg.Add(1)
		go func() {
			defer feedbackWg.Done()

			feedbackAgentName := opts.PRFeedbackAgent
			if feedbackAgentName == "" {
				feedbackAgentName = opts.SummarizerAgent
			}

			summarizer := feedback.NewSummarizer(feedbackAgentName, opts.SummarizerModel, opts.Verbose, logger)
			feedbackCtx, feedbackCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
			defer feedbackCancel()

			summary, err := summarizer.SummarizeFromDir(feedbackCtx, opts.DetectedPR, opts.WorkDir)
			if summary != "" {
				priorFeedback = summary
			}
			if err != nil {

				if ctx.Err() != nil {

					return
				}
				if feedbackCtx.Err() == context.DeadlineExceeded {
					logger.Logf(terminal.StyleWarning, "PR feedback summarizer timed out after %s", opts.SummarizerTimeout)
					return
				}
				logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: %v", err)
				return
			}
			if summary != "" {
				logger.Log("PR feedback summarized", terminal.StyleSuccess)
			} else {
				logger.Log("No relevant PR feedback found", terminal.StyleDim)
			}
		}()
	}

	results, wallClock, err := r.Run(ctx)
	if !opts.Verbose {
		for _, result := range results {
			for _, warning := range result.Warnings {
				logger.Logf(terminal.StyleWarning, "Reviewer #%d: %s", result.ReviewerID, warning.Message)
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	stats := runner.BuildStats(results, opts.Reviewers, wallClock)

	if stats.AllFailed() {
		logger.Log("All reviewers failed", terminal.StyleError)
		return domain.ExitError
	}

	allFindings := runner.CollectFindings(results)
	aggregated := domain.AggregateFindings(allFindings)

	phaseSpinner := terminal.NewPhaseSpinner("Summarizing")
	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		phaseSpinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	summarizerCtx, summarizerCancel := context.WithTimeout(ctx, opts.SummarizerTimeout)
	defer summarizerCancel()
	summaryResult, err := summarizer.Summarize(summarizerCtx, opts.SummarizerAgent, opts.SummarizerModel, aggregated, opts.WorkDir, opts.Verbose, logger)
	spinnerCancel()
	<-spinnerDone
	if summaryResult != nil && !opts.Verbose {
		for _, warning := range summaryResult.Warnings {
			logger.Log(warning, terminal.StyleWarning)
		}
	}

	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		if summarizerCtx.Err() == context.DeadlineExceeded {
			logger.Logf(terminal.StyleError, "Summarizer timed out after %s", opts.SummarizerTimeout)
		} else {
			logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		}
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	feedbackWg.Wait()

	var fpFilteredCount int
	var fpRemoved []domain.FPRemovedInfo
	if opts.FPFilterEnabled && summaryResult.ExitCode == 0 && len(summaryResult.Grouped.Findings) > 0 && ctx.Err() == nil {
		fpSpinner := terminal.NewPhaseSpinner("Filtering false positives")
		fpSpinnerCtx, fpSpinnerCancel := context.WithCancel(ctx)
		fpSpinnerDone := make(chan struct{})
		go func() {
			fpSpinner.Run(fpSpinnerCtx)
			close(fpSpinnerDone)
		}()

		fpCtx, fpCancel := context.WithTimeout(ctx, opts.FPFilterTimeout)
		defer fpCancel()
		fpFilter := fpfilter.New(opts.SummarizerAgent, opts.SummarizerModel, opts.FPThreshold, opts.WorkDir, opts.Verbose, logger)
		fpResult := fpFilter.Apply(fpCtx, summaryResult.Grouped, priorFeedback, stats.SuccessfulReviewers)
		fpSpinnerCancel()
		<-fpSpinnerDone

		if fpResult != nil && fpResult.Skipped && ctx.Err() == nil {
			logger.Logf(terminal.StyleWarning, "FP filter skipped (%s): showing all findings", fpResult.SkipReason)
		}
		if fpResult != nil {
			if !opts.Verbose {
				for _, warning := range fpResult.Warnings {
					logger.Log(warning, terminal.StyleWarning)
				}
			}
			summaryResult.Grouped = fpResult.Grouped
			fpFilteredCount = fpResult.RemovedCount
			stats.FPFilterDuration = fpResult.Duration

			for _, r := range fpResult.Removed {
				fpRemoved = append(fpRemoved, domain.FPRemovedInfo{
					Sources:   r.Finding.Sources,
					FPScore:   r.FPScore,
					Reasoning: r.Reasoning,
					Title:     r.Finding.Title,
				})
			}
		}
	}
	stats.FPFilteredCount = fpFilteredCount

	if ctx.Err() != nil {
		return domain.ExitInterrupted
	}

	var excludeFiltered []domain.FindingGroup
	if len(opts.ExcludePatterns) > 0 {
		f, err := filter.New(opts.ExcludePatterns)
		if err != nil {
			logger.Logf(terminal.StyleError, "Invalid exclude pattern: %v", err)
			return domain.ExitError
		}
		preExclude := summaryResult.Grouped.Findings
		summaryResult.Grouped = f.Apply(summaryResult.Grouped)
		excludeFiltered = diffFindingGroups(preExclude, summaryResult.Grouped.Findings)
	}

	dispositions := domain.BuildDispositions(
		len(aggregated),
		summaryResult.Grouped.Info,
		fpRemoved,
		excludeFiltered,
		summaryResult.Grouped.Findings,
	)

	report := runner.RenderReport(summaryResult.Grouped, summaryResult, stats)
	fmt.Println(report)

	if summaryResult.ExitCode != 0 {
		return domain.ExitError
	}

	if !summaryResult.Grouped.HasFindings() {
		return handleLGTM(ctx, opts, allFindings, aggregated, dispositions, stats, logger)
	}

	return handleFindings(ctx, opts, summaryResult.Grouped, aggregated, stats, logger)
}

func usesGeminiAgent(opts ReviewOpts) bool {
	for _, reviewerAgent := range opts.ReviewerAgents {
		if reviewerAgent == "gemini" {
			return true
		}
	}
	if opts.SummarizerAgent == "gemini" {
		return true
	}
	if opts.PRFeedbackEnabled && opts.DetectedPR != "" && opts.FPFilterEnabled {
		feedbackAgent := opts.PRFeedbackAgent
		if feedbackAgent == "" {
			feedbackAgent = opts.SummarizerAgent
		}
		return feedbackAgent == "gemini"
	}
	return false
}

func diffFindingGroups(before, after []domain.FindingGroup) []domain.FindingGroup {
	j := 0
	var removed []domain.FindingGroup
	for i := range before {
		if j < len(after) && slices.Equal(before[i].Sources, after[j].Sources) {
			j++
		} else {
			removed = append(removed, before[i])
		}
	}
	return removed
}
