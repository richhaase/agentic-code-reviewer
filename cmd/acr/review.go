package main

import (
	"context"
	"fmt"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
	"github.com/anthropics/agentic-code-reviewer/internal/filter"
	"github.com/anthropics/agentic-code-reviewer/internal/runner"
	"github.com/anthropics/agentic-code-reviewer/internal/summarizer"
	"github.com/anthropics/agentic-code-reviewer/internal/terminal"
)

func executeReview(ctx context.Context, workDir string, excludePatterns []string, logger *terminal.Logger) domain.ExitCode {
	if concurrency < reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, concurrency, baseRef, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, baseRef, terminal.Color(terminal.Reset))
	}

	if verbose {
		logger.Logf(terminal.StyleDim, "%sCommand: codex exec --json --color never review --base %s%s",
			terminal.Color(terminal.Dim), baseRef, terminal.Color(terminal.Reset))
	}

	// Run reviewers
	r := runner.New(runner.Config{
		Reviewers:   reviewers,
		Concurrency: concurrency,
		BaseRef:     baseRef,
		Timeout:     timeout,
		Retries:     retries,
		Verbose:     verbose,
		WorkDir:     workDir,
	}, logger)

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

	summaryResult, err := summarizer.Summarize(ctx, aggregated)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Apply exclude patterns if configured
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
