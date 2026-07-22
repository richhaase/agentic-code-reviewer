package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

type capturedReviewService struct {
	request reviewpkg.Request
	run     *domain.ReviewRun
	err     error
}

type noopReviewEventSink struct{}

func (*noopReviewEventSink) HandleReviewEvent(reviewpkg.Event) {}

func (s *capturedReviewService) Run(_ context.Context, request reviewpkg.Request) (*domain.ReviewRun, error) {
	s.request = request
	return s.run, s.err
}

func typedReviewOpts() ReviewOpts {
	return ReviewOpts{
		ResolvedConfig: config.ResolvedConfig{
			Reviewers:         3,
			Concurrency:       2,
			Base:              "main",
			Timeout:           2 * time.Minute,
			Retries:           2,
			ReviewerAgents:    []string{"codex", "claude"},
			ReviewerModel:     "review-model",
			SummarizerAgent:   "codex",
			SummarizerModel:   "summary-model",
			SummarizerTimeout: 3 * time.Minute,
			FPFilterTimeout:   4 * time.Minute,
			Guidance:          "trusted guidance",
			FPFilterEnabled:   true,
			FPThreshold:       81,
			PRFeedbackEnabled: true,
			PRFeedbackAgent:   "claude",
		},
		Local:           true,
		DetectedPR:      "195",
		UseRefFile:      true,
		ExcludePatterns: []string{"generated"},
		RepositoryRoot:  "/tmp/repository",
		WorkDir:         "/tmp/repository/worktree",
		PullRequest: &domain.PullRequestKey{
			Host:       "github.com",
			Owner:      "richhaase",
			Repository: "agentic-code-reviewer",
			Number:     195,
		},
		ConfigSource: config.SourceIdentity{
			Kind:          config.SourceKindRepositoryRevision,
			Locator:       "/tmp/repository",
			Ref:           "refs/acr/trusted-config/origin/main",
			Revision:      "trusted-revision",
			ConfigPresent: true,
			ConfigDigest:  "trusted-digest",
		},
	}
}

func TestNewReviewRequestMapsOrdinaryCLIInputs(t *testing.T) {
	opts := typedReviewOpts()
	sink := &noopReviewEventSink{}
	request, err := newReviewRequest(opts, "origin/main", sink)
	if err != nil {
		t.Fatal(err)
	}
	if request.Trigger != domain.ReviewTriggerManual {
		t.Fatalf("trigger = %q", request.Trigger)
	}
	if request.Target.RepositoryRoot != opts.RepositoryRoot || request.Target.WorktreeRoot != opts.WorkDir {
		t.Fatalf("target roots = %#v", request.Target)
	}
	if request.Target.Revision.RequestedBaseRef != "main" || request.Target.Revision.ResolvedBaseRef != "origin/main" {
		t.Fatalf("target revision = %#v", request.Target.Revision)
	}
	if request.Target.PullRequest == nil || *request.Target.PullRequest != *opts.PullRequest {
		t.Fatalf("pull request = %#v", request.Target.PullRequest)
	}
	if request.Engine.Name != "acr" || request.Engine.Version == "" {
		t.Fatalf("engine = %#v", request.Engine)
	}
	if request.Events != sink {
		t.Fatal("event sink was not preserved")
	}
	wantSource := configurationSourceIdentity(opts.ConfigSource)
	if request.ConfigurationSource != wantSource {
		t.Fatalf("configuration source = %#v, want %#v", request.ConfigurationSource, wantSource)
	}
	values := request.Configuration.Values()
	wantValues := domain.ReviewConfigurationValues{
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
	}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("configuration values = %#v, want %#v", values, wantValues)
	}
}

func TestExecuteTypedReviewPassesRequestAndHandlesNoChanges(t *testing.T) {
	opts := typedReviewOpts()
	outcome := &CycleOutcome{}
	opts.Outcome = outcome
	service := &capturedReviewService{run: &domain.ReviewRun{
		Status:     domain.ReviewStatusCompleted,
		Conclusion: domain.ReviewConclusionNoChanges,
		Target: domain.ReviewTarget{Revision: domain.RevisionEvidence{
			ResolvedBaseRef: "origin/main",
		}},
	}}
	sink := &noopReviewEventSink{}
	code := executeTypedReview(context.Background(), opts, "origin/main", service, sink, terminal.NewLogger())
	if code != domain.ExitNoFindings || outcome.Kind != OutcomeNoChanges {
		t.Fatalf("code = %d, outcome = %d", code, outcome.Kind)
	}
	if service.request.Trigger != domain.ReviewTriggerManual || service.request.Events != sink {
		t.Fatalf("request = %#v", service.request)
	}
}

func TestHandleTypedReviewRunPreservesCompletedOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		conclusion domain.ReviewConclusion
		want       domain.ExitCode
	}{
		{name: "clean", conclusion: domain.ReviewConclusionClean, want: domain.ExitNoFindings},
		{name: "findings", conclusion: domain.ReviewConclusionFindings, want: domain.ExitFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := typedReviewOpts()
			group := domain.FindingGroup{Title: "Finding", Summary: "Evidence", Sources: []int{0}}
			run := &domain.ReviewRun{
				Status:     domain.ReviewStatusCompleted,
				Conclusion: tt.conclusion,
				Stats:      domain.ReviewStats{TotalReviewers: 3, SuccessfulReviewers: 2, FailedReviewers: []int{3}},
				Summarizer: domain.SummarizerOutcome{ExitCode: 0},
			}
			if tt.conclusion == domain.ReviewConclusionFindings {
				run.Findings = []domain.ReviewFinding{{ID: "finding-001", Kind: domain.ReviewFindingActionable, Group: group}}
			}
			if got := handleTypedReviewRun(context.Background(), opts, run, terminal.NewLogger()); got != tt.want {
				t.Fatalf("exit = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHandleTypedReviewRunPreservesFailuresAndInterruptions(t *testing.T) {
	configuration, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         1,
		Concurrency:       1,
		Timeout:           time.Minute,
		ReviewerAgents:    []string{"codex"},
		SummarizerAgent:   "codex",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		FPThreshold:       75,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  *domain.ReviewRun
		want domain.ExitCode
	}{
		{
			name: "interrupted",
			run:  &domain.ReviewRun{Status: domain.ReviewStatusInterrupted},
			want: domain.ExitInterrupted,
		},
		{
			name: "all reviewers failed",
			run: &domain.ReviewRun{
				Status:  domain.ReviewStatusFailed,
				Failure: &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: "all reviewers failed"},
			},
			want: domain.ExitError,
		},
		{
			name: "summarizer timeout",
			run: &domain.ReviewRun{
				Status:        domain.ReviewStatusFailed,
				Configuration: configuration,
				Failure:       &domain.ReviewFailure{Phase: domain.ReviewPhaseSummarization, Message: "summarizer timed out after 1m0s"},
			},
			want: domain.ExitError,
		},
		{
			name: "summarizer read failure",
			run: &domain.ReviewRun{
				Status:     domain.ReviewStatusFailed,
				Summarizer: domain.SummarizerOutcome{ExitCode: -1, DiagnosticOutput: "partial output"},
				Failure:    &domain.ReviewFailure{Phase: domain.ReviewPhaseSummarization, Message: "read summarizer output: stream failed"},
			},
			want: domain.ExitError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handleTypedReviewRun(context.Background(), typedReviewOpts(), tt.run, terminal.NewLogger()); got != tt.want {
				t.Fatalf("exit = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExecuteTypedReviewPreservesServiceErrors(t *testing.T) {
	service := &capturedReviewService{err: errors.New("service unavailable")}
	code := executeTypedReview(context.Background(), typedReviewOpts(), "origin/main", service, nil, terminal.NewLogger())
	if code != domain.ExitError {
		t.Fatalf("exit = %d", code)
	}
}

func TestCLIReviewEventsCompletesEveryProgressPhase(t *testing.T) {
	events := newCLIReviewEvents(ReviewOpts{ResolvedConfig: config.ResolvedConfig{Reviewers: 2}}, terminal.NewLogger())
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseStarted, Phase: domain.ReviewPhaseReviewers})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventReviewerCompleted})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventReviewerCompleted})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseCompleted, Phase: domain.ReviewPhaseReviewers})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseStarted, Phase: domain.ReviewPhaseSummarization})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseCompleted, Phase: domain.ReviewPhaseSummarization})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseStarted, Phase: domain.ReviewPhaseFalsePositiveFilter})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventPhaseCompleted, Phase: domain.ReviewPhaseFalsePositiveFilter})
	events.HandleReviewEvent(reviewpkg.Event{Kind: reviewpkg.EventRunCompleted})
	events.Close()
}
