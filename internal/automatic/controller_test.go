package automatic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

var testIDCounter atomic.Int64

type failingEconomicsStore struct {
	store.EconomicsStore
	listCalls int
	failAfter int
}

type releaseFailingDecisionStore struct {
	store.LoopDecisionStore
}

type unreadableCorruptDecisionStore struct {
	store.LoopDecisionStore
	reportCorruption bool
}

func (s *unreadableCorruptDecisionStore) ListLoopDecisions(key store.PullRequestKeyV1) ([]store.LoopDecisionV1, []store.CorruptRecord, error) {
	decisions, corrupt, err := s.LoopDecisionStore.ListLoopDecisions(key)
	if s.reportCorruption {
		corrupt = append(corrupt, store.CorruptRecord{Path: "/unreadable/corrupt.json"})
	}
	return decisions, corrupt, err
}

func (s *releaseFailingDecisionStore) AcquireDecisionWriteLock() (func() error, error) {
	return func() error { return fmt.Errorf("release failed") }, nil
}

func (s *failingEconomicsStore) ListEconomics(key store.PullRequestKeyV1) ([]store.EconomicsRecordV1, []store.CorruptRecord, error) {
	s.listCalls++
	if s.listCalls > s.failAfter {
		return nil, nil, fmt.Errorf("economics backend unavailable")
	}
	return s.EconomicsStore.ListEconomics(key)
}

func testKey() store.PullRequestKeyV1 {
	return store.PullRequestKeyV1{Host: "github.com", Owner: "owner", Repository: "repo", Number: 42}
}

func testTarget() store.ReviewTargetV1 {
	key := testKey()
	return store.ReviewTargetV1{
		RepositoryRoot: "/repo",
		WorktreeRoot:   "/repo/worktree",
		Revision: store.RevisionEvidenceV1{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "refs/remotes/origin/main",
			HeadObjectID:     "head-object",
			BaseObjectID:     "base-object",
		},
		PullRequest: &key,
	}
}

func testPolicy(t *testing.T, reviews int, duration time.Duration, cost float64) TrustedPolicy {
	t.Helper()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget: store.BudgetPolicyV1{
			MaxIterations: reviews,
			MaxDuration:   duration,
			MaxCostUSD:    cost,
		},
	}, testTarget())
	if err != nil {
		t.Fatalf("NewTrustedPolicy: %v", err)
	}
	return policy
}

func testController(t *testing.T, dir string, now *time.Time) *Controller {
	t.Helper()
	controller, err := NewController(
		store.NewFilesystemLoopDecisionStore(dir),
		store.NewFilesystemEconomicsStore(dir),
		store.NewFilesystemRunStore(dir),
	)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	controller.now = func() time.Time { return *now }
	controller.newID = func() (string, error) {
		return fmt.Sprintf("id-%d", testIDCounter.Add(1)), nil
	}
	return controller
}

func testUserAuthorization(t *testing.T) Authorization {
	t.Helper()
	authorization, err := UserAuthorization("alice")
	if err != nil {
		t.Fatalf("UserAuthorization: %v", err)
	}
	return authorization
}

func testAuthorize(t *testing.T, controller *Controller, key store.PullRequestKeyV1, runID string) (Decision, error) {
	t.Helper()
	target := testTarget()
	target.Revision.HeadObjectID = "head-" + runID
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 1, MaxDuration: time.Hour},
	}, target)
	if err != nil {
		t.Fatalf("NewTrustedPolicy: %v", err)
	}
	decision, err := controller.AuthorizeReview(key, target, policy, runID)
	if err == nil && decision.Allowed {
		saveAutomaticRunToStore(t, controller.runs, runID, target, domain.ReviewStatusCompleted)
	}
	return decision, err
}

func authorizeWithHead(t *testing.T, controller *Controller, key store.PullRequestKeyV1, policy TrustedPolicy, runID string) (Decision, error) {
	t.Helper()
	target := testTarget()
	target.Revision.HeadObjectID = "head-" + runID
	policy.target = target
	decision, err := controller.AuthorizeReview(key, target, policy, runID)
	if err == nil && decision.Allowed {
		saveAutomaticRunToStore(t, controller.runs, runID, target, domain.ReviewStatusCompleted)
	}
	return decision, err
}

func saveAutomaticRun(t *testing.T, dir, runID string, target store.ReviewTargetV1, status domain.ReviewStatus) {
	t.Helper()
	saveAutomaticRunToStore(t, store.NewFilesystemRunStore(dir), runID, target, status)
}

func saveAutomaticRunToStore(t *testing.T, runStore store.RunStore, runID string, target store.ReviewTargetV1, status domain.ReviewStatus) {
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
		t.Fatal(err)
	}
	run := domain.ReviewRun{
		ID:                       runID,
		Target:                   target.ToDomain(),
		Trigger:                  domain.ReviewTriggerDesk,
		Engine:                   domain.ReviewEngine{Name: "acr", Version: "test"},
		StartedAt:                time.Unix(1, 0),
		CompletedAt:              time.Unix(2, 0),
		Configuration:            configuration,
		ConfigurationSource:      domain.ConfigurationSourceIdentity{Kind: "test"},
		ConfigurationFingerprint: configuration.Fingerprint(),
		Status:                   status,
	}
	if status == domain.ReviewStatusCompleted {
		run.Conclusion = domain.ReviewConclusionClean
	} else {
		run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: string(status)}
	}
	schema, err := store.ToReviewRunSchema(run, store.RenderedOutcomeV1{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runStore.SaveRun(schema); err != nil {
		t.Fatal(err)
	}
}

func TestControllerDeduplicatesCompletedRevisionButAllowsFailedAndInterruptedRetries(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	target := testTarget()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 10},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, target, policy, "failed-run"); err != nil {
		t.Fatal(err)
	}
	saveAutomaticRun(t, dir, "failed-run", target, domain.ReviewStatusFailed)
	if _, err := controller.AuthorizeReview(key, target, policy, "interrupted-run"); err != nil {
		t.Fatalf("failed revision was not retryable: %v", err)
	}
	saveAutomaticRun(t, dir, "interrupted-run", target, domain.ReviewStatusInterrupted)
	if _, err := controller.AuthorizeReview(key, target, policy, "completed-run"); err != nil {
		t.Fatalf("interrupted revision was not retryable: %v", err)
	}
	saveAutomaticRun(t, dir, "completed-run", target, domain.ReviewStatusCompleted)
	if _, err := controller.Escalate(key, "trusted review"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, target, policy, "duplicate-run"); !errors.Is(err, ErrRevisionAlreadyAuthorized) {
		t.Fatalf("completed revision after resume error = %v, want duplicate rejection", err)
	}
}

func TestControllerRejectsUnresolvedAutomaticTarget(t *testing.T) {
	now := time.Now()
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	target := testTarget()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	target.Revision.HeadObjectID = ""
	policy.target = target
	if _, err := controller.AuthorizeReview(key, target, policy, "unresolved-run"); err == nil || !strings.Contains(err.Error(), "resolved object ids") {
		t.Fatalf("AuthorizeReview error = %v, want unresolved target rejection", err)
	}
}

func TestControllerEscalatesOrphanReservationAndResumeRecovers(t *testing.T) {
	now := time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	target := testTarget()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 5},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, target, policy, "orphan-run"); err != nil {
		t.Fatal(err)
	}
	escalated, err := controller.AuthorizeReview(key, target, policy, "blocked-run", "discussion:new-evidence")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Allowed || escalated.Kind != store.LoopDecisionEscalate {
		t.Fatalf("orphan decision = %+v, want controlled escalation", escalated)
	}
	decisions, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 || decisions[len(decisions)-1].Decision != store.LoopDecisionEscalate {
		t.Fatalf("durable decisions = %+v, corrupt = %+v, err = %v", decisions, corrupt, err)
	}
	if _, err := controller.Resume(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if decision, err := controller.AuthorizeReview(key, target, policy, "recovered-run"); err != nil || !decision.Allowed {
		t.Fatalf("post-resume authorization = %+v, err = %v", decision, err)
	}
}

func TestControllerEscalatesCrossControllerOrphanForDifferentRevision(t *testing.T) {
	now := time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	first := testController(t, dir, &now)
	second := testController(t, dir, &now)
	key := testKey()
	firstTarget := testTarget()
	secondTarget := testTarget()
	secondTarget.Revision.HeadObjectID = "replacement-head"
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 5},
	}
	firstPolicy, err := NewTrustedPolicy(policyRecord, firstTarget)
	if err != nil {
		t.Fatal(err)
	}
	secondPolicy, err := NewTrustedPolicy(policyRecord, secondTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Commission(key, firstTarget, firstPolicy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AuthorizeReview(key, firstTarget, firstPolicy, "active-first-head"); err != nil {
		t.Fatal(err)
	}
	escalated, err := second.AuthorizeReview(key, secondTarget, secondPolicy, "blocked-replacement-head")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Allowed || escalated.Kind != store.LoopDecisionEscalate {
		t.Fatalf("different-head orphan decision = %+v, want controlled escalation", escalated)
	}
	decisions, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("decisions = %+v, corrupt = %+v, err = %v", decisions, corrupt, err)
	}
	var continues int
	for _, decision := range decisions {
		if decision.Decision == store.LoopDecisionContinue {
			continues++
		}
	}
	if continues != 1 || decisions[len(decisions)-1].Decision != store.LoopDecisionEscalate {
		t.Fatalf("durable decisions = %+v", decisions)
	}
}

func TestControllerOrphanEscalationRefreshesBudget(t *testing.T) {
	now := time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	target := testTarget()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget: store.BudgetPolicyV1{
			MaxIterations: 5,
			MaxDuration:   2 * time.Hour,
		},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, target, policy, "orphan-budget-run"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(37 * time.Minute)
	escalated, err := controller.AuthorizeReview(key, target, policy, "blocked-budget-run")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Kind != store.LoopDecisionEscalate || escalated.Budget.Elapsed != 37*time.Minute || escalated.Budget.IterationsUsed != 1 {
		t.Fatalf("refreshed orphan budget = %+v", escalated)
	}
	decisions, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("decisions = %+v, corrupt = %+v, err = %v", decisions, corrupt, err)
	}
	latest := decisions[len(decisions)-1]
	if latest.Budget.Elapsed != 37*time.Minute || latest.Budget.IterationsUsed != 1 {
		t.Fatalf("durable refreshed budget = %+v", latest.Budget)
	}
}

func TestControllerDeduplicatesCompletedTargetByEvidenceIdentity(t *testing.T) {
	now := time.Date(2026, 8, 4, 14, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	target := testTarget()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 5},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, target, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, target, policy, "discussion-a", "discussion:evidence-a"); err != nil {
		t.Fatal(err)
	}
	saveAutomaticRun(t, dir, "discussion-a", target, domain.ReviewStatusCompleted)
	if _, err := controller.AuthorizeReview(key, target, policy, "discussion-a-repeat", "discussion:evidence-a"); !errors.Is(err, ErrRevisionAlreadyAuthorized) {
		t.Fatalf("repeated discussion evidence error = %v, want duplicate rejection", err)
	}
	if decision, err := controller.AuthorizeReview(key, target, policy, "discussion-b", "discussion:evidence-b"); err != nil || !decision.Allowed {
		t.Fatalf("new discussion evidence decision = %+v, err = %v", decision, err)
	}
	decisions, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("decisions = %+v, corrupt = %+v, err = %v", decisions, corrupt, err)
	}
	var identities []string
	for _, decision := range decisions {
		if decision.Decision == store.LoopDecisionContinue {
			identities = append(identities, decision.EvidenceIdentity)
		}
	}
	if fmt.Sprint(identities) != "[discussion:evidence-a discussion:evidence-b]" {
		t.Fatalf("evidence identities = %v", identities)
	}
}

func TestControllerRequiresTrustedCommissionAndRecordsAdmission(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()

	if _, err := testAuthorize(t, controller, key, "run-before-commission"); err == nil {
		t.Fatal("expected uncommissioned automatic review to be rejected")
	}

	decision, err := controller.Commission(key, testTarget(), testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t))
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if decision.Kind != store.LoopDecisionAdmit || decision.Allowed {
		t.Fatalf("unexpected admission decision: %+v", decision)
	}

	records, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("ListLoopDecisions: corrupt=%v err=%v", corrupt, err)
	}
	if len(records) != 1 || records[0].AuthorizationKind != string(AuthorizationUser) || records[0].AuthorizedBy != "alice" {
		t.Fatalf("trusted admission was not durably attributed: %+v", records)
	}
	if records[0].PolicySource == nil || records[0].PolicySource.Kind != config.SourceKindDefaults {
		t.Fatalf("trusted policy source was not recorded: %+v", records[0].PolicySource)
	}
}

func TestControllerReviewBoundPersistsAcrossRunsAndRequiresResume(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	for _, runID := range []string{"run-head-one", "run-head-two"} {
		decision, err := testAuthorize(t, controller, key, runID)
		if err != nil {
			t.Fatalf("AuthorizeReview(%s): %v", runID, err)
		}
		if !decision.Allowed || decision.Kind != store.LoopDecisionContinue {
			t.Fatalf("expected %s to be admitted, got %+v", runID, decision)
		}
	}
	stopped, err := testAuthorize(t, controller, key, "run-head-three")
	if err != nil {
		t.Fatalf("AuthorizeReview(stop): %v", err)
	}
	if stopped.Allowed || stopped.Kind != store.LoopDecisionStop || !strings.Contains(stopped.Reason, "review bound") {
		t.Fatalf("expected durable review-bound stop, got %+v", stopped)
	}

	restarted := testController(t, dir, &now)
	stillStopped, err := testAuthorize(t, restarted, key, "run-after-restart")
	if err != nil {
		t.Fatalf("AuthorizeReview after restart: %v", err)
	}
	if stillStopped.Kind != store.LoopDecisionStop || stillStopped.Allowed {
		t.Fatalf("restart bypassed exhausted budget: %+v", stillStopped)
	}

	workspace, err := WorkspaceAuthorization("scheduler-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Resume(key, testTarget(), policy, workspace); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumed, err := testAuthorize(t, restarted, key, "run-after-resume")
	if err != nil {
		t.Fatalf("AuthorizeReview after resume: %v", err)
	}
	if !resumed.Allowed || resumed.Budget.IterationsUsed != 1 {
		t.Fatalf("trusted resume did not start a new bounded session: %+v", resumed)
	}
}

func TestControllerDurationBoundAndDeadlineSpanLifecycle(t *testing.T) {
	started := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := started
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 10, 30*time.Minute, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	now = started.Add(10 * time.Minute)
	allowed, err := testAuthorize(t, controller, key, "run-one")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allowed || !allowed.Deadline.Equal(started.Add(30*time.Minute)) {
		t.Fatalf("expected lifecycle deadline %s, got %+v", started.Add(30*time.Minute), allowed)
	}

	now = started.Add(31 * time.Minute)
	stopped, err := testAuthorize(t, controller, key, "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != store.LoopDecisionStop || !strings.Contains(stopped.Reason, "duration bound") {
		t.Fatalf("expected duration-bound stop, got %+v", stopped)
	}
}

func TestControllerDurationStartsAtAdmissionDespiteFutureHistory(t *testing.T) {
	started := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := started
	dir := t.TempDir()
	key := testKey()
	future := store.LoopDecisionV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "future-semantic-decision",
		PullRequest:   key,
		RunID:         "future-run",
		Scope:         store.LoopDecisionScopeSemanticConvergence,
		Decision:      store.LoopDecisionContinue,
		Reason:        "future clock skew",
		Budget:        store.BudgetStateV1{Known: true},
		DecidedAt:     started.Add(2 * time.Hour),
	}
	if _, err := store.NewFilesystemLoopDecisionStore(dir).SaveLoopDecision(future); err != nil {
		t.Fatal(err)
	}
	controller := testController(t, dir, &now)
	admission, err := controller.Commission(key, testTarget(), testPolicy(t, 10, 30*time.Minute, 0), testUserAuthorization(t))
	if err != nil {
		t.Fatal(err)
	}
	if !admission.Budget.StartedAt.Equal(started) {
		t.Fatalf("duration started at %s, want actual admission time %s", admission.Budget.StartedAt, started)
	}
	now = started.Add(31 * time.Minute)
	stopped, err := testAuthorize(t, controller, key, "run-after-duration")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != store.LoopDecisionStop {
		t.Fatalf("future history postponed duration stop: %+v", stopped)
	}
}

func TestControllerSamplesAdmissionClockOnce(t *testing.T) {
	first := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	second := first.Add(-time.Minute)
	dir := t.TempDir()
	controller, err := NewController(
		store.NewFilesystemLoopDecisionStore(dir),
		store.NewFilesystemEconomicsStore(dir),
		store.NewFilesystemRunStore(dir),
	)
	if err != nil {
		t.Fatal(err)
	}
	times := []time.Time{first, second}
	calls := 0
	controller.now = func() time.Time {
		value := times[calls]
		calls++
		return value
	}
	controller.newID = func() (string, error) {
		return fmt.Sprintf("id-%d", testIDCounter.Add(1)), nil
	}
	admission, err := controller.Commission(testKey(), testTarget(), testPolicy(t, 1, time.Hour, 0), testUserAuthorization(t))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("sampled admission clock %d times, want 1", calls)
	}
	if !admission.Budget.StartedAt.Equal(first) {
		t.Fatalf("admission started at %s, want %s", admission.Budget.StartedAt, first)
	}
}

func TestControllerElapsedDurationDoesNotRegressWithClock(t *testing.T) {
	started := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	now := started
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 10, time.Hour, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	now = started.Add(20 * time.Minute)
	first, err := testAuthorize(t, controller, key, "run-before-regression")
	if err != nil {
		t.Fatal(err)
	}
	now = started.Add(10 * time.Minute)
	second, err := testAuthorize(t, controller, key, "run-after-regression")
	if err != nil {
		t.Fatal(err)
	}
	if second.Budget.Elapsed < first.Budget.Elapsed {
		t.Fatalf("elapsed duration regressed from %s to %s", first.Budget.Elapsed, second.Budget.Elapsed)
	}
}

func TestControllerDoesNotCountSemanticConvergenceDecisionsAsReviewAdmissions(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "automatic-run-one"); err != nil {
		t.Fatal(err)
	}
	semantic := store.LoopDecisionV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "semantic-decision",
		PullRequest:   key,
		RunID:         "automatic-run-one",
		Scope:         store.LoopDecisionScopeSemanticConvergence,
		Decision:      store.LoopDecisionContinue,
		Reason:        "new actionable evidence",
		Budget:        store.BudgetStateV1{Known: true},
		DecidedAt:     now.Add(time.Minute),
	}
	if _, err := store.NewFilesystemLoopDecisionStore(dir).SaveLoopDecision(semantic); err != nil {
		t.Fatalf("SaveLoopDecision: %v", err)
	}

	second, err := testAuthorize(t, controller, key, "automatic-run-two")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Allowed || second.Budget.IterationsUsed != 2 {
		t.Fatalf("semantic decision consumed the automatic review budget: %+v", second)
	}
}

func TestControllerEnforcesMeasuredProviderCost(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 10, time.Hour, 1), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "run-one"); err != nil {
		t.Fatal(err)
	}
	economics := store.ReviewEconomicsV1{
		SchemaVersion: store.CurrentSchemaVersion,
		RunID:         "run-one",
		ProviderUsage: []store.ProviderUsageRecordV1{
			{Provider: "anthropic", Usage: store.ProviderUsageV1{Known: true, CostUSD: 1.25}},
		},
	}
	if err := controller.RecordEconomics(key, now, economics); err != nil {
		t.Fatalf("RecordEconomics: %v", err)
	}

	stopped, err := testAuthorize(t, controller, key, "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != store.LoopDecisionStop || stopped.Budget.CostUSDUsed != 1.25 || !stopped.Budget.CostKnown {
		t.Fatalf("expected measured provider-cost stop, got %+v", stopped)
	}
}

func TestControllerEscalatesWhenConfiguredProviderUsageIsUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 10, time.Hour, 5), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "run-one"); err != nil {
		t.Fatal(err)
	}
	economics := store.ReviewEconomicsV1{
		SchemaVersion: store.CurrentSchemaVersion,
		RunID:         "run-one",
		ProviderUsage: []store.ProviderUsageRecordV1{
			{Provider: "codex", Usage: store.ProviderUsageV1{Known: false}},
		},
	}
	if err := controller.RecordEconomics(key, now, economics); err != nil {
		t.Fatal(err)
	}

	escalated, err := testAuthorize(t, controller, key, "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Kind != store.LoopDecisionEscalate || escalated.Allowed || !strings.Contains(escalated.Reason, "could not be measured") {
		t.Fatalf("expected unavailable provider usage to escalate, got %+v", escalated)
	}
	again, err := testAuthorize(t, controller, key, "run-three")
	if err != nil {
		t.Fatal(err)
	}
	if again.Kind != store.LoopDecisionEscalate || again.Allowed {
		t.Fatalf("escalation did not block subsequent automatic work: %+v", again)
	}
}

func TestControllerClassifiesCorruptEconomicsByConfiguredBudget(t *testing.T) {
	tests := []struct {
		name     string
		cost     float64
		wantKind store.LoopDecisionKindV1
		allowed  bool
	}{
		{name: "no cost bound", cost: 0, wantKind: store.LoopDecisionContinue, allowed: true},
		{name: "cost bound", cost: 5, wantKind: store.LoopDecisionEscalate, allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
			dir := t.TempDir()
			controller := testController(t, dir, &now)
			key := testKey()
			policy := testPolicy(t, 3, time.Hour, tt.cost)
			if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
				t.Fatal(err)
			}
			if _, err := authorizeWithHead(t, controller, key, policy, "run-before-corruption"); err != nil {
				t.Fatal(err)
			}
			economicsDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "economics")
			if err := os.MkdirAll(economicsDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(economicsDir, "20260731T120001.000000000Z-corrupt.json"), []byte("not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			decision, err := authorizeWithHead(t, controller, key, policy, "run-after-corruption")
			if err != nil {
				t.Fatal(err)
			}
			if decision.Kind != tt.wantKind || decision.Allowed != tt.allowed {
				t.Fatalf("unexpected corruption decision: %+v", decision)
			}
		})
	}
}

func TestControllerFailsClosedForCorruptActiveEconomicsWithValidReplacement(t *testing.T) {
	tests := []struct {
		name        string
		cost        float64
		corruptName string
		wantKind    store.LoopDecisionKindV1
		allowed     bool
	}{
		{name: "cost disabled", cost: 0, corruptName: "20260731T120001.000000000Z-run-with-corruption.json", wantKind: store.LoopDecisionContinue, allowed: true},
		{name: "active run", cost: 5, corruptName: "20260731T120001.000000000Z-run-with-corruption.json", wantKind: store.LoopDecisionEscalate, allowed: false},
		{name: "unknown association", cost: 5, corruptName: "malformed.json", wantKind: store.LoopDecisionEscalate, allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
			dir := t.TempDir()
			controller := testController(t, dir, &now)
			key := testKey()
			policy := testPolicy(t, 3, time.Hour, tt.cost)
			if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
				t.Fatal(err)
			}
			if _, err := authorizeWithHead(t, controller, key, policy, "run-with-corruption"); err != nil {
				t.Fatal(err)
			}
			economicsDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "economics")
			if err := os.MkdirAll(economicsDir, 0o700); err != nil {
				t.Fatal(err)
			}
			corruptPath := filepath.Join(economicsDir, tt.corruptName)
			if err := os.WriteFile(corruptPath, []byte("not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := controller.RecordEconomics(key, now.Add(2*time.Second), store.ReviewEconomicsV1{
				SchemaVersion: store.CurrentSchemaVersion,
				RunID:         "run-with-corruption",
			}); err != nil {
				t.Fatal(err)
			}
			decision, err := authorizeWithHead(t, controller, key, policy, "run-after-corruption")
			if err != nil {
				t.Fatal(err)
			}
			if decision.Kind != tt.wantKind || decision.Allowed != tt.allowed {
				t.Fatalf("unexpected decision with corrupt active economics: %+v", decision)
			}
		})
	}
}

func TestControllerIgnoresCorruptEconomicsOutsideActiveSession(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 3, time.Hour, 5)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeWithHead(t, controller, key, policy, "old-session-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Escalate(key, "trusted restart requested"); err != nil {
		t.Fatal(err)
	}
	economicsDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "economics")
	if err := os.MkdirAll(economicsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(economicsDir, "20260731T120001.000000000Z-old-corrupt.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeWithHead(t, controller, key, policy, "active-session-run"); err != nil {
		t.Fatal(err)
	}
	if err := controller.RecordEconomics(key, now, store.ReviewEconomicsV1{
		SchemaVersion: store.CurrentSchemaVersion,
		RunID:         "active-session-run",
		ProviderUsage: []store.ProviderUsageRecordV1{
			{Provider: "codex", Usage: store.ProviderUsageV1{Known: true, CostUSD: 1}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := authorizeWithHead(t, controller, key, policy, "next-active-run")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.Budget.CostUSDUsed != 1 || !decision.Budget.CostKnown {
		t.Fatalf("unrelated corrupt economics poisoned active session: %+v", decision)
	}
}

func TestControllerSkipsBudgetEconomicsReadWithoutCostLimit(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	economics := &failingEconomicsStore{
		EconomicsStore: store.NewFilesystemEconomicsStore(dir),
		failAfter:      2,
	}
	controller, err := NewController(store.NewFilesystemLoopDecisionStore(dir), economics, store.NewFilesystemRunStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }
	controller.newID = func() (string, error) {
		return fmt.Sprintf("id-%d", testIDCounter.Add(1)), nil
	}
	key := testKey()
	policy := testPolicy(t, 3, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeWithHead(t, controller, key, policy, "run-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeWithHead(t, controller, key, policy, "run-two"); err != nil {
		t.Fatal(err)
	}
	if economics.listCalls != 2 {
		t.Fatalf("got %d economics reads, want only the two run-id checks", economics.listCalls)
	}
}

func TestTrustedPolicyRejectsReviewedWorktreeAndHead(t *testing.T) {
	target := testTarget()
	target.Revision.HeadObjectID = "head-sha"
	base := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Budget:        store.BudgetPolicyV1{MaxIterations: 1},
	}

	filesystem := base
	filesystem.Source = store.PolicySourceV1{Kind: config.SourceKindFilesystem, Locator: "/review/worktree/.acr.yaml"}
	if _, err := NewTrustedPolicy(filesystem, target); err == nil {
		t.Fatal("expected reviewed-worktree policy to be rejected")
	}

	head := base
	head.Source = store.PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: "head-sha"}
	if _, err := NewTrustedPolicy(head, target); err == nil {
		t.Fatal("expected reviewed-head policy to be rejected")
	}
}

func TestTrustedPolicyCannotBeReusedForAnotherTarget(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	targetA := testTarget()
	targetA.Revision.HeadObjectID = "head-a"
	targetB := testTarget()
	targetB.Revision.HeadObjectID = "head-b"
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 1},
	}, targetA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, targetB, policy, testUserAuthorization(t)); err == nil {
		t.Fatal("expected policy reuse for another target to be rejected")
	}
}

func TestTrustedPolicyRequiresPullRequestIdentity(t *testing.T) {
	_, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 1},
	}, store.ReviewTargetV1{})
	if err == nil {
		t.Fatal("expected target without pull request identity to be rejected")
	}
}

func TestTrustedPolicyRejectsIncompleteReviewTarget(t *testing.T) {
	key := testKey()
	_, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 1},
	}, store.ReviewTargetV1{PullRequest: &key})
	if err == nil {
		t.Fatal("expected incomplete review target to be rejected")
	}
}

func TestTrustedPolicyCopiesTargetIdentity(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	controller := testController(t, t.TempDir(), &now)
	target := testTarget()
	policy, err := NewTrustedPolicy(store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 1},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	target.PullRequest.Repository = "other-repo"
	mutatedKey := *target.PullRequest
	if _, err := controller.Commission(mutatedKey, target, policy, testUserAuthorization(t)); err == nil {
		t.Fatal("expected caller mutation not to rebind trusted policy")
	}
}

func TestControllerRevalidatesTrustedTargetForEachRun(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	targetA := testTarget()
	targetA.Revision.HeadObjectID = "head-a"
	policyRecord := store.AdjudicationPolicyV1{
		SchemaVersion: store.CurrentSchemaVersion,
		Source:        store.PolicySourceV1{Kind: config.SourceKindDefaults},
		Budget:        store.BudgetPolicyV1{MaxIterations: 2},
	}
	policyA, err := NewTrustedPolicy(policyRecord, targetA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Commission(key, targetA, policyA, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	targetB := testTarget()
	targetB.Revision.HeadObjectID = "head-b"
	if _, err := controller.AuthorizeReview(key, targetB, policyA, "run-head-b"); err == nil {
		t.Fatal("expected policy validated for old head to be rejected")
	}
	policyB, err := NewTrustedPolicy(policyRecord, targetB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, targetB, policyB, "run-head-b"); err != nil {
		t.Fatalf("AuthorizeReview with current target: %v", err)
	}
	records, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("ListLoopDecisions: corrupt=%v err=%v", corrupt, err)
	}
	if records[0].ReviewTarget == nil || records[0].ReviewTarget.Revision.HeadObjectID != "head-a" {
		t.Fatalf("admission did not preserve trusted target revision: %+v", records[0].ReviewTarget)
	}
}

func TestControllerRejectsRunIDAcrossDecisionScopesAndEconomics(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 3, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	semantic := store.LoopDecisionV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "semantic-collision",
		PullRequest:   key,
		RunID:         "semantic-run",
		Scope:         store.LoopDecisionScopeSemanticConvergence,
		Decision:      store.LoopDecisionContinue,
		Reason:        "semantic routing",
		Budget:        store.BudgetStateV1{Known: true},
		DecidedAt:     now.Add(time.Minute),
	}
	if _, err := store.NewFilesystemLoopDecisionStore(dir).SaveLoopDecision(semantic); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, testTarget(), policy, "semantic-run"); err == nil {
		t.Fatal("expected cross-scope run id collision to be rejected")
	}
	economics := store.ReviewEconomicsV1{SchemaVersion: store.CurrentSchemaVersion, RunID: "economics-run"}
	if _, err := store.NewFilesystemEconomicsStore(dir).SaveEconomics(key, now.Add(2*time.Minute), economics); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, testTarget(), policy, "economics-run"); err == nil {
		t.Fatal("expected economics run id collision to be rejected")
	}
}

func TestControllerSerializesReviewAdmissionAcrossInstances(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	first := testController(t, dir, &now)
	second := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 1, time.Hour, 0)
	if _, err := first.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	type result struct {
		decision Decision
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var group sync.WaitGroup
	for index, controller := range []*Controller{first, second} {
		group.Add(1)
		go func(index int, controller *Controller) {
			defer group.Done()
			<-start
			decision, err := testAuthorize(t, controller, key, fmt.Sprintf("concurrent-run-%d", index))
			results <- result{decision: decision, err: err}
		}(index, controller)
	}
	close(start)
	group.Wait()
	close(results)

	allowed := 0
	for result := range results {
		if result.err == nil && result.decision.Allowed {
			allowed++
		}
	}
	if allowed != 1 {
		t.Fatalf("got %d concurrently admitted reviews, want 1", allowed)
	}
}

func TestControllerRejectsRunIDReusedAcrossSessions(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 1, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "reused-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "stop-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "reused-run"); err == nil {
		t.Fatal("expected run id reused across sessions to be rejected")
	}
}

func TestControllerRecordsUnknownBudgetForCorruptHistory(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	decisionDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "loop_decisions")
	if err := os.WriteFile(filepath.Join(decisionDir, "malformed.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	escalated, err := testAuthorize(t, controller, key, "run-after-corruption")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Kind != store.LoopDecisionEscalate || escalated.Budget.Known {
		t.Fatalf("expected corrupt history escalation with unknown budget, got %+v", escalated)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatalf("trusted resume after corruption escalation: %v", err)
	}
	resumed, err := controller.AuthorizeReview(key, testTarget(), policy, "run-after-resume")
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Allowed {
		t.Fatalf("trusted resume did not isolate corrupt prior session: %+v", resumed)
	}
	if err := controller.RecordEconomics(key, now, store.ReviewEconomicsV1{
		SchemaVersion: store.CurrentSchemaVersion,
		RunID:         "run-after-resume",
	}); err != nil {
		t.Fatalf("record economics after trusted resume: %v", err)
	}
	if _, err := controller.Escalate(key, "operator decision required"); err != nil {
		t.Fatalf("explicit escalation after trusted resume: %v", err)
	}
	records, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 1 {
		t.Fatalf("ListLoopDecisions: corrupt=%v err=%v", corrupt, err)
	}
	var resumedAdmission store.LoopDecisionV1
	for _, record := range records {
		if record.Decision == store.LoopDecisionAdmit {
			resumedAdmission = record
		}
	}
	wantAcknowledgment := []store.CorruptRecordAcknowledgmentV1{{
		Name:        "malformed.json",
		Fingerprint: corrupt[0].Fingerprint,
	}}
	if !reflect.DeepEqual(resumedAdmission.AcknowledgedCorruptRecords, wantAcknowledgment) {
		t.Fatalf("trusted resume did not durably acknowledge corrupt history: %+v", resumedAdmission)
	}
}

func TestControllerEscalatesForCorruptionReplacedAfterTrustedResume(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	decisionDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "loop_decisions")
	if err := os.WriteFile(filepath.Join(decisionDir, "old-corrupt.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "run-before-resume"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decisionDir, "old-corrupt.json"), []byte("different invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, err := testAuthorize(t, controller, key, "run-after-replaced-corruption")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != store.LoopDecisionEscalate || decision.Budget.Known {
		t.Fatalf("expected replaced corruption to fail closed, got %+v", decision)
	}
}

func TestControllerRejectsResumeForUnacknowledgeableCorruption(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	decisionStore := &unreadableCorruptDecisionStore{LoopDecisionStore: store.NewFilesystemLoopDecisionStore(dir)}
	controller, err := NewController(decisionStore, store.NewFilesystemEconomicsStore(dir), store.NewFilesystemRunStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }
	controller.newID = func() (string, error) {
		return fmt.Sprintf("id-%d", testIDCounter.Add(1)), nil
	}
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Escalate(key, "operator decision required"); err != nil {
		t.Fatal(err)
	}
	decisionStore.reportCorruption = true
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err == nil || !strings.Contains(err.Error(), "cannot be durably acknowledged") {
		t.Fatalf("expected unacknowledgeable corruption to reject resume, got %v", err)
	}
	decisionStore.reportCorruption = false
	decisions, corrupt, err := decisionStore.ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("ListLoopDecisions: corrupt=%v err=%v", corrupt, err)
	}
	if len(decisions) != 2 || decisions[len(decisions)-1].Decision != store.LoopDecisionEscalate {
		t.Fatalf("failed resume persisted a new admission: %+v", decisions)
	}
}

func TestControllerResumeRejectsDecisionFromAnotherSession(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	foreign := store.LoopDecisionV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "foreign-stop",
		PullRequest:   key,
		SessionID:     "foreign-session",
		Scope:         store.LoopDecisionScopeAutomaticExecution,
		Decision:      store.LoopDecisionStop,
		Reason:        "foreign stop",
		Budget:        store.BudgetStateV1{Known: true, IterationsLimit: 2},
		DecidedAt:     now,
	}
	if _, err := store.NewFilesystemLoopDecisionStore(dir).SaveLoopDecision(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err == nil {
		t.Fatal("expected resume to reject a terminal decision from another session")
	}
}

func TestActiveSessionRejectsDecisionFromAnotherSession(t *testing.T) {
	admission := store.LoopDecisionV1{ID: "admission", SessionID: "session-a", Decision: store.LoopDecisionAdmit}
	foreign := store.LoopDecisionV1{ID: "foreign", SessionID: "session-b", Decision: store.LoopDecisionContinue}
	if _, _, err := activeSession([]store.LoopDecisionV1{admission, foreign}); err == nil {
		t.Fatal("expected decision from another session to be rejected")
	}
}

func TestControllerTreatsZeroCallEconomicsAsKnownZero(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	controller := testController(t, t.TempDir(), &now)
	key := testKey()
	policy := testPolicy(t, 3, time.Hour, 5)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeWithHead(t, controller, key, policy, "zero-call-run"); err != nil {
		t.Fatal(err)
	}
	if err := controller.RecordEconomics(key, now, store.ReviewEconomicsV1{
		SchemaVersion: store.CurrentSchemaVersion,
		RunID:         "zero-call-run",
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := authorizeWithHead(t, controller, key, policy, "run-after-zero-call")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || !decision.Budget.CostKnown || decision.Budget.CostUSDUsed != 0 {
		t.Fatalf("zero-call economics were not treated as known zero: %+v", decision)
	}
}

func TestControllerPropagatesDecisionLockReleaseFailure(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller, err := NewController(
		&releaseFailingDecisionStore{LoopDecisionStore: store.NewFilesystemLoopDecisionStore(dir)},
		store.NewFilesystemEconomicsStore(dir),
		store.NewFilesystemRunStore(dir),
	)
	if err != nil {
		t.Fatal(err)
	}
	controller.now = func() time.Time { return now }
	controller.newID = func() (string, error) {
		return fmt.Sprintf("id-%d", testIDCounter.Add(1)), nil
	}
	_, err = controller.Commission(testKey(), testTarget(), testPolicy(t, 1, time.Hour, 0), testUserAuthorization(t))
	if err == nil || !strings.Contains(err.Error(), "release automatic review decision lock") {
		t.Fatalf("expected lock release failure, got %v", err)
	}
}

func TestControllerRecordsExplicitEscalationAndTrustedResume(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 2, time.Hour, 0)
	if _, err := controller.Commission(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := testAuthorize(t, controller, key, "run-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Escalate(key, "operator decision required"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, testTarget(), policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	records, corrupt, err := store.NewFilesystemLoopDecisionStore(dir).ListLoopDecisions(key)
	if err != nil || len(corrupt) != 0 {
		t.Fatalf("ListLoopDecisions: corrupt=%v err=%v", corrupt, err)
	}
	want := []store.LoopDecisionKindV1{
		store.LoopDecisionAdmit,
		store.LoopDecisionContinue,
		store.LoopDecisionEscalate,
		store.LoopDecisionAdmit,
	}
	if len(records) != len(want) {
		t.Fatalf("got %d durable decisions, want %d: %+v", len(records), len(want), records)
	}
	for i := range want {
		if records[i].Decision != want[i] || records[i].Reason == "" || !records[i].Budget.Known {
			t.Fatalf("decision %d was not durably reasoned with budget state: %+v", i, records[i])
		}
	}
}
