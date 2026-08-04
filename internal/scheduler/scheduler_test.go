package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/automatic"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

type lifecycleFunc func(context.Context) watch.ExitReason

type inertAgent struct{}

func (inertAgent) Name() string {
	return "inert"
}

func (inertAgent) IsAvailable() error {
	return nil
}

func (inertAgent) ExecuteReview(context.Context, *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	return nil, fmt.Errorf("review execution was not expected")
}

func (inertAgent) ExecuteSummary(context.Context, *agent.SummaryConfig) (*agent.ExecutionResult, error) {
	return nil, fmt.Errorf("summary execution was not expected")
}

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

func schedulerTarget(key store.PullRequestKeyV1, head string) store.ReviewTargetV1 {
	return store.ReviewTargetV1{
		RepositoryRoot: "/repo",
		WorktreeRoot:   "/repo/worktree",
		Revision: store.RevisionEvidenceV1{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "origin/main",
			HeadObjectID:     head,
			BaseObjectID:     "base-1",
		},
		PullRequest: &key,
	}
}

func schedulerWork(target store.ReviewTargetV1, run func(context.Context) (*domain.ReviewRun, error)) BackgroundWork {
	return BackgroundWork{target: target, run: run}
}

func schedulerRun(id string, target store.ReviewTargetV1, conclusion domain.ReviewConclusion) *domain.ReviewRun {
	return &domain.ReviewRun{
		ID:          id,
		Target:      target.ToDomain(),
		StartedAt:   time.Unix(10, 0),
		CompletedAt: time.Unix(20, 0),
		Status:      domain.ReviewStatusCompleted,
		Conclusion:  conclusion,
	}
}

func TestBackgroundReviewAuthorizesBoundsAndRecordsRun(t *testing.T) {
	deadline := time.Now().Add(time.Minute).Round(0)
	controller := &controllerStub{decision: automatic.Decision{Allowed: true, Deadline: deadline}}
	key := schedulerKey(1)
	target := schedulerTarget(key, "head-1")
	port, err := (BackgroundReview{
		Key:        key,
		Controller: controller,
		Now:        func() time.Time { return time.Unix(100, 0) },
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  "run-1",
				Target: target,
				Work: schedulerWork(target, func(ctx context.Context) (*domain.ReviewRun, error) {
					gotDeadline, ok := ctx.Deadline()
					if !ok || !gotDeadline.Equal(deadline) {
						t.Fatalf("deadline = %v, %t, want %v", gotDeadline, ok, deadline)
					}
					return schedulerRun("run-1", target, domain.ReviewConclusionFindings), nil
				}),
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
	target := schedulerTarget(key, "head-1")
	var runs atomic.Int32
	review := BackgroundReview{
		Key:        key,
		Controller: controller,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			runID := fmt.Sprintf("run-%d", len(controller.authorized)+1)
			return PreparedCycle{
				RunID:  runID,
				Target: target,
				Work: schedulerWork(target, func(context.Context) (*domain.ReviewRun, error) {
					runs.Add(1)
					return schedulerRun(runID, target, domain.ReviewConclusionFindings), nil
				}),
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

func TestBackgroundReviewDurablyDeduplicatesRevisionAcrossRecreation(t *testing.T) {
	dataDir := t.TempDir()
	key := schedulerKey(1)
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 3},
	}
	newPolicy := func(target store.ReviewTargetV1) automatic.TrustedPolicy {
		policy, err := automatic.NewTrustedPolicy(policyRecord, target)
		if err != nil {
			t.Fatalf("NewTrustedPolicy: %v", err)
		}
		return policy
	}
	newController := func() *automatic.Controller {
		controller, err := automatic.NewController(
			store.NewFilesystemLoopDecisionStore(dataDir),
			store.NewFilesystemEconomicsStore(dataDir),
		)
		if err != nil {
			t.Fatalf("NewController: %v", err)
		}
		return controller
	}
	authorization, err := automatic.WorkspaceAuthorization("scheduler-test")
	if err != nil {
		t.Fatalf("WorkspaceAuthorization: %v", err)
	}
	targetA := schedulerTarget(key, "head-a")
	controller := newController()
	if _, err := controller.Commission(key, targetA, newPolicy(targetA), authorization); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	var runs atomic.Int32
	prepare := func(runID string, target store.ReviewTargetV1, policy automatic.TrustedPolicy) PrepareCycle {
		return func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  runID,
				Target: target,
				Policy: policy,
				Work: schedulerWork(target, func(context.Context) (*domain.ReviewRun, error) {
					runs.Add(1)
					return schedulerRun(runID, target, domain.ReviewConclusionFindings), nil
				}),
			}, nil
		}
	}
	first, err := (BackgroundReview{Key: key, Controller: controller, Prepare: prepare("run-a-1", targetA, newPolicy(targetA))}).Port()
	if err != nil {
		t.Fatalf("first Port: %v", err)
	}
	if _, err := first.RunCycle(context.Background(), 1, "initial review", nil, ""); err != nil {
		t.Fatalf("first RunCycle: %v", err)
	}

	restarted := newController()
	duplicate, err := (BackgroundReview{Key: key, Controller: restarted, Prepare: prepare("run-a-2", targetA, newPolicy(targetA))}).Port()
	if err != nil {
		t.Fatalf("duplicate Port: %v", err)
	}
	if _, err := duplicate.RunCycle(context.Background(), 1, "initial review", nil, ""); !errors.Is(err, automatic.ErrRevisionAlreadyAuthorized) {
		t.Fatalf("duplicate RunCycle error = %v, want durable revision rejection", err)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs after duplicate = %d, want 1", runs.Load())
	}

	targetB := schedulerTarget(key, "head-b")
	replacement, err := (BackgroundReview{Key: key, Controller: restarted, Prepare: prepare("run-b-1", targetB, newPolicy(targetB))}).Port()
	if err != nil {
		t.Fatalf("replacement Port: %v", err)
	}
	if _, err := replacement.RunCycle(context.Background(), 1, "commits settled", nil, ""); err != nil {
		t.Fatalf("replacement RunCycle: %v", err)
	}
	if runs.Load() != 2 {
		t.Fatalf("runs after replacement = %d, want 2", runs.Load())
	}
}

func TestBackgroundWorkExposesNoSubmissionCapability(t *testing.T) {
	workType := reflect.TypeOf(BackgroundWork{})
	for i := 0; i < workType.NumField(); i++ {
		if workType.Field(i).IsExported() {
			t.Fatalf("background work exposes configurable field %q", workType.Field(i).Name)
		}
	}
	if workType.NumMethod() != 0 {
		t.Fatalf("background work exposes %d public method(s)", workType.NumMethod())
	}
	preparedType := reflect.TypeOf(PreparedCycle{})
	for i := 0; i < preparedType.NumField(); i++ {
		if preparedType.Field(i).Type.Kind() == reflect.Func {
			t.Fatalf("prepared background cycle accepts executable function %q", preparedType.Field(i).Name)
		}
	}

	repositoryRoot := t.TempDir()
	if output, err := exec.Command("git", "init", repositoryRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	key := schedulerKey(1)
	target := schedulerTarget(key, "head-1")
	target.RepositoryRoot = repositoryRoot
	target.WorktreeRoot = repositoryRoot
	configuration, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         1,
		Concurrency:       1,
		Timeout:           time.Minute,
		ReviewerAgents:    []string{"inert"},
		SummarizerAgent:   "inert",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		FPThreshold:       75,
	})
	if err != nil {
		t.Fatalf("NewReviewConfiguration: %v", err)
	}
	service, err := reviewpkg.NewService(
		reviewpkg.WithRunIDGenerator(func(time.Time) (string, error) { return "run-clean", nil }),
		reviewpkg.WithAgentFactory(func(string, string) (agent.Agent, error) { return inertAgent{}, nil }),
		reviewpkg.WithRevisionProvider(func(context.Context, domain.ReviewTarget) (domain.RevisionEvidence, error) {
			return target.Revision.ToDomain(), nil
		}),
		reviewpkg.WithDiffProvider(func(context.Context, domain.ReviewTarget) (string, error) { return "", nil }),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	var submissionCalls atomic.Int32
	work, err := NewBackgroundWork(service, reviewpkg.Request{
		Target:        target.ToDomain(),
		Trigger:       domain.ReviewTriggerDesk,
		Engine:        domain.ReviewEngine{Name: "acr", Version: "test"},
		Configuration: configuration,
		ConfigurationSource: domain.ConfigurationSourceIdentity{
			Kind: "test",
		},
		Events: reviewpkg.EventSinkFunc(func(reviewpkg.Event) {
			submissionCalls.Add(1)
		}),
	})
	if err != nil {
		t.Fatalf("NewBackgroundWork: %v", err)
	}
	controller := &controllerStub{decision: automatic.Decision{Allowed: true}}
	port, err := (BackgroundReview{
		Key:        key,
		Controller: controller,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				RunID:  "run-clean",
				Target: target,
				Work:   work,
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
	if cycle.Result != watch.CycleNoChanges {
		t.Fatalf("cycle result = %d, want no changes", cycle.Result)
	}
	if submissionCalls.Load() != 0 {
		t.Fatalf("background work invoked supplied submission callback %d time(s)", submissionCalls.Load())
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
			target := schedulerTarget(key, head)
			runID := "run-" + head
			return PreparedCycle{
				RunID:  runID,
				Target: target,
				Work: schedulerWork(target, func(context.Context) (*domain.ReviewRun, error) {
					conclusion := domain.ReviewConclusionFindings
					if head == "head-b" {
						conclusion = domain.ReviewConclusionClean
					}
					return schedulerRun(runID, target, conclusion), nil
				}),
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
