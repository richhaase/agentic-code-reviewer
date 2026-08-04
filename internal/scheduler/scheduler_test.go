package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
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
	mu           sync.Mutex
	decision     automatic.Decision
	authorized   []string
	recorded     []store.ReviewEconomicsV1
	economicsErr error
	order        *[]string
}

func (c *controllerStub) AuthorizeReview(_ store.PullRequestKeyV1, _ store.ReviewTargetV1, _ automatic.TrustedPolicy, runID string, _ ...string) (automatic.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authorized = append(c.authorized, runID)
	return c.decision, nil
}

func (c *controllerStub) RecordEconomics(_ store.PullRequestKeyV1, _ time.Time, economics store.ReviewEconomicsV1) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.order != nil {
		*c.order = append(*c.order, "economics")
	}
	c.recorded = append(c.recorded, economics)
	return c.economicsErr
}

type runStoreStub struct {
	mu      sync.Mutex
	runs    []store.ReviewRunV1
	saveErr error
	order   *[]string
}

func (s *runStoreStub) SaveRun(run store.ReviewRunV1) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.order != nil {
		*s.order = append(*s.order, "run")
	}
	if s.saveErr != nil {
		return "", s.saveErr
	}
	s.runs = append(s.runs, run)
	return run.ID, nil
}

func (s *runStoreStub) ListRuns(store.PullRequestKeyV1) ([]store.ReviewRunV1, []store.CorruptRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.ReviewRunV1(nil), s.runs...), nil, nil
}

func (s *runStoreStub) LoadRun(_ store.PullRequestKeyV1, runID string) (store.ReviewRunV1, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.ID == runID {
			return run, nil
		}
	}
	return store.ReviewRunV1{}, fmt.Errorf("run %s not found", runID)
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

func schedulerWork(runID string, target store.ReviewTargetV1, run func(context.Context) (*domain.ReviewRun, error)) BackgroundWork {
	return BackgroundWork{runID: runID, target: target, run: run}
}

func schedulerRun(t *testing.T, id string, target store.ReviewTargetV1, conclusion domain.ReviewConclusion) *domain.ReviewRun {
	t.Helper()
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
	return &domain.ReviewRun{
		ID:                       id,
		Target:                   target.ToDomain(),
		Trigger:                  domain.ReviewTriggerDesk,
		Engine:                   domain.ReviewEngine{Name: "acr", Version: "test"},
		StartedAt:                time.Unix(10, 0),
		CompletedAt:              time.Unix(20, 0),
		Configuration:            configuration,
		ConfigurationSource:      domain.ConfigurationSourceIdentity{Kind: "test"},
		ConfigurationFingerprint: configuration.Fingerprint(),
		Status:                   domain.ReviewStatusCompleted,
		Conclusion:               conclusion,
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
		Runs:       store.NewFilesystemRunStore(t.TempDir()),
		Now:        func() time.Time { return time.Unix(100, 0) },
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{
				Work: schedulerWork("run-1", target, func(ctx context.Context) (*domain.ReviewRun, error) {
					gotDeadline, ok := ctx.Deadline()
					if !ok || !gotDeadline.Equal(deadline) {
						t.Fatalf("deadline = %v, %t, want %v", gotDeadline, ok, deadline)
					}
					return schedulerRun(t, "run-1", target, domain.ReviewConclusionFindings), nil
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

func TestBackgroundReviewRetriesPreparationFailures(t *testing.T) {
	transient := errors.New("revision lookup unavailable")
	key := schedulerKey(1)
	target := schedulerTarget(key, "head-1")
	newReview := func(prepare func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error)) BackgroundReview {
		return BackgroundReview{
			Key:        key,
			Controller: &controllerStub{decision: automatic.Decision{Allowed: true}},
			Runs:       store.NewFilesystemRunStore(t.TempDir()),
			Prepare:    prepare,
		}
	}
	t.Run("preserves original error", func(t *testing.T) {
		port, err := newReview(func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return PreparedCycle{}, transient
		}).Port()
		if err != nil {
			t.Fatal(err)
		}
		_, err = port.RunCycle(context.Background(), 1, "initial review", nil, "")
		if !errors.Is(err, watch.ErrRetryableCycle) || !errors.Is(err, transient) {
			t.Fatalf("RunCycle error = %v, want retryable wrapper preserving original", err)
		}
	})
	t.Run("succeeds after retry", func(t *testing.T) {
		attempts := 0
		lifecycle, err := NewLifecycle(watch.Config{
			PollInterval: time.Minute,
			SettleTime:   time.Minute,
			MaxReviews:   2,
			MaxDuration:  time.Hour,
		}, watch.Polling{
			Clock: &advancingClock{now: time.Now()},
			State: func(context.Context) (watch.PRState, error) {
				if attempts >= 2 {
					return watch.PRState{HeadSHA: "head-1", Closed: true}, nil
				}
				return watch.PRState{HeadSHA: "head-1"}, nil
			},
		}, newReview(func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			attempts++
			if attempts == 1 {
				return PreparedCycle{}, transient
			}
			return PreparedCycle{Work: schedulerWork("prepare-retry", target, func(context.Context) (*domain.ReviewRun, error) {
				return schedulerRun(t, "prepare-retry", target, domain.ReviewConclusionClean), nil
			})}, nil
		}), nil, watch.Presentation{})
		if err != nil {
			t.Fatal(err)
		}
		if reason := lifecycle.Run(context.Background()); reason != watch.ReasonClosed {
			t.Fatalf("reason = %v, want closed after successful retry", reason)
		}
		if attempts != 2 {
			t.Fatalf("attempts = %d, want 2", attempts)
		}
	})
	t.Run("stops after bounded failures", func(t *testing.T) {
		attempts := 0
		lifecycle, err := NewLifecycle(watch.Config{
			PollInterval: time.Minute,
			SettleTime:   time.Minute,
			MaxReviews:   10,
			MaxDuration:  time.Hour,
		}, watch.Polling{
			Clock: &advancingClock{now: time.Now()},
			State: func(context.Context) (watch.PRState, error) {
				return watch.PRState{HeadSHA: "head-1"}, nil
			},
		}, newReview(func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			attempts++
			return PreparedCycle{}, transient
		}), nil, watch.Presentation{})
		if err != nil {
			t.Fatal(err)
		}
		if reason := lifecycle.Run(context.Background()); reason != watch.ReasonError {
			t.Fatalf("reason = %v, want error", reason)
		}
		if attempts != 5 {
			t.Fatalf("attempts = %d, want bounded retry count 5", attempts)
		}
	})
}

func TestBackgroundReviewPersistsEconomicsBeforeTerminalRun(t *testing.T) {
	economicsFailure := errors.New("economics unavailable")
	runFailure := errors.New("run store unavailable")
	tests := []struct {
		name         string
		economicsErr error
		runErr       error
		wantOrder    string
		wantRuns     int
		wantErr      error
	}{
		{name: "economics failure leaves no completed marker", economicsErr: economicsFailure, wantOrder: "economics", wantErr: economicsFailure},
		{name: "run failure retains economics", runErr: runFailure, wantOrder: "economics,run", wantErr: runFailure},
		{name: "success records economics then run", wantOrder: "economics,run", wantRuns: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := schedulerKey(1)
			target := schedulerTarget(key, "head-1")
			var order []string
			controller := &controllerStub{
				decision:     automatic.Decision{Allowed: true},
				economicsErr: test.economicsErr,
				order:        &order,
			}
			runs := &runStoreStub{saveErr: test.runErr, order: &order}
			port, err := (BackgroundReview{
				Key:        key,
				Controller: controller,
				Runs:       runs,
				Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
					return PreparedCycle{Work: schedulerWork("ordered-run", target, func(context.Context) (*domain.ReviewRun, error) {
						return schedulerRun(t, "ordered-run", target, domain.ReviewConclusionClean), nil
					})}, nil
				},
			}).Port()
			if err != nil {
				t.Fatal(err)
			}
			_, cycleErr := port.RunCycle(context.Background(), 1, "initial review", nil, "")
			if test.wantErr == nil && cycleErr != nil {
				t.Fatalf("RunCycle: %v", cycleErr)
			}
			if test.wantErr != nil && !errors.Is(cycleErr, test.wantErr) {
				t.Fatalf("RunCycle error = %v, want %v", cycleErr, test.wantErr)
			}
			persisted, corrupt, listErr := runs.ListRuns(key)
			if listErr != nil || len(corrupt) != 0 || len(persisted) != test.wantRuns {
				t.Fatalf("runs = %+v, corrupt = %+v, err = %v", persisted, corrupt, listErr)
			}
			if strings.Join(order, ",") != test.wantOrder {
				t.Fatalf("persistence order = %v, want %q", order, test.wantOrder)
			}
			if len(controller.recorded) != 1 || controller.recorded[0].RunID != "ordered-run" {
				t.Fatalf("recorded economics = %+v", controller.recorded)
			}
		})
	}
}

func TestBackgroundReviewPersistsFailedAndInterruptedRuns(t *testing.T) {
	tests := []domain.ReviewStatus{domain.ReviewStatusFailed, domain.ReviewStatusInterrupted}
	for _, status := range tests {
		t.Run(string(status), func(t *testing.T) {
			key := schedulerKey(1)
			target := schedulerTarget(key, "head-1")
			runID := "run-" + string(status)
			runStore := store.NewFilesystemRunStore(t.TempDir())
			controller := &controllerStub{decision: automatic.Decision{Allowed: true}}
			port, err := (BackgroundReview{
				Key:        key,
				Controller: controller,
				Runs:       runStore,
				Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
					return PreparedCycle{Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
						run := schedulerRun(t, runID, target, domain.ReviewConclusionClean)
						run.Status = status
						run.Conclusion = domain.ReviewConclusionNone
						run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: string(status)}
						return run, nil
					})}, nil
				},
			}).Port()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := port.RunCycle(context.Background(), 1, "initial review", nil, ""); !errors.Is(err, watch.ErrRetryableCycle) {
				t.Fatalf("%s cycle error = %v, want retryable", status, err)
			}
			runs, corrupt, err := runStore.ListRuns(key)
			if err != nil || len(corrupt) != 0 || len(runs) != 1 || runs[0].Status != string(status) {
				t.Fatalf("persisted runs = %+v, corrupt = %+v, err = %v", runs, corrupt, err)
			}
			if len(controller.recorded) != 1 || controller.recorded[0].RunID != runID {
				t.Fatalf("recorded economics = %+v", controller.recorded)
			}
		})
	}
}

func TestEconomicsUsesMeasuredModelCalls(t *testing.T) {
	run := &domain.ReviewRun{
		ID:          "run-measured",
		StartedAt:   time.Unix(10, 0),
		CompletedAt: time.Unix(20, 0),
		ReviewerResults: []domain.ReviewerResult{
			{Attempts: 2},
			{Attempts: 1},
		},
		Stats: domain.ReviewStats{
			SummarizerDuration: time.Second,
			FPFilterDuration:   time.Second,
			ModelCallCount:     6,
		},
	}
	economics := economicsFromRun(run)
	if economics.ReviewerCallCount != 3 || economics.ModelCallCount != 6 {
		t.Fatalf("economics = %+v, want reviewer=3 model=6", economics)
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
		Runs:       store.NewFilesystemRunStore(t.TempDir()),
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			runID := fmt.Sprintf("run-%d", len(controller.authorized)+1)
			return PreparedCycle{
				Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
					runs.Add(1)
					return schedulerRun(t, runID, target, domain.ReviewConclusionFindings), nil
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
			store.NewFilesystemRunStore(dataDir),
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
				Policy: policy,
				Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
					runs.Add(1)
					return schedulerRun(t, runID, target, domain.ReviewConclusionFindings), nil
				}),
			}, nil
		}
	}
	first, err := (BackgroundReview{Key: key, Controller: controller, Runs: store.NewFilesystemRunStore(dataDir), Prepare: prepare("run-a-1", targetA, newPolicy(targetA))}).Port()
	if err != nil {
		t.Fatalf("first Port: %v", err)
	}
	if _, err := first.RunCycle(context.Background(), 1, "initial review", nil, ""); err != nil {
		t.Fatalf("first RunCycle: %v", err)
	}

	restarted := newController()
	duplicate, err := (BackgroundReview{Key: key, Controller: restarted, Runs: store.NewFilesystemRunStore(dataDir), Prepare: prepare("run-a-2", targetA, newPolicy(targetA))}).Port()
	if err != nil {
		t.Fatalf("duplicate Port: %v", err)
	}
	duplicateCycle, err := duplicate.RunCycle(context.Background(), 1, "initial review", nil, "")
	if err != nil || duplicateCycle.Result != watch.CycleAlreadyReviewed {
		t.Fatalf("duplicate cycle = %+v, err = %v, want already reviewed", duplicateCycle, err)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs after duplicate = %d, want 1", runs.Load())
	}

	targetB := schedulerTarget(key, "head-b")
	replacement, err := (BackgroundReview{Key: key, Controller: restarted, Runs: store.NewFilesystemRunStore(dataDir), Prepare: prepare("run-b-1", targetB, newPolicy(targetB))}).Port()
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

func TestLifecycleSkipsCompletedRevisionAfterRestartAndReviewsReplacement(t *testing.T) {
	dataDir := t.TempDir()
	key := schedulerKey(1)
	targetA := schedulerTarget(key, "head-a")
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 3},
	}
	newPolicy := func(target store.ReviewTargetV1) automatic.TrustedPolicy {
		policy, err := automatic.NewTrustedPolicy(policyRecord, target)
		if err != nil {
			t.Fatal(err)
		}
		return policy
	}
	newController := func() *automatic.Controller {
		controller, err := automatic.NewController(
			store.NewFilesystemLoopDecisionStore(dataDir),
			store.NewFilesystemEconomicsStore(dataDir),
			store.NewFilesystemRunStore(dataDir),
		)
		if err != nil {
			t.Fatal(err)
		}
		return controller
	}
	authorization, err := automatic.WorkspaceAuthorization("restart-test")
	if err != nil {
		t.Fatal(err)
	}
	controller := newController()
	if _, err := controller.Commission(key, targetA, newPolicy(targetA), authorization); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, targetA, newPolicy(targetA), "completed-head-a"); err != nil {
		t.Fatal(err)
	}
	completed := schedulerRun(t, "completed-head-a", targetA, domain.ReviewConclusionClean)
	schema, err := store.ToReviewRunSchema(*completed, store.RenderedOutcomeV1{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewFilesystemRunStore(dataDir).SaveRun(schema); err != nil {
		t.Fatal(err)
	}

	clock := &advancingClock{now: time.Now()}
	states := []watch.PRState{{HeadSHA: "head-a"}, {HeadSHA: "head-b"}, {HeadSHA: "head-b"}}
	var stateIndex int
	var mu sync.Mutex
	var preparedHeads []string
	var executedHeads []string
	lifecycle, err := NewLifecycle(watch.Config{
		PollInterval:    time.Minute,
		SettleTime:      time.Minute,
		MaxReviews:      1,
		MaxDuration:     time.Hour,
		UncertainPolicy: watch.UncertainWait,
	}, watch.Polling{
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
	}, BackgroundReview{
		Key:        key,
		Controller: newController(),
		Runs:       store.NewFilesystemRunStore(dataDir),
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			mu.Lock()
			head := states[stateIndex-1].HeadSHA
			preparedHeads = append(preparedHeads, head)
			mu.Unlock()
			target := schedulerTarget(key, head)
			runID := "restart-" + head
			return PreparedCycle{
				Policy: newPolicy(target),
				Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
					executedHeads = append(executedHeads, head)
					return schedulerRun(t, runID, target, domain.ReviewConclusionClean), nil
				}),
			}, nil
		},
	}, nil, watch.Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	if reason := lifecycle.Run(context.Background()); reason != watch.ReasonMaxReviews {
		t.Fatalf("reason = %v, want max reviews", reason)
	}
	if fmt.Sprint(preparedHeads) != "[head-a head-b]" || fmt.Sprint(executedHeads) != "[head-b]" {
		t.Fatalf("prepared = %v, executed = %v", preparedHeads, executedHeads)
	}
}

func TestBackgroundReviewDeduplicatesTargetByWatcherEvidence(t *testing.T) {
	dataDir := t.TempDir()
	key := schedulerKey(1)
	target := schedulerTarget(key, "head-1")
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 10},
	}
	policy, err := automatic.NewTrustedPolicy(policyRecord, target)
	if err != nil {
		t.Fatal(err)
	}
	newController := func() *automatic.Controller {
		controller, err := automatic.NewController(
			store.NewFilesystemLoopDecisionStore(dataDir),
			store.NewFilesystemEconomicsStore(dataDir),
			store.NewFilesystemRunStore(dataDir),
		)
		if err != nil {
			t.Fatal(err)
		}
		return controller
	}
	controller := newController()
	authorization, err := automatic.WorkspaceAuthorization("evidence-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, authorization); err != nil {
		t.Fatal(err)
	}
	var prepared int
	var executed int
	newPort := func(controller *automatic.Controller) watch.ReviewExecution {
		port, err := (BackgroundReview{
			Key:        key,
			Controller: controller,
			Runs:       store.NewFilesystemRunStore(dataDir),
			Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
				prepared++
				runID := fmt.Sprintf("evidence-run-%d", prepared)
				return PreparedCycle{
					Policy: policy,
					Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
						executed++
						return schedulerRun(t, runID, target, domain.ReviewConclusionClean), nil
					}),
				}, nil
			},
		}).Port()
		if err != nil {
			t.Fatal(err)
		}
		return port
	}
	run := func(port watch.ReviewExecution, trigger, discussionRevision string) watch.Cycle {
		cycle, err := port.RunCycle(context.Background(), 1, trigger, nil, discussionRevision)
		if err != nil {
			t.Fatalf("RunCycle(%q): %v", trigger, err)
		}
		return cycle
	}
	port := newPort(controller)
	if cycle := run(port, "initial review", ""); cycle.Result != watch.CycleClean {
		t.Fatalf("initial cycle = %+v", cycle)
	}
	if cycle := run(port, "manual request", ""); cycle.Result != watch.CycleClean {
		t.Fatalf("manual cycle = %+v", cycle)
	}
	if cycle := run(port, "re-review requested", ""); cycle.Result != watch.CycleClean {
		t.Fatalf("re-review cycle = %+v", cycle)
	}
	discussionA := "issue_comment:10:revision-a\n"
	if cycle := run(port, "discussion requires reconsideration", discussionA); cycle.Result != watch.CycleClean {
		t.Fatalf("discussion cycle = %+v", cycle)
	}

	port = newPort(newController())
	if cycle := run(port, "discussion requires reconsideration", discussionA); cycle.Result != watch.CycleAlreadyReviewed {
		t.Fatalf("repeated discussion cycle = %+v", cycle)
	}
	discussionB := discussionA + "review_comment:11:revision-b\n"
	if cycle := run(port, "discussion requires reconsideration", discussionB); cycle.Result != watch.CycleClean {
		t.Fatalf("new discussion cycle = %+v", cycle)
	}
	if cycle := run(port, "commits settled", ""); cycle.Result != watch.CycleAlreadyReviewed {
		t.Fatalf("ordinary duplicate cycle = %+v", cycle)
	}
	if executed != 5 {
		t.Fatalf("executed = %d, want five distinct evidence reviews", executed)
	}
	decisions, corrupt, err := store.NewFilesystemLoopDecisionStore(dataDir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("decisions = %+v, corrupt = %+v, err = %v", decisions, corrupt, err)
	}
	var identities []string
	for _, decision := range decisions {
		if decision.Decision == store.LoopDecisionContinue {
			identities = append(identities, decision.EvidenceIdentity)
		}
	}
	if len(identities) != 5 || identities[0] != "automatic-revision" || identities[1] != "explicit:evidence-run-2" || identities[2] != "explicit:evidence-run-3" || identities[3] == identities[4] {
		t.Fatalf("evidence identities = %v", identities)
	}
}

func TestLifecycleRetriesTerminalFailuresWithinControllerIterationBudget(t *testing.T) {
	dataDir := t.TempDir()
	key := schedulerKey(1)
	target := schedulerTarget(key, "head-1")
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 2},
	}
	policy, err := automatic.NewTrustedPolicy(policyRecord, target)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := automatic.NewController(
		store.NewFilesystemLoopDecisionStore(dataDir),
		store.NewFilesystemEconomicsStore(dataDir),
		store.NewFilesystemRunStore(dataDir),
	)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := automatic.WorkspaceAuthorization("retry-budget-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, authorization); err != nil {
		t.Fatal(err)
	}
	clock := &advancingClock{now: time.Now()}
	var prepared int
	var executed int
	lifecycle, err := NewLifecycle(watch.Config{
		PollInterval:    time.Minute,
		SettleTime:      time.Minute,
		MaxReviews:      10,
		MaxDuration:     time.Hour,
		UncertainPolicy: watch.UncertainWait,
	}, watch.Polling{
		Clock: clock,
		State: func(context.Context) (watch.PRState, error) {
			return watch.PRState{HeadSHA: "head-1"}, nil
		},
	}, BackgroundReview{
		Key:        key,
		Controller: controller,
		Runs:       store.NewFilesystemRunStore(dataDir),
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			prepared++
			runID := fmt.Sprintf("failed-%d", prepared)
			return PreparedCycle{
				Policy: policy,
				Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
					executed++
					run := schedulerRun(t, runID, target, domain.ReviewConclusionClean)
					run.Status = domain.ReviewStatusFailed
					run.Conclusion = domain.ReviewConclusionNone
					run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: "failed"}
					return run, nil
				}),
			}, nil
		},
	}, nil, watch.Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	if reason := lifecycle.Run(context.Background()); reason != watch.ReasonStopped {
		t.Fatalf("reason = %v, want controller stop", reason)
	}
	if prepared != 3 || executed != 2 {
		t.Fatalf("prepared = %d, executed = %d, want 3 and 2", prepared, executed)
	}
	runs, corrupt, err := store.NewFilesystemRunStore(dataDir).ListRuns(key)
	if err != nil || len(corrupt) != 0 || len(runs) != 2 {
		t.Fatalf("runs = %+v, corrupt = %+v, err = %v", runs, corrupt, err)
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
	requestTarget := target
	requestTarget.Revision.HeadObjectID = ""
	requestTarget.Revision.BaseObjectID = ""
	work, err := NewBackgroundWork(context.Background(), service, reviewpkg.Request{
		Target:        requestTarget.ToDomain(),
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
	if work.runID != "run-clean" || work.target.Revision.HeadObjectID != "head-1" || work.target.Revision.BaseObjectID != "base-1" {
		t.Fatalf("background work identity = %q %+v, want resolved prepared identity", work.runID, work.target.Revision)
	}
	controller := &controllerStub{decision: automatic.Decision{Allowed: true}}
	runStore := store.NewFilesystemRunStore(t.TempDir())
	port, err := (BackgroundReview{
		Key:        key,
		Controller: controller,
		Runs:       runStore,
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			return NewPreparedCycle(work, store.AdjudicationPolicyV1{
				SchemaVersion: store.CurrentSchemaVersion,
				Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
				Budget:        store.BudgetPolicyV1{MaxIterations: 1},
			})
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
	runs, corrupt, err := runStore.ListRuns(key)
	if err != nil || len(corrupt) != 0 || len(runs) != 1 || runs[0].ID != "run-clean" {
		t.Fatalf("persisted runs = %+v, corrupt = %+v, err = %v", runs, corrupt, err)
	}
	if len(controller.recorded) != 1 || controller.recorded[0].ModelCallCount != 0 {
		t.Fatalf("recorded economics = %+v, want exact zero model calls", controller.recorded)
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
		Runs:       store.NewFilesystemRunStore(t.TempDir()),
		Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
			mu.Lock()
			head := states[stateIndex-1].HeadSHA
			heads = append(heads, head)
			mu.Unlock()
			target := schedulerTarget(key, head)
			runID := "run-" + head
			return PreparedCycle{
				Work: schedulerWork(runID, target, func(context.Context) (*domain.ReviewRun, error) {
					conclusion := domain.ReviewConclusionFindings
					if head == "head-b" {
						conclusion = domain.ReviewConclusionClean
					}
					return schedulerRun(t, runID, target, conclusion), nil
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

func TestLifecycleMapsControllerStopAndEscalation(t *testing.T) {
	tests := []struct {
		kind store.LoopDecisionKindV1
		want watch.ExitReason
	}{
		{kind: store.LoopDecisionStop, want: watch.ReasonStopped},
		{kind: store.LoopDecisionEscalate, want: watch.ReasonEscalated},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			key := schedulerKey(1)
			target := schedulerTarget(key, "head-1")
			clock := &advancingClock{now: time.Now()}
			lifecycle, err := NewLifecycle(watch.Config{
				PollInterval:    time.Minute,
				SettleTime:      time.Minute,
				MaxReviews:      2,
				MaxDuration:     time.Hour,
				UncertainPolicy: watch.UncertainWait,
			}, watch.Polling{
				Clock: clock,
				State: func(context.Context) (watch.PRState, error) {
					return watch.PRState{HeadSHA: "head-1"}, nil
				},
			}, BackgroundReview{
				Key:        key,
				Controller: &controllerStub{decision: automatic.Decision{Kind: test.kind, Reason: "controlled"}},
				Runs:       store.NewFilesystemRunStore(t.TempDir()),
				Prepare: func(context.Context, int, string, []watch.Discussion, string) (PreparedCycle, error) {
					return PreparedCycle{Work: schedulerWork("run-controlled", target, func(context.Context) (*domain.ReviewRun, error) {
						t.Fatal("controlled termination executed review work")
						return nil, nil
					})}, nil
				},
			}, nil, watch.Presentation{})
			if err != nil {
				t.Fatal(err)
			}
			if reason := lifecycle.Run(context.Background()); reason != test.want {
				t.Fatalf("reason = %v, want %v", reason, test.want)
			}
		})
	}
}
