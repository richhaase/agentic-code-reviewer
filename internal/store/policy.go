package store

import (
	"fmt"
	"math"
)

type BudgetPolicyV1 struct {
	MaxIterations int     `json:"max_iterations"`
	MaxCostUSD    float64 `json:"max_cost_usd"`
}

func (p BudgetPolicyV1) Validate() error {
	if p.MaxIterations < 0 {
		return fmt.Errorf("budget policy max_iterations must not be negative")
	}
	if p.MaxCostUSD < 0 || math.IsNaN(p.MaxCostUSD) || math.IsInf(p.MaxCostUSD, 0) {
		return fmt.Errorf("budget policy max_cost_usd must be a finite number that is not negative")
	}
	return nil
}

type StopPolicyV1 struct {
	StopOnCleanRun      bool `json:"stop_on_clean_run"`
	StopOnNoNewFindings bool `json:"stop_on_no_new_findings"`
}

type AdjudicationPolicyV1 struct {
	SchemaVersion      int            `json:"schema_version"`
	Source             PolicySourceV1 `json:"source"`
	Budget             BudgetPolicyV1 `json:"budget"`
	Stop               StopPolicyV1   `json:"stop"`
	EvaluationGuidance string         `json:"evaluation_guidance"`
}

func (p AdjudicationPolicyV1) Validate() error {
	if err := validateSchemaVersion("adjudication policy", p.SchemaVersion); err != nil {
		return err
	}
	if err := p.Source.Validate(); err != nil {
		return err
	}
	return p.Budget.Validate()
}
