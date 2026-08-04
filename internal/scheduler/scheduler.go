package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/automatic"
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
	RunID  string
	Target store.ReviewTargetV1
	Policy automatic.TrustedPolicy
	Run    func(context.Context) (watch.Cycle, *store.ReviewEconomicsV1, error)
}

type PrepareCycle func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error)

type BackgroundReview struct {
	Key        store.PullRequestKeyV1
	Controller Controller
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
		if prepared.RunID == "" {
			return watch.Cycle{}, fmt.Errorf("prepared automatic review run id is required")
		}
		if prepared.Run == nil {
			return watch.Cycle{}, fmt.Errorf("prepared automatic review runner is required")
		}
		decision, err := review.Controller.AuthorizeReview(review.Key, prepared.Target, prepared.Policy, prepared.RunID)
		if err != nil {
			return watch.Cycle{}, err
		}
		if !decision.Allowed {
			return watch.Cycle{}, fmt.Errorf("%w: %s", ErrAutomaticReviewStopped, decision.Reason)
		}
		runCtx := ctx
		cancel := func() {}
		if !decision.Deadline.IsZero() {
			runCtx, cancel = context.WithDeadline(ctx, decision.Deadline)
		}
		cycle, economics, runErr := prepared.Run(runCtx)
		cancel()
		if economics != nil {
			if economics.RunID != prepared.RunID {
				return watch.Cycle{}, fmt.Errorf("automatic review economics run id %q does not match prepared run %q", economics.RunID, prepared.RunID)
			}
			if err := review.Controller.RecordEconomics(review.Key, now().UTC(), *economics); err != nil {
				return watch.Cycle{}, errors.Join(runErr, err)
			}
		}
		if runErr != nil {
			return cycle, runErr
		}
		switch cycle.Result {
		case watch.CycleError, watch.CycleNoChanges, watch.CycleFindings, watch.CycleStaleHead, watch.CycleClean:
			return cycle, nil
		default:
			return watch.Cycle{}, fmt.Errorf("background review returned posting result %d", cycle.Result)
		}
	}}, nil
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
