package automatic

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

type AuthorizationKind string

const (
	AuthorizationUser      AuthorizationKind = "user"
	AuthorizationWorkspace AuthorizationKind = "workspace_controller"
)

type Authorization struct {
	kind  AuthorizationKind
	actor string
}

func UserAuthorization(actor string) (Authorization, error) {
	return newAuthorization(AuthorizationUser, actor)
}

func WorkspaceAuthorization(controller string) (Authorization, error) {
	return newAuthorization(AuthorizationWorkspace, controller)
}

func newAuthorization(kind AuthorizationKind, actor string) (Authorization, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Authorization{}, fmt.Errorf("automatic review authorization actor is required")
	}
	return Authorization{kind: kind, actor: actor}, nil
}

type TrustedPolicy struct {
	policy store.AdjudicationPolicyV1
	target store.ReviewTargetV1
}

func NewTrustedPolicy(policy store.AdjudicationPolicyV1, target store.ReviewTargetV1) (TrustedPolicy, error) {
	if err := policy.Validate(); err != nil {
		return TrustedPolicy{}, fmt.Errorf("automatic review policy: %w", err)
	}
	if err := store.ValidatePolicySourceOutsideReview(policy.Source, target); err != nil {
		return TrustedPolicy{}, fmt.Errorf("automatic review policy: %w", err)
	}
	if policy.Budget.MaxIterations == 0 && policy.Budget.MaxDuration == 0 {
		return TrustedPolicy{}, fmt.Errorf("automatic review policy requires a review or duration bound")
	}
	if target.PullRequest == nil {
		return TrustedPolicy{}, fmt.Errorf("automatic review target must identify a pull request")
	}
	targetCopy := target
	pullRequestCopy := *target.PullRequest
	targetCopy.PullRequest = &pullRequestCopy
	return TrustedPolicy{policy: policy, target: targetCopy}, nil
}

type Decision struct {
	Kind     store.LoopDecisionKindV1
	Reason   string
	Budget   store.BudgetStateV1
	Allowed  bool
	Deadline time.Time
}

type Controller struct {
	decisions store.LoopDecisionStore
	economics store.EconomicsStore
	now       func() time.Time
	newID     func() (string, error)
	mu        sync.Mutex
}

func NewController(decisions store.LoopDecisionStore, economics store.EconomicsStore) (*Controller, error) {
	if decisions == nil {
		return nil, fmt.Errorf("automatic review decision store is required")
	}
	if economics == nil {
		return nil, fmt.Errorf("automatic review economics store is required")
	}
	return &Controller{
		decisions: decisions,
		economics: economics,
		now:       time.Now,
		newID:     randomID,
	}, nil
}

func (c *Controller) Commission(key store.PullRequestKeyV1, target store.ReviewTargetV1, policy TrustedPolicy, authorization Authorization) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	release, err := c.decisions.AcquireDecisionWriteLock()
	if err != nil {
		return Decision{}, fmt.Errorf("lock automatic review decisions: %w", err)
	}
	defer func() { _ = release() }()
	if err := validateTrustedTarget(key, target, policy); err != nil {
		return Decision{}, err
	}

	decisions, corrupt, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return Decision{}, fmt.Errorf("load automatic review decisions: %w", err)
	}
	if len(corrupt) != 0 {
		return Decision{}, fmt.Errorf("automatic review history contains %d corrupt record(s)", len(corrupt))
	}
	for _, decision := range automaticDecisions(decisions) {
		if decision.Decision == store.LoopDecisionAdmit {
			return Decision{}, fmt.Errorf("automatic review is already commissioned; use a trusted resume after it stops")
		}
	}
	return c.admit(key, policy, authorization, "automatic review explicitly commissioned")
}

func (c *Controller) Resume(key store.PullRequestKeyV1, target store.ReviewTargetV1, policy TrustedPolicy, authorization Authorization) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	release, err := c.decisions.AcquireDecisionWriteLock()
	if err != nil {
		return Decision{}, fmt.Errorf("lock automatic review decisions: %w", err)
	}
	defer func() { _ = release() }()
	if err := validateTrustedTarget(key, target, policy); err != nil {
		return Decision{}, err
	}

	decisions, corrupt, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return Decision{}, fmt.Errorf("load automatic review decisions: %w", err)
	}
	if len(corrupt) != 0 {
		return Decision{}, fmt.Errorf("automatic review history contains %d corrupt record(s)", len(corrupt))
	}
	decisions = automaticDecisions(decisions)
	if len(decisions) == 0 {
		return Decision{}, fmt.Errorf("automatic review has not been commissioned")
	}
	latest := decisions[len(decisions)-1]
	if latest.Decision != store.LoopDecisionStop && latest.Decision != store.LoopDecisionEscalate {
		return Decision{}, fmt.Errorf("automatic review can only resume after a stop or escalation decision")
	}
	return c.admit(key, policy, authorization, "automatic review resumed by trusted decision")
}

func (c *Controller) AuthorizeReview(key store.PullRequestKeyV1, target store.ReviewTargetV1, policy TrustedPolicy, runID string) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	release, err := c.decisions.AcquireDecisionWriteLock()
	if err != nil {
		return Decision{}, fmt.Errorf("lock automatic review decisions: %w", err)
	}
	defer func() { _ = release() }()
	if err := validateTrustedTarget(key, target, policy); err != nil {
		return Decision{}, err
	}

	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Decision{}, fmt.Errorf("automatic review run id is required")
	}
	allDecisions, corrupt, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return Decision{}, fmt.Errorf("load automatic review decisions: %w", err)
	}
	decisions := automaticDecisions(allDecisions)
	if len(decisions) == 0 {
		return Decision{}, fmt.Errorf("automatic review has not been commissioned")
	}
	session, sessionDecisions, err := activeSession(decisions)
	if err != nil {
		return Decision{}, err
	}
	latest := sessionDecisions[len(sessionDecisions)-1]
	if latest.Decision == store.LoopDecisionStop || latest.Decision == store.LoopDecisionEscalate {
		return decisionFromRecord(latest, false), nil
	}
	if len(corrupt) != 0 {
		return c.recordDecision(key, session, store.LoopDecisionEscalate, "automatic review history is incomplete because durable decision records are corrupt", "", store.BudgetStateV1{}, false)
	}
	for _, decision := range allDecisions {
		if decision.RunID == runID {
			return Decision{}, fmt.Errorf("automatic review run %q already has a durable decision", runID)
		}
	}
	economics, _, err := c.economics.ListEconomics(key)
	if err != nil {
		return Decision{}, fmt.Errorf("load automatic review economics: %w", err)
	}
	for _, record := range economics {
		if record.Economics.RunID == runID {
			return Decision{}, fmt.Errorf("automatic review run %q already has durable economics", runID)
		}
	}

	budget, usageAvailable, err := c.currentBudget(key, session, sessionDecisions)
	if err != nil {
		return Decision{}, err
	}
	if budget.IterationsLimit > 0 && budget.IterationsUsed >= budget.IterationsLimit {
		return c.recordDecision(key, session, store.LoopDecisionStop, "automatic review stopped because the review bound was reached", "", budget, false)
	}
	if budget.DurationLimit > 0 && budget.Elapsed >= budget.DurationLimit {
		return c.recordDecision(key, session, store.LoopDecisionStop, "automatic review stopped because the lifecycle duration bound was reached", "", budget, false)
	}
	if budget.CostUSDLimit > 0 && !usageAvailable {
		return c.recordDecision(key, session, store.LoopDecisionEscalate, "automatic review requires trusted intervention because configured provider usage could not be measured", "", budget, false)
	}
	if budget.CostUSDLimit > 0 && budget.CostUSDUsed >= budget.CostUSDLimit {
		return c.recordDecision(key, session, store.LoopDecisionStop, "automatic review stopped because the provider usage bound was reached", "", budget, false)
	}

	budget.IterationsUsed++
	return c.recordDecision(key, session, store.LoopDecisionContinue, "automatic review admitted within the configured lifecycle bounds", runID, budget, true)
}

func (c *Controller) RecordEconomics(key store.PullRequestKeyV1, recordedAt time.Time, economics store.ReviewEconomicsV1) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	release, err := c.decisions.AcquireDecisionWriteLock()
	if err != nil {
		return fmt.Errorf("lock automatic review decisions: %w", err)
	}
	defer func() { _ = release() }()

	decisions, corrupt, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return fmt.Errorf("load automatic review decisions: %w", err)
	}
	if len(corrupt) != 0 {
		return fmt.Errorf("automatic review history contains %d corrupt record(s)", len(corrupt))
	}
	found := false
	for _, decision := range automaticDecisions(decisions) {
		if decision.Decision == store.LoopDecisionContinue && decision.RunID == economics.RunID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("automatic review run %q was not admitted by this controller", economics.RunID)
	}
	if _, err := c.economics.SaveEconomics(key, recordedAt, economics); err != nil {
		return fmt.Errorf("record automatic review economics: %w", err)
	}
	return nil
}

func (c *Controller) Escalate(key store.PullRequestKeyV1, reason string) (Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	release, err := c.decisions.AcquireDecisionWriteLock()
	if err != nil {
		return Decision{}, fmt.Errorf("lock automatic review decisions: %w", err)
	}
	defer func() { _ = release() }()

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Decision{}, fmt.Errorf("automatic review escalation reason is required")
	}
	decisions, corrupt, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return Decision{}, fmt.Errorf("load automatic review decisions: %w", err)
	}
	if len(corrupt) != 0 {
		return Decision{}, fmt.Errorf("automatic review history contains %d corrupt record(s)", len(corrupt))
	}
	session, sessionDecisions, err := activeSession(automaticDecisions(decisions))
	if err != nil {
		return Decision{}, err
	}
	budget, _, err := c.currentBudget(key, session, sessionDecisions)
	if err != nil {
		return Decision{}, err
	}
	return c.recordDecision(key, session, store.LoopDecisionEscalate, reason, "", budget, false)
}

func (c *Controller) admit(key store.PullRequestKeyV1, policy TrustedPolicy, authorization Authorization, reason string) (Decision, error) {
	if authorization.kind != AuthorizationUser && authorization.kind != AuthorizationWorkspace {
		return Decision{}, fmt.Errorf("automatic review requires explicit user or trusted workspace authorization")
	}
	if strings.TrimSpace(authorization.actor) == "" {
		return Decision{}, fmt.Errorf("automatic review authorization actor is required")
	}
	sessionID, err := c.newID()
	if err != nil {
		return Decision{}, fmt.Errorf("create automatic review session id: %w", err)
	}
	startedAt := c.now().UTC()
	decidedAt, err := c.nextDecisionTime(key)
	if err != nil {
		return Decision{}, err
	}
	budget := store.BudgetStateV1{
		Known:           true,
		IterationsLimit: policy.policy.Budget.MaxIterations,
		StartedAt:       startedAt,
		DurationLimit:   policy.policy.Budget.MaxDuration,
		CostKnown:       true,
		CostUSDLimit:    policy.policy.Budget.MaxCostUSD,
	}
	id, err := c.newID()
	if err != nil {
		return Decision{}, fmt.Errorf("create automatic review decision id: %w", err)
	}
	source := policy.policy.Source
	target := policy.target
	record := store.LoopDecisionV1{
		SchemaVersion:     store.CurrentSchemaVersion,
		ID:                id,
		PullRequest:       key,
		SessionID:         sessionID,
		AuthorizationKind: string(authorization.kind),
		AuthorizedBy:      authorization.actor,
		PolicySource:      &source,
		ReviewTarget:      &target,
		Scope:             store.LoopDecisionScopeAutomaticExecution,
		Decision:          store.LoopDecisionAdmit,
		Reason:            fmt.Sprintf("%s by %s %q", reason, authorization.kind, authorization.actor),
		Budget:            budget,
		DecidedAt:         decidedAt,
	}
	if _, err := c.decisions.SaveLoopDecision(record); err != nil {
		return Decision{}, fmt.Errorf("record automatic review admission: %w", err)
	}
	return decisionFromRecord(record, false), nil
}

func (c *Controller) currentBudget(key store.PullRequestKeyV1, admission store.LoopDecisionV1, decisions []store.LoopDecisionV1) (store.BudgetStateV1, bool, error) {
	budget := budgetFromAdmission(admission, c.now())
	runIDs := make(map[string]struct{})
	for _, decision := range decisions {
		if decision.Budget.Elapsed > budget.Elapsed {
			budget.Elapsed = decision.Budget.Elapsed
		}
		if decision.Decision == store.LoopDecisionContinue {
			budget.IterationsUsed++
			runIDs[decision.RunID] = struct{}{}
		}
	}
	if budget.CostUSDLimit == 0 {
		return budget, true, nil
	}
	if len(runIDs) == 0 {
		budget.CostKnown = true
		return budget, true, nil
	}
	records, _, err := c.economics.ListEconomics(key)
	if err != nil {
		return store.BudgetStateV1{}, false, fmt.Errorf("load automatic review economics: %w", err)
	}
	byRun := make(map[string]store.ReviewEconomicsV1, len(records))
	for _, record := range records {
		byRun[record.Economics.RunID] = record.Economics
	}
	for runID := range runIDs {
		economics, ok := byRun[runID]
		if !ok || len(economics.ProviderUsage) == 0 {
			budget.CostKnown = false
			budget.CostUSDUsed = 0
			return budget, false, nil
		}
		for _, usage := range economics.ProviderUsage {
			if !usage.Usage.Known {
				budget.CostKnown = false
				budget.CostUSDUsed = 0
				return budget, false, nil
			}
			budget.CostUSDUsed += usage.Usage.CostUSD
		}
	}
	budget.CostKnown = true
	return budget, true, nil
}

func budgetFromAdmission(admission store.LoopDecisionV1, now time.Time) store.BudgetStateV1 {
	budget := admission.Budget
	budget.IterationsUsed = 0
	budget.Elapsed = now.UTC().Sub(budget.StartedAt)
	if budget.Elapsed < 0 {
		budget.Elapsed = 0
	}
	budget.CostUSDUsed = 0
	return budget
}

func activeSession(decisions []store.LoopDecisionV1) (store.LoopDecisionV1, []store.LoopDecisionV1, error) {
	for i := len(decisions) - 1; i >= 0; i-- {
		if decisions[i].Decision == store.LoopDecisionAdmit {
			if decisions[i].SessionID == "" {
				return store.LoopDecisionV1{}, nil, fmt.Errorf("automatic review admission has no session id")
			}
			return decisions[i], decisions[i:], nil
		}
	}
	return store.LoopDecisionV1{}, nil, fmt.Errorf("automatic review has no durable trusted admission")
}

func (c *Controller) recordDecision(key store.PullRequestKeyV1, admission store.LoopDecisionV1, kind store.LoopDecisionKindV1, reason, runID string, budget store.BudgetStateV1, allowed bool) (Decision, error) {
	id, err := c.newID()
	if err != nil {
		return Decision{}, fmt.Errorf("create automatic review decision id: %w", err)
	}
	decidedAt, err := c.nextDecisionTime(key)
	if err != nil {
		return Decision{}, err
	}
	record := store.LoopDecisionV1{
		SchemaVersion:  store.CurrentSchemaVersion,
		ID:             id,
		PullRequest:    key,
		RunID:          runID,
		SessionID:      admission.SessionID,
		Scope:          store.LoopDecisionScopeAutomaticExecution,
		Decision:       kind,
		Reason:         reason,
		IterationCount: budget.IterationsUsed,
		Budget:         budget,
		DecidedAt:      decidedAt,
	}
	if _, err := c.decisions.SaveLoopDecision(record); err != nil {
		return Decision{}, fmt.Errorf("record automatic review decision: %w", err)
	}
	return decisionFromRecord(record, allowed), nil
}

func (c *Controller) nextDecisionTime(key store.PullRequestKeyV1) (time.Time, error) {
	now := c.now().UTC()
	decisions, _, err := c.decisions.ListLoopDecisions(key)
	if err != nil {
		return time.Time{}, fmt.Errorf("load automatic review decisions: %w", err)
	}
	for _, decision := range decisions {
		if !now.After(decision.DecidedAt) {
			now = decision.DecidedAt.Add(time.Nanosecond)
		}
	}
	return now, nil
}

func decisionFromRecord(record store.LoopDecisionV1, allowed bool) Decision {
	decision := Decision{Kind: record.Decision, Reason: record.Reason, Budget: record.Budget, Allowed: allowed}
	if record.Budget.DurationLimit > 0 {
		decision.Deadline = record.Budget.StartedAt.Add(record.Budget.DurationLimit)
	}
	return decision
}

func automaticDecisions(decisions []store.LoopDecisionV1) []store.LoopDecisionV1 {
	result := make([]store.LoopDecisionV1, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Scope == store.LoopDecisionScopeAutomaticExecution {
			result = append(result, decision)
		}
	}
	return result
}

func validateTrustedTarget(key store.PullRequestKeyV1, target store.ReviewTargetV1, policy TrustedPolicy) error {
	if !reflect.DeepEqual(target, policy.target) {
		return fmt.Errorf("automatic review policy was not validated for the requested review target")
	}
	if target.PullRequest == nil {
		return fmt.Errorf("automatic review target must identify the requested pull request")
	}
	if *target.PullRequest != key {
		return fmt.Errorf("automatic review target %s does not match requested pull request %s", target.PullRequest.String(), key.String())
	}
	return nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
