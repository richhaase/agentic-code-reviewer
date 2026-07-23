package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func newTestReviewConfiguration(t *testing.T) domain.ReviewConfiguration {
	t.Helper()
	cfg, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         3,
		Concurrency:       3,
		Timeout:           10 * time.Minute,
		Retries:           1,
		ReviewerAgents:    []string{"claude", "codex"},
		ReviewerModel:     "opus",
		SummarizerAgent:   "claude",
		SummarizerModel:   "opus",
		SummarizerTimeout: 5 * time.Minute,
		FPFilterTimeout:   5 * time.Minute,
		Guidance:          "focus on security",
		UseRefFile:        true,
		FPFilterEnabled:   true,
		FPThreshold:       75,
		PRFeedbackEnabled: true,
		PRFeedbackAgent:   "claude",
		ExcludePatterns:   []string{"vendor/.*"},
	})
	if err != nil {
		t.Fatalf("NewReviewConfiguration: %v", err)
	}
	return cfg
}

func TestReviewConfigurationV1_RoundTrip(t *testing.T) {
	cfg := newTestReviewConfiguration(t)
	schema := ToReviewConfigurationSchema(cfg)

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewConfigurationV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	restored, err := decoded.ToDomain()
	if err != nil {
		t.Fatalf("ToDomain: %v", err)
	}
	if restored.Fingerprint() != cfg.Fingerprint() {
		t.Fatalf("fingerprint mismatch: got %s, want %s", restored.Fingerprint(), cfg.Fingerprint())
	}
	if restored.Values().Guidance != cfg.Values().Guidance {
		t.Fatalf("guidance mismatch: got %s, want %s", restored.Values().Guidance, cfg.Values().Guidance)
	}
}

func TestReviewConfigurationV1_RejectsCorruptedFingerprint(t *testing.T) {
	cfg := newTestReviewConfiguration(t)
	schema := ToReviewConfigurationSchema(cfg)
	schema.Fingerprint = "sha256:corrupted"

	if _, err := schema.ToDomain(); err == nil {
		t.Fatal("expected an error for a fingerprint that does not match the stored values")
	}
}

func TestConfigurationSourceIdentityV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		source domain.ConfigurationSourceIdentity
	}{
		{
			name:   "disabled",
			source: domain.ConfigurationSourceIdentity{Kind: "disabled", Locator: "--no-config"},
		},
		{
			name: "repository revision with config present",
			source: domain.ConfigurationSourceIdentity{
				Kind:          "repository-revision",
				Locator:       "/repo",
				Ref:           "refs/acr/trusted-config/origin/main",
				Revision:      "abc123",
				ConfigPresent: true,
				ConfigDigest:  "deadbeef",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := ToConfigurationSourceIdentitySchema(tt.source)
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ConfigurationSourceIdentityV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := decoded.ToDomain(); got != tt.source {
				t.Fatalf("round trip mismatch: got %+v, want %+v", got, tt.source)
			}
		})
	}
}

func TestPolicySourceV1_Validate(t *testing.T) {
	tests := []struct {
		name    string
		source  PolicySourceV1
		wantErr bool
	}{
		{name: "disabled is trusted", source: PolicySourceV1{Kind: config.SourceKindDisabled}, wantErr: false},
		{name: "defaults is trusted", source: PolicySourceV1{Kind: config.SourceKindDefaults}, wantErr: false},
		{name: "repository revision is trusted", source: PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: "abc"}, wantErr: false},
		{name: "filesystem is rejected", source: PolicySourceV1{Kind: config.SourceKindFilesystem}, wantErr: true},
		{name: "empty kind is rejected", source: PolicySourceV1{}, wantErr: true},
		{name: "unknown kind is rejected", source: PolicySourceV1{Kind: "pr-worktree"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidatePolicySourceOutsideReview(t *testing.T) {
	reviewedHead := "reviewed-head-sha"
	target := ReviewTargetV1{Revision: RevisionEvidenceV1{HeadObjectID: reviewedHead}}

	tests := []struct {
		name    string
		source  PolicySourceV1
		wantErr bool
	}{
		{
			name:    "pinned revision distinct from reviewed head is allowed",
			source:  PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: "trusted-branch-sha"},
			wantErr: false,
		},
		{
			name:    "pinned revision equal to reviewed head is rejected",
			source:  PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: reviewedHead},
			wantErr: true,
		},
		{
			name:    "disabled source is allowed regardless of head",
			source:  PolicySourceV1{Kind: config.SourceKindDisabled},
			wantErr: false,
		},
		{
			name:    "filesystem source is rejected outright",
			source:  PolicySourceV1{Kind: config.SourceKindFilesystem},
			wantErr: true,
		},
		{
			name:    "missing source revision fails closed rather than passing",
			source:  PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicySourceOutsideReview(tt.source, target)
			if tt.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidatePolicySourceOutsideReview_MissingTargetHeadFailsClosed(t *testing.T) {
	incompleteTarget := ReviewTargetV1{Revision: RevisionEvidenceV1{HeadObjectID: ""}}
	source := PolicySourceV1{Kind: config.SourceKindRepositoryRevision, Revision: "trusted-branch-sha"}

	if err := ValidatePolicySourceOutsideReview(source, incompleteTarget); err == nil {
		t.Fatal("expected an error when the reviewed target has no head revision to compare against; missing evidence must not be treated as a passing check")
	}
}

func TestPolicySourceV1_ToConfigSourceIdentityRoundTrip(t *testing.T) {
	identity := config.SourceIdentity{
		Kind:          config.SourceKindRepositoryRevision,
		Locator:       "/repo",
		Ref:           "refs/acr/trusted-config/origin/main",
		Revision:      "abc123",
		ConfigPresent: true,
		ConfigDigest:  "digest",
	}
	schema := ToPolicySourceSchema(identity)
	if got := schema.ToConfigSourceIdentity(); got != identity {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, identity)
	}
}
