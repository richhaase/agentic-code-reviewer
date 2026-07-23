package store

import "fmt"

// BudgetPolicyV1 bounds how much a review convergence loop may spend before
// it must stop or escalate.
type BudgetPolicyV1 struct {
	MaxIterations int     `json:"max_iterations"`
	MaxCostUSD    float64 `json:"max_cost_usd"`
}

func (p BudgetPolicyV1) Validate() error {
	if p.MaxIterations < 0 {
		return fmt.Errorf("budget policy max_iterations must not be negative")
	}
	if p.MaxCostUSD < 0 {
		return fmt.Errorf("budget policy max_cost_usd must not be negative")
	}
	return nil
}

// StopPolicyV1 defines when the convergence loop is allowed to stop.
type StopPolicyV1 struct {
	StopOnCleanRun      bool `json:"stop_on_clean_run"`
	StopOnNoNewFindings bool `json:"stop_on_no_new_findings"`
}

// AdjudicationPolicyV1 is the versioned control-plane record carrying budget
// policy, stop policy, and evaluation guidance used by the review
// convergence loop (issue #223). Source records where this policy was
// resolved from, reusing config.SourceIdentity, the exact trust mechanism
// issue #220 established, rather than inventing a second trust boundary.
// ValidatePolicySourceOutsideReview must be used together with Validate to
// confirm Source does not resolve to the head of the pull request under
// review.
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
