package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

type semanticReviewService interface {
	Run(context.Context, reviewpkg.Request) (*domain.ReviewRun, error)
}

func executeReview(ctx context.Context, opts ReviewOpts, logger *terminal.Logger) domain.ExitCode {
	logReviewStart(opts, logger)

	if usesGeminiAgent(opts) {
		logger.Logf(terminal.StyleWarning, "%s", geminiDeprecationWarning)
	}

	reviewAgents, ready := prepareReviewAgents(opts, logger)
	if !ready {
		return domain.ExitError
	}
	logAgentDistribution(opts, reviewAgents, logger)

	resolvedBaseRef := resolveReviewBase(ctx, opts, logger)
	if opts.PullRequest == nil && opts.DetectedPR != "" {
		pullRequest, err := github.GetPullRequestKey(ctx, opts.RepositoryRoot, opts.DetectedPR)
		if err != nil {
			logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: could not resolve PR identity: %v", err)
		} else {
			opts.PullRequest = &pullRequest
		}
	}
	events := newCLIReviewEvents(opts, logger)
	defer events.Close()

	service, err := reviewpkg.NewService(reviewpkg.WithDiffProvider(func(ctx context.Context, target domain.ReviewTarget) (string, error) {
		diff, err := git.GetDiff(ctx, target.Revision.BaseObjectID, target.WorktreeRoot)
		if err != nil {
			return "", err
		}
		if diff != "" && opts.Verbose {
			logger.Logf(terminal.StyleDim, "Diff size: %d bytes", len(diff))
			if opts.UseRefFile {
				logger.Logf(terminal.StyleDim, "Ref-file mode enabled")
			}
		}
		return diff, nil
	}))
	if err != nil {
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	return executeTypedReview(ctx, opts, resolvedBaseRef, service, events, logger)
}

func logReviewStart(opts ReviewOpts, logger *terminal.Logger) {
	if opts.Concurrency < opts.Reviewers {
		logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, %d concurrent, base=%s)%s",
			terminal.Color(terminal.Dim), opts.Reviewers, opts.Concurrency, opts.Base, terminal.Color(terminal.Reset))
		return
	}
	logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
		terminal.Color(terminal.Dim), opts.Reviewers, opts.Base, terminal.Color(terminal.Reset))
}

func prepareReviewAgents(opts ReviewOpts, logger *terminal.Logger) ([]agent.Agent, bool) {
	if err := agent.ValidateAgentNames(opts.ReviewerAgents); err != nil {
		logger.Logf(terminal.StyleError, "Invalid agent: %v", err)
		return nil, false
	}
	reviewAgents, err := agent.CreateAgentsWithModel(opts.ReviewerAgents, opts.ReviewerModel)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return nil, false
	}
	summarizerAgent, err := agent.NewAgentWithModel(opts.SummarizerAgent, opts.SummarizerModel)
	if err != nil {
		logger.Logf(terminal.StyleError, "Invalid summarizer agent: %v", err)
		return nil, false
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "%s CLI not found (summarizer): %v", opts.SummarizerAgent, err)
		return nil, false
	}
	return reviewAgents, true
}

func logAgentDistribution(opts ReviewOpts, reviewAgents []agent.Agent, logger *terminal.Logger) {
	if len(reviewAgents) > 1 {
		distribution := agent.FormatDistribution(reviewAgents, opts.Reviewers)
		logger.Logf(terminal.StyleInfo, "Agent distribution: %s%s%s",
			terminal.Color(terminal.Dim), distribution, terminal.Color(terminal.Reset))
	} else if opts.Verbose && len(opts.ReviewerAgents) > 0 {
		logger.Logf(terminal.StyleDim, "%sUsing agent: %s%s",
			terminal.Color(terminal.Dim), opts.ReviewerAgents[0], terminal.Color(terminal.Reset))
	}
}

func resolveReviewBase(ctx context.Context, opts ReviewOpts, logger *terminal.Logger) string {
	resolvedBaseRef := opts.Base
	if !opts.Fetch {
		return resolvedBaseRef
	}
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
	return resolvedBaseRef
}

func executeTypedReview(ctx context.Context, opts ReviewOpts, resolvedBaseRef string, service semanticReviewService, events reviewpkg.EventSink, logger *terminal.Logger) domain.ExitCode {
	request, err := newReviewRequest(opts, resolvedBaseRef, events)
	if err != nil {
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}
	run, err := service.Run(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}
	return handleTypedReviewRun(ctx, opts, run, logger)
}

func newReviewRequest(opts ReviewOpts, resolvedBaseRef string, events reviewpkg.EventSink) (reviewpkg.Request, error) {
	configuration, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         opts.Reviewers,
		Concurrency:       opts.Concurrency,
		Timeout:           opts.Timeout,
		Retries:           opts.Retries,
		ReviewerAgents:    opts.ReviewerAgents,
		ReviewerModel:     opts.ReviewerModel,
		SummarizerAgent:   opts.SummarizerAgent,
		SummarizerModel:   opts.SummarizerModel,
		SummarizerTimeout: opts.SummarizerTimeout,
		FPFilterTimeout:   opts.FPFilterTimeout,
		Guidance:          opts.Guidance,
		UseRefFile:        opts.UseRefFile,
		FPFilterEnabled:   opts.FPFilterEnabled,
		FPThreshold:       opts.FPThreshold,
		PRFeedbackEnabled: opts.PRFeedbackEnabled,
		PRFeedbackAgent:   opts.PRFeedbackAgent,
		ExcludePatterns:   opts.ExcludePatterns,
	})
	if err != nil {
		return reviewpkg.Request{}, err
	}
	engineVersion, _, _ := getVersionInfo()
	return reviewpkg.Request{
		Target: domain.ReviewTarget{
			RepositoryRoot: opts.RepositoryRoot,
			WorktreeRoot:   opts.WorkDir,
			Revision: domain.RevisionEvidence{
				RequestedBaseRef: opts.Base,
				ResolvedBaseRef:  resolvedBaseRef,
			},
			PullRequest: opts.PullRequest,
		},
		Trigger:             domain.ReviewTriggerManual,
		Engine:              domain.ReviewEngine{Name: "acr", Version: engineVersion},
		Configuration:       configuration,
		ConfigurationSource: configurationSourceIdentity(opts.ConfigSource),
		Events:              events,
	}, nil
}

func handleTypedReviewRun(ctx context.Context, opts ReviewOpts, run *domain.ReviewRun, logger *terminal.Logger) domain.ExitCode {
	if run == nil {
		logger.Log("Review failed: no review result returned", terminal.StyleError)
		return domain.ExitError
	}
	if run.Status == domain.ReviewStatusInterrupted {
		return domain.ExitInterrupted
	}
	if run.Status == domain.ReviewStatusFailed {
		return handleTypedReviewFailure(run, logger)
	}
	if run.Status != domain.ReviewStatusCompleted {
		logger.Logf(terminal.StyleError, "Review failed: unexpected review status %q", run.Status)
		return domain.ExitError
	}
	if run.Conclusion == domain.ReviewConclusionNoChanges {
		logger.Logf(terminal.StyleSuccess, "No changes detected between HEAD and %s. Nothing to review.", run.Target.Revision.ResolvedBaseRef)
		opts.record(OutcomeNoChanges)
		return domain.ExitNoFindings
	}
	if run.Conclusion != domain.ReviewConclusionClean && run.Conclusion != domain.ReviewConclusionFindings {
		logger.Logf(terminal.StyleError, "Review failed: unexpected review conclusion %q", run.Conclusion)
		return domain.ExitError
	}

	fmt.Println(runner.RenderReviewRun(*run))
	grouped := run.FinalGroupedFindings()
	if run.Conclusion == domain.ReviewConclusionClean {
		return handleLGTM(ctx, opts, run.RawFindings, run.AggregatedFindings, run.Dispositions, run.Stats, logger)
	}
	return handleFindings(ctx, opts, grouped, run.AggregatedFindings, run.Stats, logger)
}

func handleTypedReviewFailure(run *domain.ReviewRun, logger *terminal.Logger) domain.ExitCode {
	if run.Failure == nil {
		logger.Log("Review failed without failure evidence", terminal.StyleError)
		return domain.ExitError
	}
	if run.Failure.Phase == domain.ReviewPhaseSummarization && run.Summarizer.ExitCode != 0 && !strings.Contains(run.Failure.Message, "summarizer timed out") && !strings.Contains(run.Failure.Message, "read summarizer output") {
		fmt.Println(runner.RenderReviewRun(*run))
		return domain.ExitError
	}
	switch run.Failure.Phase {
	case domain.ReviewPhaseInitialization:
		message := run.Failure.Message
		if strings.Contains(message, "isolated post-processing workspace") {
			logger.Logf(terminal.StyleError, "Failed to create isolated post-processing workspace: %s", strings.TrimPrefix(message, "create isolated post-processing workspace: "))
		} else {
			logger.Logf(terminal.StyleError, "Review failed: %s", message)
		}
	case domain.ReviewPhaseRevisions, domain.ReviewPhaseDiff:
		logger.Logf(terminal.StyleError, "Failed to get diff: %s", run.Failure.Message)
	case domain.ReviewPhaseReviewers:
		if strings.Contains(run.Failure.Message, "all reviewers failed") {
			logger.Log("All reviewers failed", terminal.StyleError)
		} else {
			logger.Logf(terminal.StyleError, "Review failed: %s", run.Failure.Message)
		}
	case domain.ReviewPhaseSummarization:
		if strings.Contains(run.Failure.Message, "summarizer timed out") {
			logger.Logf(terminal.StyleError, "Summarizer timed out after %s", run.Configuration.Values().SummarizerTimeout)
		} else {
			logger.Logf(terminal.StyleError, "Summarizer error: %s", run.Failure.Message)
		}
	case domain.ReviewPhaseExcludeFilter:
		logger.Logf(terminal.StyleError, "Invalid exclude pattern: %s", run.Failure.Message)
	default:
		logger.Logf(terminal.StyleError, "Review failed: %s", run.Failure.Message)
	}
	return domain.ExitError
}

func configurationSourceIdentity(source config.SourceIdentity) domain.ConfigurationSourceIdentity {
	return domain.ConfigurationSourceIdentity{
		Kind:          source.Kind,
		Locator:       source.Locator,
		Ref:           source.Ref,
		Revision:      source.Revision,
		ConfigPresent: source.ConfigPresent,
		ConfigDigest:  source.ConfigDigest,
	}
}
