package store

import (
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

// ResolveAdjudicationPolicy loads adjudication memory's budget policy, stop
// policy, and evaluation guidance from source, the same config.Source
// abstraction issue #220 established for review configuration, and returns
// the resulting AdjudicationPolicyV1 along with any non-fatal warnings from
// loading it. It rejects a source whose resolved kind is untrusted or whose
// resolved revision is the pull request head under review, via
// PolicySourceV1.Validate and ValidatePolicySourceOutsideReview, so a
// reviewed pull request's own head or worktree can never supply or alter
// the returned policy.
func ResolveAdjudicationPolicy(ctx context.Context, source config.Source, target ReviewTargetV1) (AdjudicationPolicyV1, []string, error) {
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		return AdjudicationPolicyV1{}, nil, fmt.Errorf("resolve adjudication policy: %w", err)
	}

	policySource := ToPolicySourceSchema(result.Source)
	if err := ValidatePolicySourceOutsideReview(policySource, target); err != nil {
		return AdjudicationPolicyV1{}, nil, err
	}

	policy := AdjudicationPolicyV1{
		SchemaVersion: CurrentSchemaVersion,
		Source:        policySource,
	}
	if result.Config != nil {
		adjudication := result.Config.Adjudication
		if adjudication.MaxIterations != nil {
			policy.Budget.MaxIterations = *adjudication.MaxIterations
		}
		if adjudication.MaxCostUSD != nil {
			policy.Budget.MaxCostUSD = *adjudication.MaxCostUSD
		}
		if adjudication.StopOnCleanRun != nil {
			policy.Stop.StopOnCleanRun = *adjudication.StopOnCleanRun
		}
		if adjudication.StopOnNoNewFindings != nil {
			policy.Stop.StopOnNoNewFindings = *adjudication.StopOnNoNewFindings
		}
		if adjudication.EvaluationGuidance != nil {
			policy.EvaluationGuidance = *adjudication.EvaluationGuidance
		}
	}

	if err := policy.Validate(); err != nil {
		return AdjudicationPolicyV1{}, nil, fmt.Errorf("resolve adjudication policy: %w", err)
	}
	return policy, result.Warnings, nil
}
