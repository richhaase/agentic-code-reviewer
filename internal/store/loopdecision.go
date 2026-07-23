package store

import (
	"fmt"
	"math"
	"time"
)

type LoopDecisionKindV1 string

const (
	LoopDecisionContinue LoopDecisionKindV1 = "continue"
	LoopDecisionStop     LoopDecisionKindV1 = "stop"
	LoopDecisionEscalate LoopDecisionKindV1 = "escalate"
)

func (k LoopDecisionKindV1) Validate() error {
	switch k {
	case LoopDecisionContinue, LoopDecisionStop, LoopDecisionEscalate:
		return nil
	default:
		return fmt.Errorf("unknown loop decision kind %q", k)
	}
}

type BudgetStateV1 struct {
	Known           bool    `json:"known"`
	IterationsUsed  int     `json:"iterations_used"`
	IterationsLimit int     `json:"iterations_limit"`
	CostUSDUsed     float64 `json:"cost_usd_used"`
	CostUSDLimit    float64 `json:"cost_usd_limit"`
}

func (b BudgetStateV1) Validate() error {
	if !b.Known {
		if b.IterationsUsed != 0 || b.IterationsLimit != 0 || b.CostUSDUsed != 0 || b.CostUSDLimit != 0 {
			return fmt.Errorf("budget state marked unknown must not carry nonzero measurements")
		}
		return nil
	}
	if b.IterationsUsed < 0 || b.IterationsLimit < 0 {
		return fmt.Errorf("known budget iteration counts must not be negative")
	}
	if isInvalidKnownCost(b.CostUSDUsed) || isInvalidKnownCost(b.CostUSDLimit) {
		return fmt.Errorf("known budget cost must be a finite number that is not negative")
	}
	return nil
}

func isInvalidKnownCost(cost float64) bool {
	return cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0)
}

type LoopDecisionV1 struct {
	SchemaVersion             int                `json:"schema_version"`
	ID                        string             `json:"id"`
	PullRequest               PullRequestKeyV1   `json:"pull_request"`
	RunID                     string             `json:"run_id"`
	Decision                  LoopDecisionKindV1 `json:"decision"`
	Reason                    string             `json:"reason"`
	IterationCount            int                `json:"iteration_count"`
	Budget                    BudgetStateV1      `json:"budget"`
	SupportingAdjudicationIDs []string           `json:"supporting_adjudication_ids,omitempty"`
	DecidedAt                 time.Time          `json:"decided_at"`
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
	if err := validateNonEmpty("loop decision run_id", d.RunID); err != nil {
		return err
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
	return nil
}
