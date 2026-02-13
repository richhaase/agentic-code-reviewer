package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func executeReview(ctx context.Context, resolved config.ResolvedConfig, workDir string, excludePatterns []string, useRefFile bool, summarizerTimeout time.Duration, fpFilterTimeout time.Duration, prNumber string, logger *terminal.Logger) domain.ExitCode {
	if resolved.Concurrency < resolved.Reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), resolved.Reviewers, resolved.Concurrency, resolved.Base, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), resolved.Reviewers, resolved.Base, terminal.Color(terminal.Reset))
	}

	if err := agent.ValidateAgentNames(resolved.ReviewerAgents); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	reviewAgents, err := agent.CreateAgents(resolved.ReviewerAgents)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	summarizerAgent, err := agent.NewAgent(resolved.SummarizerAgent)
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return domain.ExitError
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", resolved.SummarizerAgent, err)
		return domain.ExitError
	}

	// Show agent distribution if multiple agents
	if len(reviewAgents) > 1 {
		distribution := agent.FormatDistribution(reviewAgents, resolved.Reviewers)
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distribution, terminal.Color(terminal.Reset))
	} else if verbose && len(resolved.ReviewerAgents) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), resolved.ReviewerAgents[0], terminal.Color(terminal.Reset))
	}

	// Resolve the base ref once before launching parallel reviewers.
	// This ensures all reviewers compare against the same ref, avoiding
	// inconsistent results if network conditions vary during parallel execution.
	resolvedBaseRef := resolved.Base
	if resolved.Fetch {
		// Update current branch from remote (fast-forward only).
		// Skip when the base ref is relative to HEAD (e.g., HEAD~3) since
		// fast-forwarding would change what those refs resolve to.
		if !git.IsRelativeRef(resolved.Base) {
			branchResult := git.UpdateCurrentBranch(ctx, workDir)
			if branchResult.Updated && verbose {
				logger.Logf(terminal.StyleDim, "Updated branch %s from origin", branchResult.BranchName)
			}
			if branchResult.Error != nil {
				logger.Logf(terminal.StyleWarning, "Could not update %s from origin: %v (reviewing local state)", branchResult.BranchName, branchResult.Error)
			}
		}

		// Fetch base ref
		result := git.FetchRemoteRef(ctx, resolved.Base, workDir)
		resolvedBaseRef = result.ResolvedRef
		if result.FetchAttempted && !result.RefResolved {
			logger.Logf(terminal.StyleWarning, "Failed to fetch %s from origin, comparing against local %s (may be stale)", resolved.Base, resolvedBaseRef)
		} else if verbose && result.FetchAttempted && result.RefResolved {
			logger.Logf(terminal.StyleDim, "Comparing against %s (fetched from origin)", resolvedBaseRef)
		}
	}

	// Pre-compute the git diff once and share it across all reviewers.
	// Always compute (even for codex-only) so we can short-circuit empty diffs.
	diff, err := git.GetDiff(ctx, resolvedBaseRef, workDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to get diff: %v", err)
		return domain.ExitError
	}

	// Short-circuit: no changes means nothing to review
	if diff == "" {
		logger.Logf(terminal.StyleSuccess, "No changes detected between HEAD and %s. Nothing to review.", resolvedBaseRef)
		return domain.ExitNoFindings
	}

	if verbose {
		logger.Logf(terminal.StyleDim, "Diff size: %d bytes", len(diff))
	}

	// Pass precomputed diff to agents that need it (Claude, Gemini).
	// Codex ignores it (built-in diff via --base).
	diffPrecomputed := agent.AgentsNeedDiff(reviewAgents)

	if verbose && useRefFile {
		logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
	}

	// Run reviewers
	r, err := runner.New(runner.Config{
		Reviewers:       resolved.Reviewers,
		Concurrency:     resolved.Concurrency,
		BaseRef:         resolvedBaseRef,
		Timeout:         resolved.Timeout,
		Retries:         resolved.Retries,
		Verbose:         verbose,
		WorkDir:         workDir,
		Guidance:        resolved.Guidance,
		UseRefFile:      useRefFile,
		Diff:            diff,
		DiffPrecomputed: diffPrecomputed,
	}, reviewAgents, logger)
	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
	}

	// Start PR feedback summarizer in parallel with reviewers (if enabled, reviewing a PR, and FP filter is on)
	// Skip if FP filter is disabled since the feedback summary is only consumed by the FP filter
	var priorFeedback string
	var feedbackWg sync.WaitGroup
	if resolved.PRFeedbackEnabled && prNumber != "" && resolved.FPFilterEnabled {
		logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
			prNumber, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		feedbackWg.Add(1)
		go func() {
			defer feedbackWg.Done()

			// Determine which agent to use for feedback summarization
			feedbackAgentName := resolved.PRFeedbackAgent
			if feedbackAgentName == "" {
				feedbackAgentName = resolved.SummarizerAgent
			}

			summarizer := feedback.NewSummarizer(feedbackAgentName, verbose, logger)
			summary, err := summarizer.Summarize(ctx, prNumber)
			if err != nil {
				logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: %v", err)
				return
			}
			if summary != "" {
				logger.Log("PR feedback summarized", terminal.StyleSuccess)
			} else {
				logger.Log("No relevant PR feedback found", terminal.StyleDim)
			}
			priorFeedback = summary
		}()
	}

	results, wallClock, err := r.Run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	// Build statistics
	stats := runner.BuildStats(results, resolved.Reviewers, wallClock)

	// Check if all reviewers failed
	if stats.AllFailed() {
		logger.Log("All reviewers failed", terminal.StyleError)
		return domain.ExitError
	}

	// Aggregate and summarize findings
	allFindings := runner.CollectFindings(results)
	aggregated := domain.AggregateFindings(allFindings)

	// Run summarizer with spinner
	phaseSpinner := terminal.NewPhaseSpinner("Summarizing")
	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		phaseSpinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	summarizerCtx, summarizerCancel := context.WithTimeout(ctx, summarizerTimeout)
	defer summarizerCancel()
	summaryResult, err := summarizer.Summarize(summarizerCtx, resolved.SummarizerAgent, aggregated, verbose, logger)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		if summarizerCtx.Err() == context.DeadlineExceeded {
			logger.Logf(terminal.StyleError, "Summarizer timed out after %s", summarizerTimeout)
		} else {
			logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		}
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Wait for PR feedback summarizer to complete
	feedbackWg.Wait()

	var fpFilteredCount int
	if resolved.FPFilterEnabled && summaryResult.ExitCode == 0 && len(summaryResult.Grouped.Findings) > 0 && ctx.Err() == nil {
		fpSpinner := terminal.NewPhaseSpinner("Filtering false positives")
		fpSpinnerCtx, fpSpinnerCancel := context.WithCancel(ctx)
		fpSpinnerDone := make(chan struct{})
		go func() {
			fpSpinner.Run(fpSpinnerCtx)
			close(fpSpinnerDone)
		}()

		fpCtx, fpCancel := context.WithTimeout(ctx, fpFilterTimeout)
		defer fpCancel()
		fpFilter := fpfilter.New(resolved.SummarizerAgent, resolved.FPThreshold, verbose, logger)
		fpResult := fpFilter.Apply(fpCtx, summaryResult.Grouped, priorFeedback)
		fpSpinnerCancel()
		<-fpSpinnerDone

		if fpResult != nil && fpResult.Skipped && ctx.Err() == nil {
			logger.Logf(terminal.StyleWarning, "FP filter skipped (%s): showing all findings", fpResult.SkipReason)
		}
		if fpResult != nil {
			summaryResult.Grouped = fpResult.Grouped
			fpFilteredCount = fpResult.RemovedCount
			stats.FPFilterDuration = fpResult.Duration
		}
	}
	stats.FPFilteredCount = fpFilteredCount

	if len(excludePatterns) > 0 {
		f, err := filter.New(excludePatterns)
		if err != nil {
			logger.Logf(terminal.StyleError, "Invalid exclude pattern: %v", err)
			return domain.ExitError
		}
		summaryResult.Grouped = f.Apply(summaryResult.Grouped)
	}

	// Render and print report
	report := runner.RenderReport(summaryResult.Grouped, summaryResult, stats)
	fmt.Println(report)

	if summaryResult.ExitCode != 0 {
		return domain.ExitError
	}

	// Handle PR actions
	if !summaryResult.Grouped.HasFindings() {
		return handleLGTM(ctx, allFindings, stats, logger)
	}

	return handleFindings(ctx, summaryResult.Grouped, aggregated, stats, logger)
}
