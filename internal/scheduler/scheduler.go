package scheduler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/automatic"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

var ErrAutomaticReviewStopped = errors.New("automatic review is stopped")

type Lifecycle interface {
	Run(context.Context) watch.ExitReason
}

type Job struct {
	Key       store.PullRequestKeyV1
	Lifecycle Lifecycle
}

type Result struct {
	Key    store.PullRequestKeyV1
	Reason watch.ExitReason
}

type Scheduler struct {
	capacity chan struct{}
	mu       sync.Mutex
	active   map[store.PullRequestKeyV1]struct{}
}

func New(concurrency int) (*Scheduler, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("scheduler concurrency must be positive")
	}
	return &Scheduler{
		capacity: make(chan struct{}, concurrency),
		active:   make(map[store.PullRequestKeyV1]struct{}),
	}, nil
}

func (s *Scheduler) Run(ctx context.Context, jobs []Job) ([]Result, error) {
	if s == nil {
		return nil, fmt.Errorf("scheduler is required")
	}
	if ctx == nil {
		return nil, fmt.Errorf("scheduler context is required")
	}
	seen := make(map[store.PullRequestKeyV1]struct{}, len(jobs))
	for _, job := range jobs {
		if err := job.Key.Validate(); err != nil {
			return nil, err
		}
		if job.Lifecycle == nil {
			return nil, fmt.Errorf("scheduler lifecycle is required for %s", job.Key.String())
		}
		if _, exists := seen[job.Key]; exists {
			return nil, fmt.Errorf("pull request %s is already scheduled", job.Key.String())
		}
		seen[job.Key] = struct{}{}
	}
	s.mu.Lock()
	for key := range seen {
		if _, exists := s.active[key]; exists {
			s.mu.Unlock()
			return nil, fmt.Errorf("pull request %s is already scheduled", key.String())
		}
	}
	for key := range seen {
		s.active[key] = struct{}{}
	}
	s.mu.Unlock()

	results := make([]Result, len(jobs))
	var group sync.WaitGroup
	for index, job := range jobs {
		group.Add(1)
		go func() {
			defer group.Done()
			defer s.release(job.Key)
			select {
			case s.capacity <- struct{}{}:
				defer func() { <-s.capacity }()
			case <-ctx.Done():
				results[index] = Result{Key: job.Key, Reason: watch.ReasonInterrupted}
				return
			}
			results[index] = Result{Key: job.Key, Reason: job.Lifecycle.Run(ctx)}
		}()
	}
	group.Wait()
	return results, nil
}

func (s *Scheduler) release(key store.PullRequestKeyV1) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
}

type Controller interface {
	AuthorizeReview(store.PullRequestKeyV1, store.ReviewTargetV1, automatic.TrustedPolicy, string) (automatic.Decision, error)
	RecordEconomics(store.PullRequestKeyV1, time.Time, store.ReviewEconomicsV1) error
}

type PreparedCycle struct {
	Policy automatic.TrustedPolicy
	Work   BackgroundWork
}

func NewPreparedCycle(work BackgroundWork, policy store.AdjudicationPolicyV1) (PreparedCycle, error) {
	if work.runID == "" || work.run == nil {
		return PreparedCycle{}, fmt.Errorf("prepared background review work is required")
	}
	trusted, err := automatic.NewTrustedPolicy(policy, work.target)
	if err != nil {
		return PreparedCycle{}, err
	}
	return PreparedCycle{Policy: trusted, Work: work}, nil
}

type PrepareCycle func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error)

type BackgroundWork struct {
	runID  string
	target store.ReviewTargetV1
	run    func(context.Context) (*domain.ReviewRun, error)
}

func NewBackgroundWork(ctx context.Context, service *reviewpkg.Service, request reviewpkg.Request) (BackgroundWork, error) {
	if service == nil {
		return BackgroundWork{}, fmt.Errorf("background semantic review service is required")
	}
	if request.Trigger != domain.ReviewTriggerDesk {
		return BackgroundWork{}, fmt.Errorf("background review trigger must be %q", domain.ReviewTriggerDesk)
	}
	request.Events = nil
	prepared, err := service.Prepare(ctx, request)
	if err != nil {
		return BackgroundWork{}, err
	}
	target := store.ToReviewTargetSchema(prepared.Target())
	if target.PullRequest == nil {
		return BackgroundWork{}, fmt.Errorf("background review target must identify a pull request")
	}
	return BackgroundWork{
		runID:  prepared.ID(),
		target: target,
		run: func(ctx context.Context) (*domain.ReviewRun, error) {
			return prepared.Run(ctx)
		},
	}, nil
}

type BackgroundReview struct {
	Key        store.PullRequestKeyV1
	Controller Controller
	Runs       store.RunStore
	Prepare    PrepareCycle
	Now        func() time.Time
}

func (review BackgroundReview) Port() (watch.ReviewExecution, error) {
	if err := review.Key.Validate(); err != nil {
		return watch.ReviewExecution{}, err
	}
	if review.Controller == nil {
		return watch.ReviewExecution{}, fmt.Errorf("automatic review controller is required")
	}
	if review.Runs == nil {
		return watch.ReviewExecution{}, fmt.Errorf("automatic review run store is required")
	}
	if review.Prepare == nil {
		return watch.ReviewExecution{}, fmt.Errorf("automatic review preparation is required")
	}
	now := review.Now
	if now == nil {
		now = time.Now
	}
	return watch.ReviewExecution{RunCycle: func(ctx context.Context, reviewNumber int, trigger string, discussion []watch.Discussion, discussionRevision string) (watch.Cycle, error) {
		prepared, err := review.Prepare(ctx, reviewNumber, trigger, discussion, discussionRevision)
		if err != nil {
			return watch.Cycle{}, err
		}
		if prepared.Work.runID == "" {
			return watch.Cycle{}, fmt.Errorf("prepared automatic review run id is required")
		}
		if prepared.Work.run == nil {
			return watch.Cycle{}, fmt.Errorf("prepared background review work is required")
		}
		if prepared.Work.target.Revision.HeadObjectID == "" || prepared.Work.target.Revision.BaseObjectID == "" {
			return watch.Cycle{}, fmt.Errorf("prepared background review target must have resolved object ids")
		}
		decision, err := review.Controller.AuthorizeReview(review.Key, prepared.Work.target, prepared.Policy, prepared.Work.runID)
		if err != nil {
			if errors.Is(err, automatic.ErrRevisionAlreadyAuthorized) {
				return watch.Cycle{Result: watch.CycleAlreadyReviewed, HeadSHA: prepared.Work.target.Revision.HeadObjectID}, nil
			}
			return watch.Cycle{}, err
		}
		if !decision.Allowed {
			return watch.Cycle{}, automaticTermination{kind: decision.Kind, reason: decision.Reason}
		}
		runCtx := ctx
		cancel := func() {}
		if !decision.Deadline.IsZero() {
			runCtx, cancel = context.WithDeadline(ctx, decision.Deadline)
		}
		run, runErr := prepared.Work.run(runCtx)
		cancel()
		if runErr != nil {
			return watch.Cycle{}, runErr
		}
		if run == nil {
			return watch.Cycle{}, fmt.Errorf("background semantic review returned no run")
		}
		if run.ID != prepared.Work.runID {
			return watch.Cycle{}, fmt.Errorf("background semantic review run id %q does not match prepared run %q", run.ID, prepared.Work.runID)
		}
		if !reflect.DeepEqual(store.ToReviewTargetSchema(run.Target), prepared.Work.target) {
			return watch.Cycle{}, fmt.Errorf("background semantic review target does not match authorized target")
		}
		schema, err := store.ToReviewRunSchema(*run, store.RenderedOutcomeV1{})
		if err != nil {
			return watch.Cycle{}, fmt.Errorf("encode background semantic review run: %w", err)
		}
		if _, err := review.Runs.SaveRun(schema); err != nil {
			return watch.Cycle{}, fmt.Errorf("persist background semantic review run: %w", err)
		}
		economics := economicsFromRun(run)
		if err := review.Controller.RecordEconomics(review.Key, now().UTC(), economics); err != nil {
			return watch.Cycle{}, errors.Join(runErr, err)
		}
		return cycleFromRun(run)
	}}, nil
}

func economicsFromRun(run *domain.ReviewRun) store.ReviewEconomicsV1 {
	reviewerCalls := 0
	for _, result := range run.ReviewerResults {
		reviewerCalls += result.Attempts
	}
	modelCalls := run.Stats.ModelCallCount
	duration := run.CompletedAt.Sub(run.StartedAt)
	if duration < 0 {
		duration = 0
	}
	return store.ReviewEconomicsV1{
		SchemaVersion:     store.CurrentSchemaVersion,
		RunID:             run.ID,
		ReviewerCallCount: reviewerCalls,
		ModelCallCount:    modelCalls,
		Duration:          duration,
	}
}

type automaticTermination struct {
	kind   store.LoopDecisionKindV1
	reason string
}

func (e automaticTermination) Error() string {
	return fmt.Sprintf("%s: %s", ErrAutomaticReviewStopped, e.reason)
}

func (e automaticTermination) Unwrap() error {
	return ErrAutomaticReviewStopped
}

func (e automaticTermination) WatchExitReason() watch.ExitReason {
	if e.kind == store.LoopDecisionEscalate {
		return watch.ReasonEscalated
	}
	return watch.ReasonStopped
}

func cycleFromRun(run *domain.ReviewRun) (watch.Cycle, error) {
	cycle := watch.Cycle{HeadSHA: run.Target.Revision.HeadObjectID}
	switch run.Status {
	case domain.ReviewStatusFailed:
		if run.Failure != nil {
			return watch.Cycle{}, fmt.Errorf("%w: background semantic review failed: %s", watch.ErrRetryableCycle, run.Failure.Message)
		}
		return watch.Cycle{}, fmt.Errorf("%w: background semantic review failed", watch.ErrRetryableCycle)
	case domain.ReviewStatusInterrupted:
		return watch.Cycle{}, fmt.Errorf("%w: background semantic review was interrupted", watch.ErrRetryableCycle)
	case domain.ReviewStatusCompleted:
		switch run.Conclusion {
		case domain.ReviewConclusionNoChanges:
			cycle.Result = watch.CycleNoChanges
		case domain.ReviewConclusionFindings:
			cycle.Result = watch.CycleFindings
		case domain.ReviewConclusionClean:
			cycle.Result = watch.CycleClean
		default:
			return watch.Cycle{}, fmt.Errorf("background semantic review completed without a supported conclusion")
		}
		return cycle, nil
	default:
		return watch.Cycle{}, fmt.Errorf("background semantic review returned unknown status %q", run.Status)
	}
}

type ControlHistory interface {
	ListEvents(store.PullRequestKeyV1) ([]store.ReviewEventV1, []store.CorruptRecord, error)
}

func StoredControl(history ControlHistory, key store.PullRequestKeyV1) func(context.Context, watch.PRState) (watch.ControlDecision, error) {
	return func(ctx context.Context, _ watch.PRState) (watch.ControlDecision, error) {
		if err := ctx.Err(); err != nil {
			return watch.ControlDecision{}, err
		}
		if history == nil {
			return watch.ControlDecision{}, fmt.Errorf("lifecycle control history is required")
		}
		events, corrupt, err := history.ListEvents(key)
		if err != nil {
			return watch.ControlDecision{}, err
		}
		if len(corrupt) > 0 {
			return watch.ControlDecision{}, fmt.Errorf("lifecycle history contains %d corrupt record(s)", len(corrupt))
		}
		decision := watch.ControlDecision{State: watch.ControlActive}
		var decidedAt time.Time
		for _, event := range events {
			var state watch.ControlState
			switch event.Type {
			case store.EventTypeUserSnoozed:
				state = watch.ControlSnoozed
			case store.EventTypeUserReleased:
				state = watch.ControlReleased
			case store.EventTypeUserOptedOut:
				state = watch.ControlOptedOut
			case store.EventTypeUserResumed:
				state = watch.ControlActive
			default:
				continue
			}
			if decidedAt.IsZero() || !event.OccurredAt.Before(decidedAt) {
				decision.State = state
				decidedAt = event.OccurredAt
			}
		}
		return decision, nil
	}
}

func NewLifecycle(cfg watch.Config, polling watch.Polling, review BackgroundReview, control func(context.Context, watch.PRState) (watch.ControlDecision, error), presentation watch.Presentation) (*watch.Lifecycle, error) {
	port, err := review.Port()
	if err != nil {
		return nil, err
	}
	cfg.Mode = watch.PostModeComment
	return watch.NewLifecycle(
		cfg,
		polling,
		port,
		watch.ActionPolicies{Control: control},
		presentation,
	), nil
}
