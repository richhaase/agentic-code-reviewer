package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
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
