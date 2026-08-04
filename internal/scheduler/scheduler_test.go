package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/automatic"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

type lifecycleFunc func(context.Context) watch.ExitReason

func (f lifecycleFunc) Run(ctx context.Context) watch.ExitReason {
	return f(ctx)
}

func schedulerKey(number int) store.PullRequestKeyV1 {
	return store.PullRequestKeyV1{Host: "github.com", Owner: "acme", Repository: "widgets", Number: number}
}

func TestSchedulerBoundsConcurrentLifecycles(t *testing.T) {
	scheduler, err := New(2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	release := make(chan struct{})
	started := make(chan struct{}, 6)
	var active atomic.Int32
	var maximum atomic.Int32
	jobs := make([]Job, 6)
	for i := range jobs {
		jobs[i] = Job{Key: schedulerKey(i + 1), Lifecycle: lifecycleFunc(func(ctx context.Context) watch.ExitReason {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			return watch.ReasonClosed
		})}
	}

	done := make(chan error, 1)
	go func() {
		results, runErr := scheduler.Run(context.Background(), jobs)
		if runErr == nil && len(results) != len(jobs) {
			runErr = fmt.Errorf("results = %d, want %d", len(results), len(jobs))
		}
		done <- runErr
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not fill configured capacity")
		}
	}
	select {
	case <-started:
		t.Fatal("scheduler exceeded configured capacity")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", got)
	}
}

func TestSchedulerRejectsDuplicatePullRequestLifecycle(t *testing.T) {
	scheduler, err := New(2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var calls atomic.Int32
	lifecycle := lifecycleFunc(func(context.Context) watch.ExitReason {
		calls.Add(1)
		return watch.ReasonClosed
	})
	key := schedulerKey(1)
	_, err = scheduler.Run(context.Background(), []Job{{Key: key, Lifecycle: lifecycle}, {Key: key, Lifecycle: lifecycle}})
	if err == nil {
		t.Fatal("expected duplicate lifecycle rejection")
	}
	if calls.Load() != 0 {
		t.Fatalf("lifecycle calls = %d, want 0", calls.Load())
	}
}

func TestSchedulerRejectsDuplicateActivePullRequestAcrossCallers(t *testing.T) {
	scheduler, err := New(2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	key := schedulerKey(1)
	firstDone := make(chan error, 1)
	go func() {
		_, runErr := scheduler.Run(context.Background(), []Job{{Key: key, Lifecycle: lifecycleFunc(func(context.Context) watch.ExitReason {
			close(started)
			<-release
			return watch.ReasonClosed
		})}})
		firstDone <- runErr
	}()
	<-started
	var duplicateCalls atomic.Int32
	_, err = scheduler.Run(context.Background(), []Job{{Key: key, Lifecycle: lifecycleFunc(func(context.Context) watch.ExitReason {
		duplicateCalls.Add(1)
		return watch.ReasonClosed
	})}})
	if err == nil {
		t.Fatal("expected active duplicate rejection")
	}
	if duplicateCalls.Load() != 0 {
		t.Fatalf("duplicate calls = %d, want 0", duplicateCalls.Load())
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Run: %v", err)
	}
}

type controllerStub struct {
	mu         sync.Mutex
	decision   automatic.Decision
	authorized []string
	recorded   []store.ReviewEconomicsV1
}

func (c *controllerStub) AuthorizeReview(_ store.PullRequestKeyV1, _ store.ReviewTargetV1, _ automatic.TrustedPolicy, runID string) (automatic.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authorized = append(c.authorized, runID)
	return c.decision, nil
}

func (c *controllerStub) RecordEconomics(_ store.PullRequestKeyV1, _ time.Time, economics store.ReviewEconomicsV1) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recorded = append(c.recorded, economics)
	return nil
}

func TestBackgroundReviewAuthorizesBoundsAndRecordsRun(t *testing.T) {
	deadline := time.Now().Add(time.Minute).Round(0)
	controller := &controllerStub{decision: automatic.Decision{Allowed: true, Deadline: deadline}}
	key := schedulerKey(1)
	port, err := (BackgroundReview{
		Key:        key,
		Controller: controller,
		Now:        func() time.Time { return time.Unix(100, 0) },
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  "run-1",
				Target: store.ReviewTargetV1{PullRequest: &key},
				Run: func(ctx context.Context) (watch.Cycle, *store.ReviewEconomicsV1, error) {
					gotDeadline, ok := ctx.Deadline()
					if !ok || !gotDeadline.Equal(deadline) {
						t.Fatalf("deadline = %v, %t, want %v", gotDeadline, ok, deadline)
					}
					economics := &store.ReviewEconomicsV1{SchemaVersion: store.CurrentSchemaVersion, RunID: "run-1"}
					return watch.Cycle{Result: watch.CycleFindings, HeadSHA: "head-1"}, economics, nil
				},
			}, nil
		},
	}).Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	cycle, err := port.RunCycle(context.Background(), 1, "initial review", nil, "")
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if cycle.Result != watch.CycleFindings {
		t.Fatalf("cycle result = %d, want findings", cycle.Result)
	}
	if len(controller.authorized) != 1 || controller.authorized[0] != "run-1" {
		t.Fatalf("authorized = %v", controller.authorized)
	}
	if len(controller.recorded) != 1 || controller.recorded[0].RunID != "run-1" {
		t.Fatalf("recorded = %v", controller.recorded)
	}
}

func TestBackgroundReviewHonorsStopAndResumeDecision(t *testing.T) {
	controller := &controllerStub{decision: automatic.Decision{Reason: "review budget exhausted"}}
	key := schedulerKey(1)
	var runs atomic.Int32
	review := BackgroundReview{
		Key:        key,
		Controller: controller,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  fmt.Sprintf("run-%d", len(controller.authorized)+1),
				Target: store.ReviewTargetV1{PullRequest: &key},
				Run: func(context.Context) (watch.Cycle, *store.ReviewEconomicsV1, error) {
					runs.Add(1)
					return watch.Cycle{Result: watch.CycleFindings}, nil, nil
				},
			}, nil
		},
	}
	port, err := review.Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	if _, err := port.RunCycle(context.Background(), 1, "initial review", nil, ""); !errors.Is(err, ErrAutomaticReviewStopped) {
		t.Fatalf("RunCycle error = %v, want stopped", err)
	}
	if runs.Load() != 0 {
		t.Fatalf("runs = %d, want 0", runs.Load())
	}
	controller.decision = automatic.Decision{Allowed: true}
	if _, err := port.RunCycle(context.Background(), 1, "initial review", nil, ""); err != nil {
		t.Fatalf("resumed RunCycle: %v", err)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d, want 1", runs.Load())
	}
}

func TestBackgroundReviewRejectsPostingResults(t *testing.T) {
	controller := &controllerStub{decision: automatic.Decision{Allowed: true}}
	key := schedulerKey(1)
	port, err := (BackgroundReview{
		Key:        key,
		Controller: controller,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  "run-post",
				Target: store.ReviewTargetV1{PullRequest: &key},
				Run: func(context.Context) (watch.Cycle, *store.ReviewEconomicsV1, error) {
					return watch.Cycle{Result: watch.CycleLGTMComment}, nil, nil
				},
			}, nil
		},
	}).Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	if _, err := port.RunCycle(context.Background(), 1, "initial review", nil, ""); err == nil {
		t.Fatal("expected posting result rejection")
	}
}

type eventHistory struct {
	events []store.ReviewEventV1
}

func (h eventHistory) ListEvents(store.PullRequestKeyV1) ([]store.ReviewEventV1, []store.CorruptRecord, error) {
	return h.events, nil, nil
}

func TestStoredControlUsesLatestRecordedDecision(t *testing.T) {
	base := time.Unix(100, 0)
	tests := []struct {
		name   string
		types  []store.ReviewEventTypeV1
		wanted watch.ControlState
	}{
		{name: "active by default", wanted: watch.ControlActive},
		{name: "snoozed", types: []store.ReviewEventTypeV1{store.EventTypeUserSnoozed}, wanted: watch.ControlSnoozed},
		{name: "released", types: []store.ReviewEventTypeV1{store.EventTypeUserReleased}, wanted: watch.ControlReleased},
		{name: "opted out", types: []store.ReviewEventTypeV1{store.EventTypeUserOptedOut}, wanted: watch.ControlOptedOut},
		{name: "resumed", types: []store.ReviewEventTypeV1{store.EventTypeUserSnoozed, store.EventTypeUserResumed}, wanted: watch.ControlActive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := make([]store.ReviewEventV1, len(test.types))
			for i, eventType := range test.types {
				events[i] = store.ReviewEventV1{Type: eventType, OccurredAt: base.Add(time.Duration(i) * time.Second)}
			}
			decision, err := StoredControl(eventHistory{events: events}, schedulerKey(1))(context.Background(), watch.PRState{})
			if err != nil {
				t.Fatalf("control: %v", err)
			}
			if decision.State != test.wanted {
				t.Fatalf("state = %q, want %q", decision.State, test.wanted)
			}
		})
	}
}

type advancingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *advancingClock) Sleep(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
	return nil
}

func TestLifecycleReviewsSettledReplacementHeadOnce(t *testing.T) {
	key := schedulerKey(1)
	controller := &controllerStub{decision: automatic.Decision{Allowed: true}}
	clock := &advancingClock{now: time.Now()}
	states := []watch.PRState{{HeadSHA: "head-a"}, {HeadSHA: "head-b"}, {HeadSHA: "head-b"}}
	var stateIndex int
	var mu sync.Mutex
	polling := watch.Polling{
		Clock: clock,
		State: func(context.Context) (watch.PRState, error) {
			mu.Lock()
			defer mu.Unlock()
			if stateIndex < len(states)-1 {
				stateIndex++
				return states[stateIndex-1], nil
			}
			return states[len(states)-1], nil
		},
	}
	var heads []string
	review := BackgroundReview{
		Key:        key,
		Controller: controller,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			mu.Lock()
			head := states[stateIndex-1].HeadSHA
			heads = append(heads, head)
			mu.Unlock()
			return PreparedCycle{
				RunID:  "run-" + head,
				Target: store.ReviewTargetV1{PullRequest: &key},
				Run: func(context.Context) (watch.Cycle, *store.ReviewEconomicsV1, error) {
					result := watch.CycleFindings
					if head == "head-b" {
						result = watch.CycleClean
					}
					return watch.Cycle{Result: result, HeadSHA: head}, nil, nil
				},
			}, nil
		},
	}
	lifecycle, err := NewLifecycle(watch.Config{
		PollInterval:    time.Minute,
		SettleTime:      time.Minute,
		MaxReviews:      2,
		MaxDuration:     time.Hour,
		UncertainPolicy: watch.UncertainWait,
	}, polling, review, nil, watch.Presentation{})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	if reason := lifecycle.Run(context.Background()); reason != watch.ReasonMaxReviews {
		t.Fatalf("reason = %v, want max reviews", reason)
	}
	if fmt.Sprint(heads) != "[head-a head-b]" {
		t.Fatalf("reviewed heads = %v, want [head-a head-b]", heads)
	}
}
