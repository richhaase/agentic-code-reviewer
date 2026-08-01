package store

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestLoopDecisionV1_RoundTripAllKinds(t *testing.T) {
	kinds := []LoopDecisionKindV1{LoopDecisionContinue, LoopDecisionStop, LoopDecisionEscalate}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			decision := LoopDecisionV1{
				SchemaVersion:  CurrentSchemaVersion,
				ID:             "loop-decision-1",
				PullRequest:    testPullRequestKey(),
				RunID:          "run-1",
				Decision:       kind,
				Reason:         "clean run",
				IterationCount: 2,
				Budget: BudgetStateV1{
					Known:           true,
					IterationsUsed:  2,
					IterationsLimit: 5,
					CostKnown:       true,
					CostUSDUsed:     1.5,
					CostUSDLimit:    10,
				},
				SupportingAdjudicationIDs: []string{"adjudication-1", "adjudication-2"},
				DecidedAt:                 time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
			}

			if err := decision.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			data, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded LoopDecisionV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(decoded, decision) {
				t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, decision)
			}
		})
	}
}

func TestLoopDecisionV1_AdmissionRequiresAuthorizationAndPolicy(t *testing.T) {
	key := testPullRequestKey()
	decision := LoopDecisionV1{
		SchemaVersion:     CurrentSchemaVersion,
		ID:                "admission-1",
		PullRequest:       key,
		SessionID:         "session-1",
		AuthorizationKind: "user",
		AuthorizedBy:      "alice",
		PolicySource:      &PolicySourceV1{Kind: config.SourceKindDefaults},
		ReviewTarget:      &ReviewTargetV1{PullRequest: &key},
		Scope:             LoopDecisionScopeAutomaticExecution,
		Decision:          LoopDecisionAdmit,
		Reason:            "commissioned",
		Budget:            BudgetStateV1{Known: true, IterationsLimit: 1},
		DecidedAt:         time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	decision.AuthorizedBy = ""
	if err := decision.Validate(); err == nil {
		t.Fatal("expected admission without a trusted actor to fail validation")
	}
}

func TestLoopDecisionV1_AdmissionRequiresBoundedBudget(t *testing.T) {
	key := testPullRequestKey()
	decision := LoopDecisionV1{
		SchemaVersion:     CurrentSchemaVersion,
		ID:                "admission-unbounded",
		PullRequest:       key,
		SessionID:         "session-unbounded",
		AuthorizationKind: "user",
		AuthorizedBy:      "alice",
		PolicySource:      &PolicySourceV1{Kind: config.SourceKindDefaults},
		ReviewTarget:      &ReviewTargetV1{PullRequest: &key},
		Scope:             LoopDecisionScopeAutomaticExecution,
		Decision:          LoopDecisionAdmit,
		Reason:            "commissioned",
		Budget:            BudgetStateV1{Known: true},
		DecidedAt:         time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}
	if err := decision.Validate(); err == nil {
		t.Fatal("expected known admission without review or duration bound to fail")
	}
	decision.Budget = BudgetStateV1{}
	if err := decision.Validate(); err == nil {
		t.Fatal("expected admission with unknown budget to fail")
	}
}

func TestLoopDecisionV1_AcknowledgedCorruptFilesRequireSafeAdmissionNames(t *testing.T) {
	key := testPullRequestKey()
	decision := LoopDecisionV1{
		SchemaVersion:            CurrentSchemaVersion,
		ID:                       "admission-corrupt-history",
		PullRequest:              key,
		SessionID:                "session-corrupt-history",
		AuthorizationKind:        "user",
		AuthorizedBy:             "alice",
		PolicySource:             &PolicySourceV1{Kind: config.SourceKindDefaults},
		ReviewTarget:             &ReviewTargetV1{PullRequest: &key},
		AcknowledgedCorruptFiles: []string{"malformed.json"},
		Scope:                    LoopDecisionScopeAutomaticExecution,
		Decision:                 LoopDecisionAdmit,
		Reason:                   "trusted recovery",
		Budget:                   BudgetStateV1{Known: true, IterationsLimit: 1},
		DecidedAt:                time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	unsafe := decision
	unsafe.AcknowledgedCorruptFiles = []string{"../malformed.json"}
	if err := unsafe.Validate(); err == nil {
		t.Fatal("expected unsafe acknowledged filename to fail validation")
	}
	nonAdmission := decision
	nonAdmission.Decision = LoopDecisionStop
	nonAdmission.AuthorizationKind = ""
	nonAdmission.AuthorizedBy = ""
	nonAdmission.PolicySource = nil
	nonAdmission.ReviewTarget = nil
	if err := nonAdmission.Validate(); err == nil {
		t.Fatal("expected non-admission acknowledgment to fail validation")
	}
}

func TestLoopDecisionV1_AdmissionRequiresTrustedTargetSourceBinding(t *testing.T) {
	key := testPullRequestKey()
	target := ReviewTargetV1{
		Revision:    RevisionEvidenceV1{HeadObjectID: "reviewed-head"},
		PullRequest: &key,
	}
	valid := func() LoopDecisionV1 {
		return LoopDecisionV1{
			SchemaVersion:     CurrentSchemaVersion,
			ID:                "admission-target",
			PullRequest:       key,
			SessionID:         "session-target",
			AuthorizationKind: "user",
			AuthorizedBy:      "alice",
			PolicySource:      &PolicySourceV1{Kind: config.SourceKindDefaults},
			ReviewTarget:      &target,
			Scope:             LoopDecisionScopeAutomaticExecution,
			Decision:          LoopDecisionAdmit,
			Reason:            "commissioned",
			Budget:            BudgetStateV1{Known: true, IterationsLimit: 1},
			DecidedAt:         time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
		}
	}

	missingTarget := valid()
	missingTarget.ReviewTarget = nil
	if err := missingTarget.Validate(); err == nil {
		t.Fatal("expected admission without target to fail")
	}
	mismatched := valid()
	otherKey := key
	otherKey.Number++
	mismatched.ReviewTarget = &ReviewTargetV1{PullRequest: &otherKey}
	if err := mismatched.Validate(); err == nil {
		t.Fatal("expected admission with mismatched target to fail")
	}
	reviewedSource := valid()
	reviewedSource.PolicySource = &PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: "reviewed-head"}
	if err := reviewedSource.Validate(); err == nil {
		t.Fatal("expected admission sourced from reviewed head to fail")
	}
	duration := valid()
	duration.Budget.IterationsLimit = 0
	duration.Budget.DurationLimit = time.Hour
	if err := duration.Validate(); err == nil {
		t.Fatal("expected duration admission without start time to fail")
	}
	duration.Budget.StartedAt = duration.DecidedAt.Add(time.Minute)
	if err := duration.Validate(); err == nil {
		t.Fatal("expected duration admission starting after its decision to fail")
	}
}

func TestBudgetStateV1_KnownZeroDistinguishableFromUnknown(t *testing.T) {
	known := BudgetStateV1{Known: true}
	unknown := BudgetStateV1{Known: false}

	if err := known.Validate(); err != nil {
		t.Fatalf("Validate(known zero budget): %v", err)
	}
	if err := unknown.Validate(); err != nil {
		t.Fatalf("Validate(unknown budget): %v", err)
	}

	knownData, _ := json.Marshal(known)
	unknownData, _ := json.Marshal(unknown)
	if string(knownData) == string(unknownData) {
		t.Fatalf("known-zero and unknown budget state must serialize differently: %s", knownData)
	}
}

func TestBudgetStateV1_ValidateRejectsUnknownWithNonzeroMeasurements(t *testing.T) {
	unknown := BudgetStateV1{Known: false, IterationsUsed: 1}
	if err := unknown.Validate(); err == nil {
		t.Fatal("expected an error for unknown budget state with a nonzero measurement")
	}
}

func TestBudgetStateV1_UnmarshalLegacyKnownCost(t *testing.T) {
	data := []byte(`{"known":true,"cost_usd_used":1.5,"cost_usd_limit":10}`)
	var budget BudgetStateV1
	if err := json.Unmarshal(data, &budget); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !budget.CostKnown || budget.CostUSDUsed != 1.5 {
		t.Fatalf("legacy known cost was not preserved: %+v", budget)
	}
}

func TestBudgetStateV1_ValidateRejectsUnknownCostWithMeasuredUsage(t *testing.T) {
	budget := BudgetStateV1{Known: true, CostKnown: false, CostUSDUsed: 1}
	if err := budget.Validate(); err == nil {
		t.Fatal("expected unknown cost with measured usage to fail validation")
	}
}

func TestLoopDecisionV1_SemanticTerminalRequiresRunID(t *testing.T) {
	decision := LoopDecisionV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "semantic-stop",
		PullRequest:   testPullRequestKey(),
		Scope:         LoopDecisionScopeSemanticConvergence,
		Decision:      LoopDecisionStop,
		Reason:        "converged",
		Budget:        BudgetStateV1{Known: true},
		DecidedAt:     time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}
	if err := decision.Validate(); err == nil {
		t.Fatal("expected semantic terminal decision without run id to fail validation")
	}
	decision.Scope = LoopDecisionScopeAutomaticExecution
	decision.SessionID = "session-1"
	if err := decision.Validate(); err != nil {
		t.Fatalf("automatic terminal decision should not require a run id: %v", err)
	}
}

func TestLoopDecisionV1_AutomaticDecisionRequiresSessionID(t *testing.T) {
	decision := LoopDecisionV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "automatic-stop",
		PullRequest:   testPullRequestKey(),
		Scope:         LoopDecisionScopeAutomaticExecution,
		Decision:      LoopDecisionStop,
		Reason:        "stopped",
		Budget:        BudgetStateV1{Known: true},
		DecidedAt:     time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}
	if err := decision.Validate(); err == nil {
		t.Fatal("expected automatic decision without session id to fail validation")
	}
}

func TestBudgetStateV1_ValidateRejectsNegativeKnownMeasurements(t *testing.T) {
	tests := []struct {
		name   string
		budget BudgetStateV1
	}{
		{name: "negative iterations used", budget: BudgetStateV1{Known: true, IterationsUsed: -1}},
		{name: "negative iterations limit", budget: BudgetStateV1{Known: true, IterationsLimit: -1}},
		{name: "negative cost used", budget: BudgetStateV1{Known: true, CostUSDUsed: -0.01}},
		{name: "negative cost limit", budget: BudgetStateV1{Known: true, CostUSDLimit: -0.01}},
		{name: "NaN cost used", budget: BudgetStateV1{Known: true, CostUSDUsed: math.NaN()}},
		{name: "infinite cost limit", budget: BudgetStateV1{Known: true, CostUSDLimit: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.budget.Validate(); err == nil {
				t.Fatalf("expected an error for known budget state with a negative measurement: %+v", tt.budget)
			}
		})
	}
}

func TestLoopDecisionV1_Validate(t *testing.T) {
	valid := func() LoopDecisionV1 {
		return LoopDecisionV1{
			SchemaVersion: CurrentSchemaVersion,
			ID:            "loop-decision-1",
			PullRequest:   testPullRequestKey(),
			RunID:         "run-1",
			Decision:      LoopDecisionContinue,
			Reason:        "not converged",
			DecidedAt:     time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
		}
	}

	tests := []struct {
		name    string
		mutate  func(d *LoopDecisionV1)
		wantErr bool
	}{
		{name: "valid", mutate: func(d *LoopDecisionV1) {}, wantErr: false},
		{name: "unsupported schema version", mutate: func(d *LoopDecisionV1) { d.SchemaVersion = 99 }, wantErr: true},
		{name: "unknown decision kind", mutate: func(d *LoopDecisionV1) { d.Decision = "retry" }, wantErr: true},
		{name: "missing reason", mutate: func(d *LoopDecisionV1) { d.Reason = "" }, wantErr: true},
		{name: "negative iteration count", mutate: func(d *LoopDecisionV1) { d.IterationCount = -1 }, wantErr: true},
		{name: "zero decided_at", mutate: func(d *LoopDecisionV1) { d.DecidedAt = time.Time{} }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := valid()
			tt.mutate(&decision)
			err := decision.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
