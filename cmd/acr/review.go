package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func executeReview(ctx context.Context, workDir string, excludePatterns []string, guidance string, reviewerAgentNames []string, summarizerAgentName string, fetchRemote bool, useRefFile bool, fpFilterEnabled bool, fpThreshold int, prFeedbackEnabled bool, prFeedbackAgent string, prNumber string, logger *terminal.Logger) domain.ExitCode {
	if concurrency < reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, concurrency, baseRef, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, baseRef, terminal.Color(terminal.Reset))
	}

	if err := agent.ValidateAgentNames(reviewerAgentNames); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	reviewAgents, err := agent.CreateAgents(reviewerAgentNames)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	summarizerAgent, err := agent.NewAgent(summarizerAgentName)
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return domain.ExitError
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", summarizerAgentName, err)
		return domain.ExitError
	}

	// Show agent distribution if multiple agents
	if len(reviewAgents) > 1 {
		distribution := agent.FormatDistribution(reviewAgents, reviewers)
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distribution, terminal.Color(terminal.Reset))
	} else if verbose && len(reviewerAgentNames) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), reviewerAgentNames[0], terminal.Color(terminal.Reset))
	}

	// Resolve the base ref once before launching parallel reviewers.
	// This ensures all reviewers compare against the same ref, avoiding
	// inconsistent results if network conditions vary during parallel execution.
	resolvedBaseRef := baseRef
	if fetchRemote {
		result := agent.FetchRemoteRef(ctx, baseRef, workDir)
		resolvedBaseRef = result.ResolvedRef
		if result.FetchAttempted && !result.RefResolved {
			logger.Logf(terminal.StyleWarning, "Failed to fetch %s from origin, comparing against local %s (may be stale)", baseRef, resolvedBaseRef)
		} else if verbose && result.FetchAttempted && result.RefResolved {
			logger.Logf(terminal.StyleDim, "Comparing against %s (fetched from origin)", resolvedBaseRef)
		}
	}

	// Run reviewers
	r, err := runner.New(runner.Config{
		Reviewers:    reviewers,
		Concurrency:  concurrency,
		BaseRef:      resolvedBaseRef,
		Timeout:      timeout,
		Retries:      retries,
		Verbose:      verbose,
		WorkDir:      workDir,
		Guidance: guidance,
		UseRefFile:   useRefFile,
	}, reviewAgents, logger)
	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
	}

	// Log ref-file mode if explicitly requested (verbose)
	// Note: We don't pre-fetch the diff here to avoid duplicate GetGitDiff calls.
	// Each agent will fetch the diff and decide on ref-file mode based on size.
	if verbose && useRefFile {
		logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
	}

	// Start PR feedback summarizer in parallel with reviewers (if enabled, reviewing a PR, and FP filter is on)
	// Skip if FP filter is disabled since the feedback summary is only consumed by the FP filter
	var priorFeedback string
	var feedbackWg sync.WaitGroup
	if prFeedbackEnabled && prNumber != "" && fpFilterEnabled {
		logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
			prNumber, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		feedbackWg.Add(1)
		go func() {
			defer feedbackWg.Done()

			// Determine which agent to use for feedback summarization
			feedbackAgentName := prFeedbackAgent
			if feedbackAgentName == "" {
				feedbackAgentName = summarizerAgentName
			}

			summarizer := feedback.NewSummarizer(feedbackAgentName, verbose)
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
	stats := runner.BuildStats(results, reviewers, wallClock)

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

	summaryResult, err := summarizer.Summarize(ctx, summarizerAgentName, aggregated, verbose)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Wait for PR feedback summarizer to complete
	feedbackWg.Wait()

	var fpFilteredCount int
	if fpFilterEnabled && summaryResult.ExitCode == 0 && len(summaryResult.Grouped.Findings) > 0 && ctx.Err() == nil {
		fpSpinner := terminal.NewPhaseSpinner("Filtering false positives")
		fpSpinnerCtx, fpSpinnerCancel := context.WithCancel(ctx)
		fpSpinnerDone := make(chan struct{})
		go func() {
			fpSpinner.Run(fpSpinnerCtx)
			close(fpSpinnerDone)
		}()

		fpFilter := fpfilter.New(summarizerAgentName, fpThreshold, verbose)
		fpResult := fpFilter.Apply(ctx, summaryResult.Grouped, priorFeedback)
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
