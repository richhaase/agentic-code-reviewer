package main

import (
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func executeReview(ctx context.Context, workDir string, excludePatterns []string, customPrompt string, reviewerAgentNames []string, logger *terminal.Logger) domain.ExitCode {
	if concurrency < reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, concurrency, baseRef, terminal.Color(terminal.Reset))
	} else {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
			terminal.Color(terminal.Dim), reviewers, baseRef, terminal.Color(terminal.Reset))
	}

	// Validate agent names
	if err := agent.ValidateAgentNames(reviewerAgentNames); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return domain.ExitError
	}

	// Create agent instances (validates all CLIs upfront - fail fast)
	reviewAgents, err := agent.CreateAgents(reviewerAgentNames)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return domain.ExitError
	}

	// Validate summarizer agent up front to fail fast if CLI is missing
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

	// Run reviewers
	r, err := runner.New(runner.Config{
		Reviewers:    reviewers,
		Concurrency:  concurrency,
		BaseRef:      baseRef,
		Timeout:      timeout,
		Retries:      retries,
		Verbose:      verbose,
		WorkDir:      workDir,
		CustomPrompt: customPrompt,
	}, reviewAgents, logger)
	if err != nil {
		logger.Logf(terminal.StyleError, "Runner initialization failed: %v", err)
		return domain.ExitError
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

	summaryResult, err := summarizer.Summarize(ctx, summarizerAgentName, aggregated)
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
