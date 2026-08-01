package automatic

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

var testIDCounter atomic.Int64

func testKey() store.PullRequestKeyV1 {
	return store.PullRequestKeyV1{Host: "github.com", Owner: "owner", Repository: "repo", Number: 42}
}

func testTarget() store.ReviewTargetV1 {
	key := testKey()
	return store.ReviewTargetV1{PullRequest: &key}
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
	return controller.AuthorizeReview(key, testTarget(), testPolicy(t, 1, time.Hour, 0), runID)
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
			if _, err := controller.AuthorizeReview(key, testTarget(), policy, "run-before-corruption"); err != nil {
				t.Fatal(err)
			}
			economicsDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "economics")
			if err := os.MkdirAll(economicsDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(economicsDir, "20260731T120001.000000000Z-corrupt.json"), []byte("not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			decision, err := controller.AuthorizeReview(key, testTarget(), policy, "run-after-corruption")
			if err != nil {
				t.Fatal(err)
			}
			if decision.Kind != tt.wantKind || decision.Allowed != tt.allowed {
				t.Fatalf("unexpected corruption decision: %+v", decision)
			}
		})
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
	targetA := store.ReviewTargetV1{Revision: store.RevisionEvidenceV1{HeadObjectID: "head-a"}, PullRequest: &key}
	targetB := store.ReviewTargetV1{Revision: store.RevisionEvidenceV1{HeadObjectID: "head-b"}, PullRequest: &key}
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
	if _, err := controller.Commission(key, testTarget(), testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	decisionDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "loop_decisions")
	if err := os.WriteFile(filepath.Join(decisionDir, "20260731T120001.000000000Z-corrupt.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	escalated, err := testAuthorize(t, controller, key, "run-after-corruption")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Kind != store.LoopDecisionEscalate || escalated.Budget.Known {
		t.Fatalf("expected corrupt history escalation with unknown budget, got %+v", escalated)
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
