package store

import (
	"fmt"
	"time"
)

// AdjudicationDispositionV1 is the disposition vocabulary for finding
// adjudications, from issue #223.
type AdjudicationDispositionV1 string

const (
	AdjudicationFixed          AdjudicationDispositionV1 = "fixed"
	AdjudicationFalsePositive  AdjudicationDispositionV1 = "false_positive"
	AdjudicationDuplicate      AdjudicationDispositionV1 = "duplicate"
	AdjudicationAcceptedRisk   AdjudicationDispositionV1 = "accepted_risk"
	AdjudicationPolicyDecision AdjudicationDispositionV1 = "policy_decision"
	AdjudicationDeferred       AdjudicationDispositionV1 = "deferred"
	AdjudicationObsolete       AdjudicationDispositionV1 = "obsolete"
)

func (d AdjudicationDispositionV1) Validate() error {
	switch d {
	case AdjudicationFixed, AdjudicationFalsePositive, AdjudicationDuplicate,
		AdjudicationAcceptedRisk, AdjudicationPolicyDecision, AdjudicationDeferred, AdjudicationObsolete:
		return nil
	default:
		return fmt.Errorf("unknown adjudication disposition %q", d)
	}
}

// AdjudicationRelationV1 marks how a record relates to a prior adjudication
// of the same finding or cluster. A record with no relation is an original
// decision. Reopening, correcting, or superseding a decision is always
// recorded as a new, additive record that references the one it acts on;
// the original record is never edited or removed.
type AdjudicationRelationV1 string

const (
	AdjudicationRelationNone       AdjudicationRelationV1 = ""
	AdjudicationRelationReopened   AdjudicationRelationV1 = "reopened"
	AdjudicationRelationCorrected  AdjudicationRelationV1 = "corrected"
	AdjudicationRelationSuperseded AdjudicationRelationV1 = "superseded"
)

func (r AdjudicationRelationV1) Validate() error {
	switch r {
	case AdjudicationRelationNone, AdjudicationRelationReopened, AdjudicationRelationCorrected, AdjudicationRelationSuperseded:
		return nil
	default:
		return fmt.Errorf("unknown adjudication relation %q", r)
	}
}

// AdjudicationActorKindV1 distinguishes who or what made an adjudication
// decision.
type AdjudicationActorKindV1 string

const (
	AdjudicationActorHuman       AdjudicationActorKindV1 = "human"
	AdjudicationActorReviewAgent AdjudicationActorKindV1 = "review_agent"
	AdjudicationActorSystem      AdjudicationActorKindV1 = "system"
)

func (k AdjudicationActorKindV1) Validate() error {
	switch k {
	case AdjudicationActorHuman, AdjudicationActorReviewAgent, AdjudicationActorSystem:
		return nil
	default:
		return fmt.Errorf("unknown adjudication actor kind %q", k)
	}
}

type AdjudicationActorV1 struct {
	Kind     AdjudicationActorKindV1 `json:"kind"`
	Identity string                  `json:"identity"`
}

func (a AdjudicationActorV1) Validate() error {
	if err := a.Kind.Validate(); err != nil {
		return err
	}
	return validateNonEmpty("adjudication actor identity", a.Identity)
}

// AdjudicationFindingRefV1 identifies the finding or finding cluster an
// adjudication decision applies to. At least one of FindingID or ClusterID
// must be set.
type AdjudicationFindingRefV1 struct {
	FindingID string `json:"finding_id,omitempty"`
	ClusterID string `json:"cluster_id,omitempty"`
}

func (r AdjudicationFindingRefV1) Validate() error {
	if r.FindingID == "" && r.ClusterID == "" {
		return fmt.Errorf("adjudication finding reference requires a finding_id or cluster_id")
	}
	return nil
}

// AdjudicationScopeV1 records the pull request, head, and configuration
// fingerprint an adjudication decision was made under. Adjudication memory is
// only meaningfully reusable within a matching scope.
type AdjudicationScopeV1 struct {
	PullRequest              PullRequestKeyV1 `json:"pull_request"`
	HeadObjectID             string           `json:"head_object_id"`
	ConfigurationFingerprint string           `json:"configuration_fingerprint"`
}

func (s AdjudicationScopeV1) Validate() error {
	if err := s.PullRequest.Validate(); err != nil {
		return err
	}
	return validateNonEmpty("adjudication scope head object id", s.HeadObjectID)
}

// AdjudicationRecordV1 is a durable, additive decision record for a finding
// or finding cluster. See issue #223. A reopened, corrected, or superseded
// decision is recorded as a new record with RelationToPrior and
// SupersedesRecordID set, rather than mutating the record it replaces, so
// the original decision remains part of history.
type AdjudicationRecordV1 struct {
	SchemaVersion          int                       `json:"schema_version"`
	ID                     string                    `json:"id"`
	FindingRef             AdjudicationFindingRefV1  `json:"finding_ref"`
	Disposition            AdjudicationDispositionV1 `json:"disposition"`
	DecidingActor          AdjudicationActorV1       `json:"deciding_actor"`
	Rationale              string                    `json:"rationale"`
	Evidence               []string                  `json:"evidence,omitempty"`
	Scope                  AdjudicationScopeV1       `json:"scope"`
	InvalidationConditions []string                  `json:"invalidation_conditions,omitempty"`
	RecordedAt             time.Time                 `json:"recorded_at"`
	RelationToPrior        AdjudicationRelationV1    `json:"relation_to_prior,omitempty"`
	SupersedesRecordID     string                    `json:"supersedes_record_id,omitempty"`
}

func (r AdjudicationRecordV1) Validate() error {
	if err := validateSchemaVersion("adjudication record", r.SchemaVersion); err != nil {
		return err
	}
	if err := validateNonEmpty("adjudication record id", r.ID); err != nil {
		return err
	}
	if err := r.FindingRef.Validate(); err != nil {
		return err
	}
	if err := r.Disposition.Validate(); err != nil {
		return err
	}
	if err := r.DecidingActor.Validate(); err != nil {
		return err
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("adjudication record recorded_at is required")
	}
	if err := r.RelationToPrior.Validate(); err != nil {
		return err
	}
	if r.RelationToPrior != AdjudicationRelationNone {
		if err := validateNonEmpty("adjudication record supersedes_record_id", r.SupersedesRecordID); err != nil {
			return err
		}
	}
	if r.RelationToPrior == AdjudicationRelationNone && r.SupersedesRecordID != "" {
		return fmt.Errorf("adjudication record %s: supersedes_record_id requires a relation_to_prior", r.ID)
	}
	return nil
}

// ResolveFindingAdjudication returns the most recently recorded adjudication
// in records whose finding reference and scope match ref and scope exactly,
// and reports whether one was found. records is expected in chronological
// order, as returned by AdjudicationStore.ListAdjudications, so the last
// match is the newest entry in that finding's reopen/correct/supersede
// history. Matching requires the pull request, head object id, and
// configuration fingerprint to all be identical: an adjudication is only
// ever reused for a genuine exact repeat. A finding observed under a
// different head or configuration is semantically uncertain and must not
// silently inherit a prior decision, so it reports no match and remains
// visible rather than being suppressed.
func ResolveFindingAdjudication(records []AdjudicationRecordV1, ref AdjudicationFindingRefV1, scope AdjudicationScopeV1) (AdjudicationRecordV1, bool) {
	var latest AdjudicationRecordV1
	found := false
	for _, record := range records {
		if record.FindingRef != ref || record.Scope != scope {
			continue
		}
		latest = record
		found = true
	}
	return latest, found
}
