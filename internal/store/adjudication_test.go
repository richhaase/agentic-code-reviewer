package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func validAdjudicationRecord() AdjudicationRecordV1 {
	return AdjudicationRecordV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "adjudication-1",
		FindingRef:    AdjudicationFindingRefV1{FindingID: "finding-1"},
		Disposition:   AdjudicationAcceptedRisk,
		DecidingActor: AdjudicationActorV1{Kind: AdjudicationActorHuman, Identity: "richhaase"},
		Rationale:     "known limitation, tracked separately",
		Evidence:      []string{"see issue #42"},
		Scope: AdjudicationScopeV1{
			PullRequest:              testPullRequestKey(),
			HeadObjectID:             "headsha",
			ConfigurationFingerprint: "sha256:abc",
		},
		InvalidationConditions: []string{"invalidate if the flagged function signature changes"},
		RecordedAt:             time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
	}
}

func TestAdjudicationRecordV1_RoundTripAllDispositions(t *testing.T) {
	dispositions := []AdjudicationDispositionV1{
		AdjudicationFixed,
		AdjudicationFalsePositive,
		AdjudicationDuplicate,
		AdjudicationAcceptedRisk,
		AdjudicationPolicyDecision,
		AdjudicationDeferred,
		AdjudicationObsolete,
	}

	for _, disposition := range dispositions {
		t.Run(string(disposition), func(t *testing.T) {
			record := validAdjudicationRecord()
			record.Disposition = disposition

			if err := record.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			data, err := json.Marshal(record)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded AdjudicationRecordV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(decoded, record) {
				t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, record)
			}
		})
	}
}

func TestAdjudicationRecordV1_ReopenCorrectSupersedeChainPreservesOriginal(t *testing.T) {
	original := validAdjudicationRecord()
	original.ID = "adjudication-1"

	reopened := validAdjudicationRecord()
	reopened.ID = "adjudication-2"
	reopened.Disposition = AdjudicationDeferred
	reopened.RelationToPrior = AdjudicationRelationReopened
	reopened.SupersedesRecordID = original.ID

	corrected := validAdjudicationRecord()
	corrected.ID = "adjudication-3"
	corrected.Disposition = AdjudicationFalsePositive
	corrected.RelationToPrior = AdjudicationRelationCorrected
	corrected.SupersedesRecordID = reopened.ID

	superseded := validAdjudicationRecord()
	superseded.ID = "adjudication-4"
	superseded.Disposition = AdjudicationFixed
	superseded.RelationToPrior = AdjudicationRelationSuperseded
	superseded.SupersedesRecordID = corrected.ID

	chain := []AdjudicationRecordV1{original, reopened, corrected, superseded}
	for _, record := range chain {
		if err := record.Validate(); err != nil {
			t.Fatalf("Validate(%s): %v", record.ID, err)
		}
	}

	if original.RelationToPrior != AdjudicationRelationNone || original.SupersedesRecordID != "" {
		t.Fatalf("the original decision must remain untouched by the chain built on top of it: %+v", original)
	}
	if reopened.SupersedesRecordID != original.ID {
		t.Fatalf("reopened record must reference the original decision it acts on")
	}
	if corrected.SupersedesRecordID != reopened.ID {
		t.Fatalf("corrected record must reference the decision it acts on")
	}
	if superseded.SupersedesRecordID != corrected.ID {
		t.Fatalf("superseded record must reference the decision it acts on")
	}
}

func TestAdjudicationRecordV1_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(r *AdjudicationRecordV1)
		wantErr bool
	}{
		{name: "valid record", mutate: func(r *AdjudicationRecordV1) {}, wantErr: false},
		{name: "unsupported schema version", mutate: func(r *AdjudicationRecordV1) { r.SchemaVersion = 99 }, wantErr: true},
		{name: "missing finding and cluster reference", mutate: func(r *AdjudicationRecordV1) { r.FindingRef = AdjudicationFindingRefV1{} }, wantErr: true},
		{name: "cluster reference is sufficient", mutate: func(r *AdjudicationRecordV1) { r.FindingRef = AdjudicationFindingRefV1{ClusterID: "cluster-1"} }, wantErr: false},
		{name: "unknown disposition", mutate: func(r *AdjudicationRecordV1) { r.Disposition = "wontfix" }, wantErr: true},
		{name: "missing deciding actor identity", mutate: func(r *AdjudicationRecordV1) { r.DecidingActor.Identity = "" }, wantErr: true},
		{name: "missing scope configuration fingerprint", mutate: func(r *AdjudicationRecordV1) { r.Scope.ConfigurationFingerprint = "" }, wantErr: true},
		{name: "zero recorded_at", mutate: func(r *AdjudicationRecordV1) { r.RecordedAt = time.Time{} }, wantErr: true},
		{
			name: "relation without supersedes id",
			mutate: func(r *AdjudicationRecordV1) {
				r.RelationToPrior = AdjudicationRelationReopened
				r.SupersedesRecordID = ""
			},
			wantErr: true,
		},
		{
			name: "supersedes id without relation",
			mutate: func(r *AdjudicationRecordV1) {
				r.RelationToPrior = AdjudicationRelationNone
				r.SupersedesRecordID = "adjudication-0"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validAdjudicationRecord()
			tt.mutate(&record)
			err := record.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestResolveFindingAdjudication_ExactRepeatReturnsPriorDecisionWithoutBecomingNewlyActionable(t *testing.T) {
	record := validAdjudicationRecord()

	found, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{record}, record.FindingRef, record.Scope)
	if !ok {
		t.Fatal("expected an exact repeat to resolve a prior adjudication")
	}
	if found.ID != record.ID || found.Disposition != record.Disposition {
		t.Fatalf("expected the prior decision to be returned unchanged, got %+v", found)
	}
}

func TestResolveFindingAdjudication_ReturnsMostRecentInSupersessionChain(t *testing.T) {
	original := validAdjudicationRecord()
	original.ID = "adjudication-1"

	corrected := validAdjudicationRecord()
	corrected.ID = "adjudication-2"
	corrected.Disposition = AdjudicationFalsePositive
	corrected.RelationToPrior = AdjudicationRelationCorrected
	corrected.SupersedesRecordID = original.ID
	corrected.RecordedAt = original.RecordedAt.Add(time.Hour)

	found, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{original, corrected}, original.FindingRef, original.Scope)
	if !ok {
		t.Fatal("expected a match")
	}
	if found.ID != corrected.ID {
		t.Fatalf("expected the most recent record in the chain, got %+v", found)
	}
}

func TestResolveFindingAdjudication_SelectionIsIndependentOfSliceOrder(t *testing.T) {
	original := validAdjudicationRecord()
	original.ID = "adjudication-1"

	corrected := validAdjudicationRecord()
	corrected.ID = "adjudication-2"
	corrected.Disposition = AdjudicationFalsePositive
	corrected.RelationToPrior = AdjudicationRelationCorrected
	corrected.SupersedesRecordID = original.ID
	corrected.RecordedAt = original.RecordedAt.Add(time.Hour)

	forward, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{original, corrected}, original.FindingRef, original.Scope)
	if !ok || forward.ID != corrected.ID {
		t.Fatalf("expected the correction regardless of order, got %+v (ok=%v)", forward, ok)
	}

	reversed, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{corrected, original}, original.FindingRef, original.Scope)
	if !ok || reversed.ID != corrected.ID {
		t.Fatalf("expected the correction even when it sorts before the original in the input slice, got %+v (ok=%v)", reversed, ok)
	}
}

func TestResolveFindingAdjudication_TiedTimestampPrefersSupersedingRecord(t *testing.T) {
	original := validAdjudicationRecord()
	original.ID = "adjudication-1"

	corrected := validAdjudicationRecord()
	corrected.ID = "adjudication-2"
	corrected.Disposition = AdjudicationFalsePositive
	corrected.RelationToPrior = AdjudicationRelationCorrected
	corrected.SupersedesRecordID = original.ID

	found, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{corrected, original}, original.FindingRef, original.Scope)
	if !ok || found.ID != corrected.ID {
		t.Fatalf("expected the record that supersedes the other to win a recorded_at tie, got %+v (ok=%v)", found, ok)
	}
}

func TestResolveFindingAdjudication_UncertaintyStaysVisibleAcrossScopeChanges(t *testing.T) {
	record := validAdjudicationRecord()

	tests := []struct {
		name  string
		scope AdjudicationScopeV1
	}{
		{
			name: "different head object id",
			scope: AdjudicationScopeV1{
				PullRequest:              record.Scope.PullRequest,
				HeadObjectID:             "different-head",
				ConfigurationFingerprint: record.Scope.ConfigurationFingerprint,
			},
		},
		{
			name: "different configuration fingerprint",
			scope: AdjudicationScopeV1{
				PullRequest:              record.Scope.PullRequest,
				HeadObjectID:             record.Scope.HeadObjectID,
				ConfigurationFingerprint: "sha256:different",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{record}, record.FindingRef, tt.scope)
			if ok {
				t.Fatal("expected no match for a scope that differs from the recorded decision; uncertainty must stay visible")
			}
		})
	}
}

func TestResolveFindingAdjudication_DifferentFindingRefDoesNotMatch(t *testing.T) {
	record := validAdjudicationRecord()
	otherRef := AdjudicationFindingRefV1{FindingID: "finding-2"}

	_, ok := ResolveFindingAdjudication([]AdjudicationRecordV1{record}, otherRef, record.Scope)
	if ok {
		t.Fatal("expected no match for an unrelated finding reference")
	}
}
