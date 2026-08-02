package store

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type LoopDecisionKindV1 string

type LoopDecisionScopeV1 string

const (
	LoopDecisionScopeAutomaticExecution  LoopDecisionScopeV1 = "automatic_execution"
	LoopDecisionScopeSemanticConvergence LoopDecisionScopeV1 = "semantic_convergence"
)

func (s LoopDecisionScopeV1) Validate() error {
	switch s {
	case "", LoopDecisionScopeAutomaticExecution, LoopDecisionScopeSemanticConvergence:
		return nil
	default:
		return fmt.Errorf("unknown loop decision scope %q", s)
	}
}

const (
	LoopDecisionAdmit    LoopDecisionKindV1 = "admit"
	LoopDecisionContinue LoopDecisionKindV1 = "continue"
	LoopDecisionStop     LoopDecisionKindV1 = "stop"
	LoopDecisionEscalate LoopDecisionKindV1 = "escalate"
)

func (k LoopDecisionKindV1) Validate() error {
	switch k {
	case LoopDecisionAdmit, LoopDecisionContinue, LoopDecisionStop, LoopDecisionEscalate:
		return nil
	default:
		return fmt.Errorf("unknown loop decision kind %q", k)
	}
}

type BudgetStateV1 struct {
	Known           bool          `json:"known"`
	IterationsUsed  int           `json:"iterations_used"`
	IterationsLimit int           `json:"iterations_limit"`
	StartedAt       time.Time     `json:"started_at,omitempty"`
	Elapsed         time.Duration `json:"elapsed"`
	DurationLimit   time.Duration `json:"duration_limit"`
	CostKnown       bool          `json:"cost_known"`
	CostUSDUsed     float64       `json:"cost_usd_used"`
	CostUSDLimit    float64       `json:"cost_usd_limit"`
}

func (b *BudgetStateV1) UnmarshalJSON(data []byte) error {
	type budgetState BudgetStateV1
	decoded := struct {
		budgetState
		CostKnown *bool `json:"cost_known"`
	}{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*b = BudgetStateV1(decoded.budgetState)
	if decoded.CostKnown == nil {
		b.CostKnown = b.Known
	} else {
		b.CostKnown = *decoded.CostKnown
	}
	return nil
}

func (b BudgetStateV1) Validate() error {
	if !b.Known {
		if b.IterationsUsed != 0 || b.IterationsLimit != 0 || !b.StartedAt.IsZero() || b.Elapsed != 0 || b.DurationLimit != 0 || b.CostKnown || b.CostUSDUsed != 0 || b.CostUSDLimit != 0 {
			return fmt.Errorf("budget state marked unknown must not carry nonzero measurements")
		}
		return nil
	}
	if b.IterationsUsed < 0 || b.IterationsLimit < 0 {
		return fmt.Errorf("known budget iteration counts must not be negative")
	}
	if b.Elapsed < 0 || b.DurationLimit < 0 {
		return fmt.Errorf("known budget durations must not be negative")
	}
	if isInvalidKnownCost(b.CostUSDUsed) || isInvalidKnownCost(b.CostUSDLimit) {
		return fmt.Errorf("known budget cost must be a finite number that is not negative")
	}
	if !b.CostKnown && b.CostUSDUsed != 0 {
		return fmt.Errorf("budget with unknown cost must not carry measured cost usage")
	}
	return nil
}

func isInvalidKnownCost(cost float64) bool {
	return cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0)
}

type LoopDecisionV1 struct {
	SchemaVersion              int                             `json:"schema_version"`
	ID                         string                          `json:"id"`
	PullRequest                PullRequestKeyV1                `json:"pull_request"`
	RunID                      string                          `json:"run_id,omitempty"`
	SessionID                  string                          `json:"session_id,omitempty"`
	AuthorizationKind          string                          `json:"authorization_kind,omitempty"`
	AuthorizedBy               string                          `json:"authorized_by,omitempty"`
	PolicySource               *PolicySourceV1                 `json:"policy_source,omitempty"`
	ReviewTarget               *ReviewTargetV1                 `json:"review_target,omitempty"`
	AcknowledgedCorruptRecords []CorruptRecordAcknowledgmentV1 `json:"acknowledged_corrupt_records,omitempty"`
	Scope                      LoopDecisionScopeV1             `json:"scope,omitempty"`
	Decision                   LoopDecisionKindV1              `json:"decision"`
	Reason                     string                          `json:"reason"`
	IterationCount             int                             `json:"iteration_count"`
	Budget                     BudgetStateV1                   `json:"budget"`
	SupportingAdjudicationIDs  []string                        `json:"supporting_adjudication_ids,omitempty"`
	DecidedAt                  time.Time                       `json:"decided_at"`
}

type CorruptRecordAcknowledgmentV1 struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

func (d LoopDecisionV1) Validate() error {
	if err := validateSchemaVersion("loop decision", d.SchemaVersion); err != nil {
		return err
	}
	if err := validateNonEmpty("loop decision id", d.ID); err != nil {
		return err
	}
	if err := d.PullRequest.Validate(); err != nil {
		return err
	}
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	if d.Scope == LoopDecisionScopeAutomaticExecution {
		if err := validateNonEmpty("loop decision session_id", d.SessionID); err != nil {
			return err
		}
	}
	if d.Decision == LoopDecisionContinue || (d.Scope != LoopDecisionScopeAutomaticExecution && (d.Decision == LoopDecisionStop || d.Decision == LoopDecisionEscalate)) {
		if err := validateNonEmpty("loop decision run_id", d.RunID); err != nil {
			return err
		}
	}
	if d.Decision == LoopDecisionAdmit {
		if d.Scope != LoopDecisionScopeAutomaticExecution {
			return fmt.Errorf("loop decision admission requires automatic_execution scope")
		}
		if err := validateNonEmpty("loop decision session_id", d.SessionID); err != nil {
			return err
		}
		if err := validateNonEmpty("loop decision authorization_kind", d.AuthorizationKind); err != nil {
			return err
		}
		if d.AuthorizationKind != "user" && d.AuthorizationKind != "workspace_controller" {
			return fmt.Errorf("loop decision authorization_kind %q is not trusted", d.AuthorizationKind)
		}
		if err := validateNonEmpty("loop decision authorized_by", d.AuthorizedBy); err != nil {
			return err
		}
		if d.PolicySource == nil {
			return fmt.Errorf("loop decision policy_source is required for admission")
		}
		if err := d.PolicySource.Validate(); err != nil {
			return err
		}
		if d.ReviewTarget == nil || d.ReviewTarget.PullRequest == nil {
			return fmt.Errorf("loop decision admission requires a pull-request review target")
		}
		if err := d.ReviewTarget.Validate(); err != nil {
			return fmt.Errorf("loop decision admission review target: %w", err)
		}
		if *d.ReviewTarget.PullRequest != d.PullRequest {
			return fmt.Errorf("loop decision admission review target does not match pull request")
		}
		if err := ValidatePolicySourceOutsideReview(*d.PolicySource, *d.ReviewTarget); err != nil {
			return fmt.Errorf("loop decision admission policy source: %w", err)
		}
		if !d.Budget.Known || (d.Budget.IterationsLimit == 0 && d.Budget.DurationLimit == 0) {
			return fmt.Errorf("loop decision admission requires a known review or duration bound")
		}
		for _, acknowledgment := range d.AcknowledgedCorruptRecords {
			if err := validateRecordID("acknowledged corrupt decision file", acknowledgment.Name); err != nil {
				return err
			}
			if err := validateNonEmpty("acknowledged corrupt decision fingerprint", acknowledgment.Fingerprint); err != nil {
				return err
			}
		}
	} else if len(d.AcknowledgedCorruptRecords) != 0 {
		return fmt.Errorf("acknowledged corrupt decision files require an admission decision")
	}
	if err := d.Decision.Validate(); err != nil {
		return err
	}
	if err := validateNonEmpty("loop decision reason", d.Reason); err != nil {
		return err
	}
	if d.IterationCount < 0 {
		return fmt.Errorf("loop decision iteration_count must not be negative")
	}
	if err := d.Budget.Validate(); err != nil {
		return err
	}
	if d.DecidedAt.IsZero() {
		return fmt.Errorf("loop decision decided_at is required")
	}
	if d.Decision == LoopDecisionAdmit && d.Budget.DurationLimit > 0 {
		if d.Budget.StartedAt.IsZero() {
			return fmt.Errorf("duration-bounded loop decision admission requires a start time")
		}
		if d.Budget.StartedAt.After(d.DecidedAt) {
			return fmt.Errorf("loop decision admission budget cannot start after the decision")
		}
	}
	return nil
}
