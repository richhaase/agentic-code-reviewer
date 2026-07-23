package store

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestAdjudicationPolicyV1_RoundTrip(t *testing.T) {
	policy := AdjudicationPolicyV1{
		SchemaVersion: CurrentSchemaVersion,
		Source: PolicySourceV1{
			Kind:          config.SourceKindRepositoryRevision,
			Locator:       "/repo",
			Ref:           "refs/acr/trusted-config/origin/main",
			Revision:      "trustedsha",
			ConfigPresent: true,
			ConfigDigest:  "digest",
		},
		Budget:             BudgetPolicyV1{MaxIterations: 5, MaxCostUSD: 10},
		Stop:               StopPolicyV1{StopOnCleanRun: true, StopOnNoNewFindings: true},
		EvaluationGuidance: "prefer precision over recall",
	}

	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AdjudicationPolicyV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, policy) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, policy)
	}
}

func TestAdjudicationPolicyV1_Validate(t *testing.T) {
	valid := func() AdjudicationPolicyV1 {
		return AdjudicationPolicyV1{
			SchemaVersion: CurrentSchemaVersion,
			Source:        PolicySourceV1{Kind: config.SourceKindDefaults},
			Budget:        BudgetPolicyV1{MaxIterations: 3, MaxCostUSD: 5},
		}
	}

	tests := []struct {
		name    string
		mutate  func(p *AdjudicationPolicyV1)
		wantErr bool
	}{
		{name: "valid", mutate: func(p *AdjudicationPolicyV1) {}, wantErr: false},
		{name: "unsupported schema version", mutate: func(p *AdjudicationPolicyV1) { p.SchemaVersion = 99 }, wantErr: true},
		{name: "filesystem source rejected", mutate: func(p *AdjudicationPolicyV1) { p.Source = PolicySourceV1{Kind: config.SourceKindFilesystem} }, wantErr: true},
		{name: "empty source kind rejected", mutate: func(p *AdjudicationPolicyV1) { p.Source = PolicySourceV1{} }, wantErr: true},
		{name: "negative max iterations", mutate: func(p *AdjudicationPolicyV1) { p.Budget.MaxIterations = -1 }, wantErr: true},
		{name: "negative max cost", mutate: func(p *AdjudicationPolicyV1) { p.Budget.MaxCostUSD = -1 }, wantErr: true},
		{name: "NaN max cost", mutate: func(p *AdjudicationPolicyV1) { p.Budget.MaxCostUSD = math.NaN() }, wantErr: true},
		{name: "infinite max cost", mutate: func(p *AdjudicationPolicyV1) { p.Budget.MaxCostUSD = math.Inf(1) }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := valid()
			tt.mutate(&policy)
			err := policy.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
