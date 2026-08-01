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
	}, store.ReviewTargetV1{})
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

func TestControllerRequiresTrustedCommissionAndRecordsAdmission(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()

	if _, err := controller.AuthorizeReview(key, "run-before-commission"); err == nil {
		t.Fatal("expected uncommissioned automatic review to be rejected")
	}

	decision, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t))
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	for _, runID := range []string{"run-head-one", "run-head-two"} {
		decision, err := controller.AuthorizeReview(key, runID)
		if err != nil {
			t.Fatalf("AuthorizeReview(%s): %v", runID, err)
		}
		if !decision.Allowed || decision.Kind != store.LoopDecisionContinue {
			t.Fatalf("expected %s to be admitted, got %+v", runID, decision)
		}
	}
	stopped, err := controller.AuthorizeReview(key, "run-head-three")
	if err != nil {
		t.Fatalf("AuthorizeReview(stop): %v", err)
	}
	if stopped.Allowed || stopped.Kind != store.LoopDecisionStop || !strings.Contains(stopped.Reason, "review bound") {
		t.Fatalf("expected durable review-bound stop, got %+v", stopped)
	}

	restarted := testController(t, dir, &now)
	stillStopped, err := restarted.AuthorizeReview(key, "run-after-restart")
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
	if _, err := restarted.Resume(key, store.ReviewTargetV1{}, policy, workspace); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumed, err := restarted.AuthorizeReview(key, "run-after-resume")
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 10, 30*time.Minute, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}

	now = started.Add(10 * time.Minute)
	allowed, err := controller.AuthorizeReview(key, "run-one")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allowed || !allowed.Deadline.Equal(started.Add(30*time.Minute)) {
		t.Fatalf("expected lifecycle deadline %s, got %+v", started.Add(30*time.Minute), allowed)
	}

	now = started.Add(31 * time.Minute)
	stopped, err := controller.AuthorizeReview(key, "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != store.LoopDecisionStop || !strings.Contains(stopped.Reason, "duration bound") {
		t.Fatalf("expected duration-bound stop, got %+v", stopped)
	}
}

func TestControllerDoesNotCountSemanticConvergenceDecisionsAsReviewAdmissions(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "automatic-run-one"); err != nil {
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

	second, err := controller.AuthorizeReview(key, "automatic-run-two")
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 10, time.Hour, 1), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "run-one"); err != nil {
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

	stopped, err := controller.AuthorizeReview(key, "run-two")
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 10, time.Hour, 5), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "run-one"); err != nil {
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

	escalated, err := controller.AuthorizeReview(key, "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Kind != store.LoopDecisionEscalate || escalated.Allowed || !strings.Contains(escalated.Reason, "could not be measured") {
		t.Fatalf("expected unavailable provider usage to escalate, got %+v", escalated)
	}
	again, err := controller.AuthorizeReview(key, "run-three")
	if err != nil {
		t.Fatal(err)
	}
	if again.Kind != store.LoopDecisionEscalate || again.Allowed {
		t.Fatalf("escalation did not block subsequent automatic work: %+v", again)
	}
}

func TestTrustedPolicyRejectsReviewedWorktreeAndHead(t *testing.T) {
	target := store.ReviewTargetV1{Revision: store.RevisionEvidenceV1{HeadObjectID: "head-sha"}}
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

func TestControllerSerializesReviewAdmissionAcrossInstances(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	first := testController(t, dir, &now)
	second := testController(t, dir, &now)
	key := testKey()
	policy := testPolicy(t, 1, time.Hour, 0)
	if _, err := first.Commission(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
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
			decision, err := controller.AuthorizeReview(key, fmt.Sprintf("concurrent-run-%d", index))
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "reused-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "stop-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "reused-run"); err == nil {
		t.Fatal("expected run id reused across sessions to be rejected")
	}
}

func TestControllerRecordsUnknownBudgetForCorruptHistory(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	controller := testController(t, dir, &now)
	key := testKey()
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, testPolicy(t, 2, time.Hour, 0), testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	decisionDir := filepath.Join(dir, "prs", key.Host, key.Owner, key.Repository, fmt.Sprintf("%d", key.Number), "loop_decisions")
	if err := os.WriteFile(filepath.Join(decisionDir, "20260731T120001.000000000Z-corrupt.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	escalated, err := controller.AuthorizeReview(key, "run-after-corruption")
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
	if _, err := controller.Commission(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AuthorizeReview(key, "run-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Escalate(key, "operator decision required"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Resume(key, store.ReviewTargetV1{}, policy, testUserAuthorization(t)); err != nil {
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
