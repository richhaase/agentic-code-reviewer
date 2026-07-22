package review

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/feedback"
	"github.com/richhaase/agentic-code-reviewer/internal/filter"
	"github.com/richhaase/agentic-code-reviewer/internal/fpfilter"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/summarizer"
)

type Request struct {
	Target              domain.ReviewTarget
	Trigger             domain.ReviewTrigger
	Engine              domain.ReviewEngine
	Configuration       domain.ReviewConfiguration
	ConfigurationSource domain.ConfigurationSourceIdentity
	Events              EventSink
}

type AgentFactory func(string, string) (agent.Agent, error)

type RevisionProvider func(context.Context, domain.ReviewTarget) (domain.RevisionEvidence, error)

type DiffProvider func(context.Context, domain.ReviewTarget) (string, error)

type PriorFeedbackProvider func(context.Context, domain.ReviewTarget, string, string) (string, error)

type RunIDGenerator func(time.Time) (string, error)

type Option func(*serviceDependencies)

type serviceDependencies struct {
	now           func() time.Time
	newRunID      RunIDGenerator
	newAgent      AgentFactory
	revisions     RevisionProvider
	diff          DiffProvider
	priorFeedback PriorFeedbackProvider
}

type Service struct {
	dependencies serviceDependencies
}

func NewService(options ...Option) (*Service, error) {
	dependencies := serviceDependencies{
		now:           time.Now,
		newRunID:      defaultRunID,
		newAgent:      agent.NewAgentWithModel,
		revisions:     resolveRevisions,
		diff:          loadDiff,
		priorFeedback: summarizePriorFeedback,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("review service option cannot be nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil || dependencies.newRunID == nil || dependencies.newAgent == nil || dependencies.revisions == nil || dependencies.diff == nil || dependencies.priorFeedback == nil {
		return nil, fmt.Errorf("review service dependencies must not be nil")
	}
	return &Service{dependencies: dependencies}, nil
}

func WithClock(now func() time.Time) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.now = now
	}
}

func WithRunIDGenerator(generator RunIDGenerator) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.newRunID = generator
	}
}

func WithAgentFactory(factory AgentFactory) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.newAgent = factory
	}
}

func WithRevisionProvider(provider RevisionProvider) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.revisions = provider
	}
}

func WithDiffProvider(provider DiffProvider) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.diff = provider
	}
}

func WithPriorFeedbackProvider(provider PriorFeedbackProvider) Option {
	return func(dependencies *serviceDependencies) {
		dependencies.priorFeedback = provider
	}
}

func (s *Service) Run(ctx context.Context, request Request) (*domain.ReviewRun, error) {
	if s == nil {
		return nil, fmt.Errorf("review service is required")
	}
	if ctx == nil {
		return nil, fmt.Errorf("review context is required")
	}
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	startedAt := s.dependencies.now()
	runID, err := s.dependencies.newRunID(startedAt)
	if err != nil {
		return nil, fmt.Errorf("create review run ID: %w", err)
	}
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("create review run ID: empty ID")
	}

	run := &domain.ReviewRun{
		ID:                       runID,
		Target:                   request.Target.Clone(),
		Trigger:                  request.Trigger,
		Engine:                   request.Engine,
		StartedAt:                startedAt,
		Configuration:            request.Configuration,
		ConfigurationSource:      request.ConfigurationSource,
		ConfigurationFingerprint: request.Configuration.Fingerprint(),
	}
	emitter := newEventEmitter(request.Events, s.dependencies.now, runID)
	emitter.emit(Event{Kind: EventRunStarted})

	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseInitialization, err, emitter), nil
	}

	values := request.Configuration.Values()
	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseInitialization})
	reviewAgents, summarizerAgent, err := s.initializeAgents(values)
	if err != nil {
		return s.fail(run, domain.ReviewPhaseInitialization, err, emitter), nil
	}
	postProcessDir, cleanupPostProcessDir, err := agent.NewIsolatedWorkDir()
	if err != nil {
		return s.fail(run, domain.ReviewPhaseInitialization, fmt.Errorf("create isolated post-processing workspace: %w", err), emitter), nil
	}
	defer cleanupPostProcessDir()
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseInitialization})
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseInitialization, err, emitter), nil
	}

	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseRevisions})
	revision, err := s.dependencies.revisions(ctx, run.Target)
	if err != nil {
		return s.finishFromError(run, domain.ReviewPhaseRevisions, err, ctx, emitter), nil
	}
	if err := validateResolvedRevision(run.Target.Revision, revision); err != nil {
		return s.fail(run, domain.ReviewPhaseRevisions, err, emitter), nil
	}
	run.Target.Revision = revision
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseRevisions})
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseRevisions, err, emitter), nil
	}

	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseDiff})
	diff, err := s.dependencies.diff(ctx, run.Target)
	if err != nil {
		return s.finishFromError(run, domain.ReviewPhaseDiff, err, ctx, emitter), nil
	}
	confirmedRevision, err := s.dependencies.revisions(ctx, run.Target)
	if err != nil {
		return s.finishFromError(run, domain.ReviewPhaseDiff, err, ctx, emitter), nil
	}
	if err := validateResolvedRevision(run.Target.Revision, confirmedRevision); err != nil {
		return s.fail(run, domain.ReviewPhaseDiff, err, emitter), nil
	}
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseDiff})
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseDiff, err, emitter), nil
	}
	if diff == "" {
		return s.complete(run, domain.ReviewConclusionNoChanges, emitter), nil
	}

	feedbackTask := s.startPriorFeedback(ctx, run.Target, values, emitter)
	if feedbackTask != nil {
		emitter.setBeforeCompletion(feedbackTask.stop)
	}

	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseReviewers})
	reviewerEvents := runner.Events{
		ReviewerStarted: func(reviewerID int, agentName string) {
			emitter.emit(Event{Kind: EventReviewerStarted, Phase: domain.ReviewPhaseReviewers, ReviewerID: reviewerID, AgentName: agentName})
		},
		ReviewerOutput: func(reviewerID int, output string) {
			emitter.emit(Event{Kind: EventReviewerOutput, Phase: domain.ReviewPhaseReviewers, ReviewerID: reviewerID, Message: output})
		},
		ReviewerRetrying: func(reviewerID int, reason string, attempt, retries int, delay time.Duration) {
			message := fmt.Sprintf("%s; retry %d/%d in %s", reason, attempt, retries, delay)
			emitter.emit(Event{Kind: EventReviewerRetrying, Phase: domain.ReviewPhaseReviewers, ReviewerID: reviewerID, Message: message})
		},
		ReviewerCompleted: func(result domain.ReviewerResult) {
			for _, warning := range result.Warnings {
				emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseReviewers, ReviewerID: result.ReviewerID, AgentName: result.AgentName, Message: warning.Message})
			}
			emitter.emit(Event{Kind: EventReviewerCompleted, Phase: domain.ReviewPhaseReviewers, ReviewerID: result.ReviewerID, AgentName: result.AgentName, ReviewerResult: cloneReviewerResult(result)})
		},
	}
	reviewRunner, err := runner.NewHeadless(runner.Config{
		Reviewers:       values.Reviewers,
		Concurrency:     values.Concurrency,
		BaseRef:         run.Target.Revision.BaseObjectID,
		Timeout:         values.Timeout,
		Retries:         values.Retries,
		WorkDir:         run.Target.WorktreeRoot,
		Guidance:        values.Guidance,
		UseRefFile:      values.UseRefFile,
		Diff:            diff,
		DiffPrecomputed: true,
		Events:          reviewerEvents,
	}, reviewAgents)
	if err != nil {
		return s.fail(run, domain.ReviewPhaseReviewers, err, emitter), nil
	}
	results, wallClock, err := reviewRunner.Run(ctx)
	run.ReviewerResults = cloneReviewerResults(results)
	run.Stats = runner.BuildStats(results, values.Reviewers, wallClock)
	run.RawFindings = cloneFindings(runner.CollectFindings(results))
	run.AggregatedFindings = cloneAggregatedFindings(domain.AggregateFindings(run.RawFindings))
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseReviewers})
	if err != nil {
		return s.finishFromError(run, domain.ReviewPhaseReviewers, err, ctx, emitter), nil
	}
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseReviewers, err, emitter), nil
	}
	if run.Stats.AllFailed() {
		return s.fail(run, domain.ReviewPhaseReviewers, fmt.Errorf("all reviewers failed"), emitter), nil
	}
	s.emitReviewerWarnings(run.Stats, emitter)

	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseSummarization})
	summaryCtx, summaryCancel := context.WithTimeout(ctx, values.SummarizerTimeout)
	summaryResult, err := summarizer.SummarizeWithAgent(summaryCtx, summarizerAgent, run.AggregatedFindings, postProcessDir)
	summaryCancel()
	if summaryResult != nil {
		run.Stats.SummarizerDuration = summaryResult.Duration
		run.Summarizer = domain.SummarizerOutcome{
			ExitCode: summaryResult.ExitCode,
			Stderr:   boundedSummaryEvidence(summaryResult.Stderr),
			Duration: summaryResult.Duration,
		}
		for _, warning := range summaryResult.Warnings {
			message := boundedSummaryEvidence(warning)
			run.Summarizer.Warnings = append(run.Summarizer.Warnings, message)
			emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseSummarization, Message: message})
		}
		if summaryResult.ExitCode != 0 {
			run.Summarizer.DiagnosticOutput = boundedSummaryEvidence(summaryResult.RawOut)
		}
		run.PreFilterSummary = cloneGroupedFindings(summaryResult.Grouped)
		initializeRunFindingRecords(run)
	}
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseSummarization})
	if err != nil {
		return s.finishFromError(run, domain.ReviewPhaseSummarization, err, ctx, emitter), nil
	}
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseSummarization, err, emitter), nil
	}
	if summaryResult == nil {
		return s.fail(run, domain.ReviewPhaseSummarization, fmt.Errorf("summarizer returned no result"), emitter), nil
	}
	if summaryResult.ExitCode != 0 {
		message := strings.TrimSpace(run.Summarizer.Stderr)
		if message == "" {
			message = fmt.Sprintf("summarizer exited with code %d", summaryResult.ExitCode)
		}
		if ctx.Err() != nil {
			return s.interrupt(run, domain.ReviewPhaseSummarization, ctx.Err(), emitter), nil
		}
		return s.fail(run, domain.ReviewPhaseSummarization, errors.New(message), emitter), nil
	}

	priorFeedback, err := feedbackTask.receive(ctx, emitter)
	if err != nil {
		return s.interrupt(run, domain.ReviewPhaseFeedback, err, emitter), nil
	}
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseFeedback, err, emitter), nil
	}
	finalGrouped := cloneGroupedFindings(run.PreFilterSummary)
	var fpRemoved []domain.FPRemovedInfo
	fpRemovedByIndex := make(map[int]domain.FPRemovedInfo)
	run.FalsePositiveFilter.Enabled = values.FPFilterEnabled
	if values.FPFilterEnabled && len(finalGrouped.Findings) > 0 {
		emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseFalsePositiveFilter})
		fpCtx, fpCancel := context.WithTimeout(ctx, values.FPFilterTimeout)
		fpResult := fpfilter.NewWithAgent(summarizerAgent, values.FPThreshold, postProcessDir).Apply(fpCtx, finalGrouped, priorFeedback, run.Stats.SuccessfulReviewers)
		fpCancel()
		if fpResult != nil {
			finalGrouped = cloneGroupedFindings(fpResult.Grouped)
			run.FalsePositiveFilter.Applied = !fpResult.Skipped
			run.FalsePositiveFilter.Skipped = fpResult.Skipped
			run.FalsePositiveFilter.SkipReason = fpResult.SkipReason
			run.FalsePositiveFilter.EvalErrors = fpResult.EvalErrors
			run.FalsePositiveFilter.Duration = fpResult.Duration
			for _, warning := range fpResult.Warnings {
				message := boundedSummaryEvidence(warning)
				run.FalsePositiveFilter.Warnings = append(run.FalsePositiveFilter.Warnings, message)
				emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseFalsePositiveFilter, Message: message})
			}
			run.Stats.FPFilterDuration = fpResult.Duration
			run.Stats.FPFilteredCount = fpResult.RemovedCount
			for _, removed := range fpResult.Removed {
				info := domain.FPRemovedInfo{
					Sources:   slices.Clone(removed.Finding.Sources),
					FPScore:   removed.FPScore,
					Reasoning: removed.Reasoning,
					Title:     removed.Finding.Title,
				}
				fpRemovedByIndex[removed.Index] = info
				fpRemoved = append(fpRemoved, info)
			}
			if fpResult.Skipped {
				emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseFalsePositiveFilter, Message: "false-positive filter skipped: " + fpResult.SkipReason})
			}
		}
		emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseFalsePositiveFilter})
		if err := ctx.Err(); err != nil {
			run.Dispositions = domain.BuildDispositions(
				len(run.AggregatedFindings),
				run.PreFilterSummary.Info,
				fpRemoved,
				nil,
				finalGrouped.Findings,
			)
			populateRunFindings(run, finalGrouped, fpRemovedByIndex, nil)
			return s.interrupt(run, domain.ReviewPhaseFalsePositiveFilter, err, emitter), nil
		}
	}

	var excludeRemoved []domain.FindingGroup
	run.ExcludeFilter.Patterns = slices.Clone(values.ExcludePatterns)
	if len(values.ExcludePatterns) > 0 {
		emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseExcludeFilter})
		exclude, err := filter.New(values.ExcludePatterns)
		if err != nil {
			return s.fail(run, domain.ReviewPhaseExcludeFilter, err, emitter), nil
		}
		before := cloneGroupedFindings(finalGrouped)
		finalGrouped = exclude.Apply(finalGrouped)
		excludeRemoved = diffFindingGroups(before.Findings, finalGrouped.Findings)
		emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseExcludeFilter})
		if err := ctx.Err(); err != nil {
			run.Dispositions = domain.BuildDispositions(
				len(run.AggregatedFindings),
				run.PreFilterSummary.Info,
				fpRemoved,
				excludeRemoved,
				finalGrouped.Findings,
			)
			populateRunFindings(run, finalGrouped, fpRemovedByIndex, excludeRemoved)
			return s.interrupt(run, domain.ReviewPhaseExcludeFilter, err, emitter), nil
		}
	}

	run.Dispositions = domain.BuildDispositions(
		len(run.AggregatedFindings),
		run.PreFilterSummary.Info,
		fpRemoved,
		excludeRemoved,
		finalGrouped.Findings,
	)
	populateRunFindings(run, finalGrouped, fpRemovedByIndex, excludeRemoved)
	if err := ctx.Err(); err != nil {
		return s.interrupt(run, domain.ReviewPhaseComplete, err, emitter), nil
	}

	if len(run.Findings) == 0 {
		return s.complete(run, domain.ReviewConclusionClean, emitter), nil
	}
	return s.complete(run, domain.ReviewConclusionFindings, emitter), nil
}

func validateRequest(request Request) error {
	var invalid []string
	if err := request.Target.Validate(); err != nil {
		invalid = append(invalid, err.Error())
	}
	if err := request.Trigger.Validate(); err != nil {
		invalid = append(invalid, err.Error())
	}
	if err := request.Engine.Validate(); err != nil {
		invalid = append(invalid, err.Error())
	}
	if err := request.Configuration.Validate(); err != nil {
		invalid = append(invalid, err.Error())
	}
	if err := request.ConfigurationSource.Validate(); err != nil {
		invalid = append(invalid, err.Error())
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid review request: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func (s *Service) initializeAgents(values domain.ReviewConfigurationValues) ([]agent.Agent, agent.Agent, error) {
	reviewAgents := make([]agent.Agent, 0, len(values.ReviewerAgents))
	seen := make(map[string]agent.Agent)
	for _, name := range values.ReviewerAgents {
		reviewAgent, ok := seen[name]
		if !ok {
			var err error
			reviewAgent, err = s.dependencies.newAgent(name, values.ReviewerModel)
			if err != nil {
				return nil, nil, fmt.Errorf("create reviewer agent %q: %w", name, err)
			}
			if reviewAgent == nil {
				return nil, nil, fmt.Errorf("create reviewer agent %q: no agent returned", name)
			}
			if err := reviewAgent.IsAvailable(); err != nil {
				return nil, nil, fmt.Errorf("reviewer agent %q unavailable: %w", name, err)
			}
			seen[name] = reviewAgent
		}
		reviewAgents = append(reviewAgents, reviewAgent)
	}

	summarizerAgent, err := s.dependencies.newAgent(values.SummarizerAgent, values.SummarizerModel)
	if err != nil {
		return nil, nil, fmt.Errorf("create summarizer agent %q: %w", values.SummarizerAgent, err)
	}
	if summarizerAgent == nil {
		return nil, nil, fmt.Errorf("create summarizer agent %q: no agent returned", values.SummarizerAgent)
	}
	if err := summarizerAgent.IsAvailable(); err != nil {
		return nil, nil, fmt.Errorf("summarizer agent %q unavailable: %w", values.SummarizerAgent, err)
	}
	return reviewAgents, summarizerAgent, nil
}

type priorFeedbackResult struct {
	summary string
	err     error
}

type priorFeedbackTask struct {
	result <-chan priorFeedbackResult
	done   <-chan struct{}
	cancel context.CancelFunc
}

func (s *Service) startPriorFeedback(ctx context.Context, target domain.ReviewTarget, values domain.ReviewConfigurationValues, emitter *eventEmitter) *priorFeedbackTask {
	if !values.PRFeedbackEnabled || !values.FPFilterEnabled || target.PullRequest == nil {
		return nil
	}
	result := make(chan priorFeedbackResult, 1)
	done := make(chan struct{})
	feedbackCtx, cancel := context.WithTimeout(ctx, values.SummarizerTimeout)
	task := &priorFeedbackTask{result: result, done: done, cancel: cancel}
	emitter.emit(Event{Kind: EventPhaseStarted, Phase: domain.ReviewPhaseFeedback})
	go func() {
		defer close(done)
		feedbackAgent := values.PRFeedbackAgent
		if feedbackAgent == "" {
			feedbackAgent = values.SummarizerAgent
		}
		summary, err := s.dependencies.priorFeedback(feedbackCtx, target, feedbackAgent, values.SummarizerModel)
		result <- priorFeedbackResult{summary: summary, err: err}
	}()
	return task
}

func (t *priorFeedbackTask) receive(ctx context.Context, emitter *eventEmitter) (string, error) {
	if t == nil {
		return "", nil
	}
	var feedbackResult priorFeedbackResult
	select {
	case feedbackResult = <-t.result:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	emitter.emit(Event{Kind: EventPhaseCompleted, Phase: domain.ReviewPhaseFeedback})
	if feedbackResult.err != nil {
		message := "prior feedback unavailable: " + feedbackResult.err.Error()
		if feedbackResult.summary != "" {
			message = "prior feedback warning: " + feedbackResult.err.Error()
		}
		emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseFeedback, Message: message})
		return feedbackResult.summary, nil
	}
	return feedbackResult.summary, nil
}

func (t *priorFeedbackTask) stop() {
	if t == nil {
		return
	}
	t.cancel()
	<-t.done
}

func (s *Service) emitReviewerWarnings(stats domain.ReviewStats, emitter *eventEmitter) {
	if stats.ParseErrors > 0 {
		emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseReviewers, Message: fmt.Sprintf("reviewer parse errors: %d", stats.ParseErrors)})
	}
	if len(stats.FailedReviewers) > 0 {
		emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseReviewers, Message: fmt.Sprintf("failed reviewers: %v", stats.FailedReviewers)})
	}
	if len(stats.TimedOutReviewers) > 0 {
		emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseReviewers, Message: fmt.Sprintf("timed out reviewers: %v", stats.TimedOutReviewers)})
	}
	if len(stats.AuthFailedReviewers) > 0 {
		emitter.emit(Event{Kind: EventWarning, Phase: domain.ReviewPhaseReviewers, Message: fmt.Sprintf("authentication failed reviewers: %v", stats.AuthFailedReviewers)})
	}
}

func (s *Service) complete(run *domain.ReviewRun, conclusion domain.ReviewConclusion, emitter *eventEmitter) *domain.ReviewRun {
	run.Status = domain.ReviewStatusCompleted
	run.Conclusion = conclusion
	emitter.runBeforeCompletion()
	run.CompletedAt = s.dependencies.now()
	emitter.emit(Event{Kind: EventRunCompleted, Phase: domain.ReviewPhaseComplete, Status: run.Status, Conclusion: run.Conclusion})
	emitter.close()
	return run
}

func (s *Service) fail(run *domain.ReviewRun, phase domain.ReviewPhase, err error, emitter *eventEmitter) *domain.ReviewRun {
	run.Status = domain.ReviewStatusFailed
	run.Conclusion = domain.ReviewConclusionNone
	run.Failure = &domain.ReviewFailure{Phase: phase, Message: err.Error()}
	emitter.runBeforeCompletion()
	run.CompletedAt = s.dependencies.now()
	emitter.emit(Event{Kind: EventRunCompleted, Phase: phase, Message: err.Error(), Status: run.Status})
	emitter.close()
	return run
}

func (s *Service) interrupt(run *domain.ReviewRun, phase domain.ReviewPhase, err error, emitter *eventEmitter) *domain.ReviewRun {
	run.Status = domain.ReviewStatusInterrupted
	run.Conclusion = domain.ReviewConclusionNone
	run.Failure = &domain.ReviewFailure{Phase: phase, Message: err.Error()}
	emitter.runBeforeCompletion()
	run.CompletedAt = s.dependencies.now()
	emitter.emit(Event{Kind: EventRunCompleted, Phase: phase, Message: err.Error(), Status: run.Status})
	emitter.close()
	return run
}

func (s *Service) finishFromError(run *domain.ReviewRun, phase domain.ReviewPhase, err error, ctx context.Context, emitter *eventEmitter) *domain.ReviewRun {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return s.interrupt(run, phase, err, emitter)
	}
	return s.fail(run, phase, err, emitter)
}

func validateResolvedRevision(expected, actual domain.RevisionEvidence) error {
	if strings.TrimSpace(actual.RequestedBaseRef) == "" || strings.TrimSpace(actual.ResolvedBaseRef) == "" || strings.TrimSpace(actual.HeadObjectID) == "" || strings.TrimSpace(actual.BaseObjectID) == "" {
		return fmt.Errorf("revision provider returned incomplete evidence")
	}
	if actual.RequestedBaseRef != expected.RequestedBaseRef {
		return fmt.Errorf("requested base ref changed from %q to %q", expected.RequestedBaseRef, actual.RequestedBaseRef)
	}
	if actual.ResolvedBaseRef != expected.ResolvedBaseRef {
		return fmt.Errorf("resolved base ref changed from %q to %q", expected.ResolvedBaseRef, actual.ResolvedBaseRef)
	}
	if expected.HeadObjectID != "" && expected.HeadObjectID != actual.HeadObjectID {
		return fmt.Errorf("review head changed from %s to %s", expected.HeadObjectID, actual.HeadObjectID)
	}
	if expected.BaseObjectID != "" && expected.BaseObjectID != actual.BaseObjectID {
		return fmt.Errorf("review base changed from %s to %s", expected.BaseObjectID, actual.BaseObjectID)
	}
	return nil
}

func resolveRevisions(ctx context.Context, target domain.ReviewTarget) (domain.RevisionEvidence, error) {
	revision := target.Revision
	headObjectID, err := git.ResolveCommit(ctx, target.WorktreeRoot, "HEAD")
	if err != nil {
		return domain.RevisionEvidence{}, err
	}
	baseObjectID, err := git.ResolveCommit(ctx, target.WorktreeRoot, revision.ResolvedBaseRef)
	if err != nil {
		return domain.RevisionEvidence{}, err
	}
	revision.HeadObjectID = headObjectID
	revision.BaseObjectID = baseObjectID
	return revision, nil
}

func loadDiff(ctx context.Context, target domain.ReviewTarget) (string, error) {
	return git.GetDiff(ctx, target.Revision.BaseObjectID, target.WorktreeRoot)
}

func summarizePriorFeedback(ctx context.Context, target domain.ReviewTarget, agentName, model string) (string, error) {
	if target.PullRequest == nil {
		return "", nil
	}
	agentDir, cleanupAgentDir, err := agent.NewIsolatedWorkDir()
	if err != nil {
		return "", fmt.Errorf("create isolated feedback workspace: %w", err)
	}
	defer cleanupAgentDir()
	summary := feedback.NewSummarizer(agentName, model, false, nil)
	return summary.SummarizePullRequestFromDirs(ctx, *target.PullRequest, target.WorktreeRoot, agentDir)
}

func defaultRunID(startedAt time.Time) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return startedAt.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random[:]), nil
}

const (
	summaryDiagnosticMaxLines         = 10
	summaryDiagnosticMaxBytes         = 4096
	summaryDiagnosticTruncationMarker = "\n[truncated]"
)

func boundedSummaryEvidence(output string) string {
	lines := strings.Split(output, "\n")
	truncated := len(lines) > summaryDiagnosticMaxLines
	if truncated {
		lines = lines[:summaryDiagnosticMaxLines]
	}
	diagnostic := strings.Join(lines, "\n")
	if len(diagnostic) > summaryDiagnosticMaxBytes {
		truncated = true
	}
	if truncated {
		limit := summaryDiagnosticMaxBytes - len(summaryDiagnosticTruncationMarker)
		if len(diagnostic) > limit {
			for limit > 0 && !utf8.RuneStart(diagnostic[limit]) {
				limit--
			}
			diagnostic = diagnostic[:limit]
		}
		return diagnostic + summaryDiagnosticTruncationMarker
	}
	return diagnostic
}

func populateRunFindings(run *domain.ReviewRun, final domain.GroupedFindings, fpRemovedByIndex map[int]domain.FPRemovedInfo, excludeRemoved []domain.FindingGroup) {
	if len(run.FindingRecords) == 0 && run.PreFilterSummary.TotalGroups() > 0 {
		initializeRunFindingRecords(run)
	}
	run.Findings = nil
	run.Info = nil
	run.FalsePositiveFilter.Removed = nil
	run.ExcludeFilter.Removed = nil
	finalRemaining := cloneFindingGroups(final.Findings)
	excludeRemaining := cloneFindingGroups(excludeRemoved)
	actionableIndex := 0
	for i, finding := range run.FindingRecords {
		if finding.Kind == domain.ReviewFindingActionable {
			finding.Disposition = dispositionForFinding(actionableIndex, finding.Group, &finalRemaining, fpRemovedByIndex, &excludeRemaining)
			run.FindingRecords[i] = finding
			actionableIndex++
		}
		switch finding.Disposition.Kind {
		case domain.DispositionSurvived:
			run.Findings = append(run.Findings, finding)
		case domain.DispositionInfo:
			run.Info = append(run.Info, finding)
		case domain.DispositionFilteredFP:
			run.FalsePositiveFilter.Removed = append(run.FalsePositiveFilter.Removed, finding)
		case domain.DispositionFilteredExclude:
			run.ExcludeFilter.Removed = append(run.ExcludeFilter.Removed, finding)
		}
	}
}

func initializeRunFindingRecords(run *domain.ReviewRun) {
	records := make([]domain.ReviewFinding, 0, run.PreFilterSummary.TotalGroups())
	for i, group := range run.PreFilterSummary.Findings {
		records = append(records, domain.ReviewFinding{
			ID:    fmt.Sprintf("finding-%03d", i+1),
			Kind:  domain.ReviewFindingActionable,
			Group: cloneFindingGroup(group),
			Disposition: domain.Disposition{
				GroupTitle: group.Title,
			},
		})
	}
	for i, group := range run.PreFilterSummary.Info {
		records = append(records, domain.ReviewFinding{
			ID:    fmt.Sprintf("info-%03d", i+1),
			Kind:  domain.ReviewFindingInformational,
			Group: cloneFindingGroup(group),
			Disposition: domain.Disposition{
				Kind:       domain.DispositionInfo,
				GroupTitle: group.Title,
			},
		})
	}
	run.FindingRecords = records
}

func dispositionForFinding(index int, group domain.FindingGroup, final *[]domain.FindingGroup, fpRemoved map[int]domain.FPRemovedInfo, excludeRemoved *[]domain.FindingGroup) domain.Disposition {
	if info, ok := fpRemoved[index]; ok {
		return domain.Disposition{Kind: domain.DispositionFilteredFP, GroupTitle: group.Title, FPScore: info.FPScore, Reasoning: info.Reasoning}
	}
	if consumeFindingGroup(final, group) {
		return domain.Disposition{Kind: domain.DispositionSurvived, GroupTitle: group.Title}
	}
	if consumeFindingGroup(excludeRemoved, group) {
		return domain.Disposition{Kind: domain.DispositionFilteredExclude, GroupTitle: group.Title}
	}
	return domain.Disposition{GroupTitle: group.Title}
}

func consumeFindingGroup(groups *[]domain.FindingGroup, candidate domain.FindingGroup) bool {
	for i, group := range *groups {
		if sameFindingGroup(group, candidate) {
			*groups = append((*groups)[:i], (*groups)[i+1:]...)
			return true
		}
	}
	return false
}

func cloneFindingGroups(groups []domain.FindingGroup) []domain.FindingGroup {
	cloned := make([]domain.FindingGroup, len(groups))
	for i, group := range groups {
		cloned[i] = cloneFindingGroup(group)
	}
	return cloned
}

func diffFindingGroups(before, after []domain.FindingGroup) []domain.FindingGroup {
	j := 0
	var removed []domain.FindingGroup
	for i := range before {
		if j < len(after) && sameFindingGroup(before[i], after[j]) {
			j++
		} else {
			removed = append(removed, cloneFindingGroup(before[i]))
		}
	}
	return removed
}

func sameFindingGroup(left, right domain.FindingGroup) bool {
	return left.Title == right.Title &&
		left.Summary == right.Summary &&
		left.ReviewerCount == right.ReviewerCount &&
		slices.Equal(left.Messages, right.Messages) &&
		slices.Equal(left.Sources, right.Sources)
}

func cloneReviewerResults(results []domain.ReviewerResult) []domain.ReviewerResult {
	cloned := make([]domain.ReviewerResult, len(results))
	for i, result := range results {
		cloned[i] = cloneReviewerResult(result)
	}
	return cloned
}

func cloneReviewerResult(result domain.ReviewerResult) domain.ReviewerResult {
	result.Findings = cloneFindings(result.Findings)
	result.Warnings = slices.Clone(result.Warnings)
	if result.Failure != nil {
		failure := *result.Failure
		result.Failure = &failure
	}
	return result
}

func cloneFindings(findings []domain.Finding) []domain.Finding {
	return slices.Clone(findings)
}

func cloneAggregatedFindings(findings []domain.AggregatedFinding) []domain.AggregatedFinding {
	cloned := make([]domain.AggregatedFinding, len(findings))
	for i, finding := range findings {
		cloned[i] = finding
		cloned[i].Reviewers = slices.Clone(finding.Reviewers)
	}
	return cloned
}

func cloneGroupedFindings(grouped domain.GroupedFindings) domain.GroupedFindings {
	cloned := domain.GroupedFindings{
		Findings: make([]domain.FindingGroup, len(grouped.Findings)),
		Info:     make([]domain.FindingGroup, len(grouped.Info)),
	}
	for i, finding := range grouped.Findings {
		cloned.Findings[i] = cloneFindingGroup(finding)
	}
	for i, finding := range grouped.Info {
		cloned.Info[i] = cloneFindingGroup(finding)
	}
	return cloned
}

func cloneFindingGroup(group domain.FindingGroup) domain.FindingGroup {
	group.Messages = slices.Clone(group.Messages)
	group.Sources = slices.Clone(group.Sources)
	return group
}
