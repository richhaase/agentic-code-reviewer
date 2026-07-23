package store

import (
	"fmt"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type ConfigurationSourceIdentityV1 struct {
	Kind          string `json:"kind"`
	Locator       string `json:"locator"`
	Ref           string `json:"ref"`
	Revision      string `json:"revision"`
	ConfigPresent bool   `json:"config_present"`
	ConfigDigest  string `json:"config_digest"`
}

func ToConfigurationSourceIdentitySchema(s domain.ConfigurationSourceIdentity) ConfigurationSourceIdentityV1 {
	return ConfigurationSourceIdentityV1{
		Kind:          s.Kind,
		Locator:       s.Locator,
		Ref:           s.Ref,
		Revision:      s.Revision,
		ConfigPresent: s.ConfigPresent,
		ConfigDigest:  s.ConfigDigest,
	}
}

func (s ConfigurationSourceIdentityV1) ToDomain() domain.ConfigurationSourceIdentity {
	return domain.ConfigurationSourceIdentity{
		Kind:          s.Kind,
		Locator:       s.Locator,
		Ref:           s.Ref,
		Revision:      s.Revision,
		ConfigPresent: s.ConfigPresent,
		ConfigDigest:  s.ConfigDigest,
	}
}

func (s ConfigurationSourceIdentityV1) Validate() error {
	return s.ToDomain().Validate()
}

type ReviewConfigurationValuesV1 struct {
	Reviewers         int           `json:"reviewers"`
	Concurrency       int           `json:"concurrency"`
	Timeout           time.Duration `json:"timeout"`
	Retries           int           `json:"retries"`
	ReviewerAgents    []string      `json:"reviewer_agents"`
	ReviewerModel     string        `json:"reviewer_model"`
	SummarizerAgent   string        `json:"summarizer_agent"`
	SummarizerModel   string        `json:"summarizer_model"`
	SummarizerTimeout time.Duration `json:"summarizer_timeout"`
	FPFilterTimeout   time.Duration `json:"fp_filter_timeout"`
	Guidance          string        `json:"guidance"`
	UseRefFile        bool          `json:"use_ref_file"`
	FPFilterEnabled   bool          `json:"fp_filter_enabled"`
	FPThreshold       int           `json:"fp_threshold"`
	PRFeedbackEnabled bool          `json:"pr_feedback_enabled"`
	PRFeedbackAgent   string        `json:"pr_feedback_agent"`
	ExcludePatterns   []string      `json:"exclude_patterns"`
}

type ReviewConfigurationV1 struct {
	Values      ReviewConfigurationValuesV1 `json:"values"`
	Fingerprint string                      `json:"fingerprint"`
}

func ToReviewConfigurationSchema(c domain.ReviewConfiguration) ReviewConfigurationV1 {
	values := c.Values()
	return ReviewConfigurationV1{
		Values: ReviewConfigurationValuesV1{
			Reviewers:         values.Reviewers,
			Concurrency:       values.Concurrency,
			Timeout:           values.Timeout,
			Retries:           values.Retries,
			ReviewerAgents:    append([]string(nil), values.ReviewerAgents...),
			ReviewerModel:     values.ReviewerModel,
			SummarizerAgent:   values.SummarizerAgent,
			SummarizerModel:   values.SummarizerModel,
			SummarizerTimeout: values.SummarizerTimeout,
			FPFilterTimeout:   values.FPFilterTimeout,
			Guidance:          values.Guidance,
			UseRefFile:        values.UseRefFile,
			FPFilterEnabled:   values.FPFilterEnabled,
			FPThreshold:       values.FPThreshold,
			PRFeedbackEnabled: values.PRFeedbackEnabled,
			PRFeedbackAgent:   values.PRFeedbackAgent,
			ExcludePatterns:   append([]string(nil), values.ExcludePatterns...),
		},
		Fingerprint: c.Fingerprint(),
	}
}

func (c ReviewConfigurationV1) ToDomain() (domain.ReviewConfiguration, error) {
	values := domain.ReviewConfigurationValues{
		Reviewers:         c.Values.Reviewers,
		Concurrency:       c.Values.Concurrency,
		Timeout:           c.Values.Timeout,
		Retries:           c.Values.Retries,
		ReviewerAgents:    append([]string(nil), c.Values.ReviewerAgents...),
		ReviewerModel:     c.Values.ReviewerModel,
		SummarizerAgent:   c.Values.SummarizerAgent,
		SummarizerModel:   c.Values.SummarizerModel,
		SummarizerTimeout: c.Values.SummarizerTimeout,
		FPFilterTimeout:   c.Values.FPFilterTimeout,
		Guidance:          c.Values.Guidance,
		UseRefFile:        c.Values.UseRefFile,
		FPFilterEnabled:   c.Values.FPFilterEnabled,
		FPThreshold:       c.Values.FPThreshold,
		PRFeedbackEnabled: c.Values.PRFeedbackEnabled,
		PRFeedbackAgent:   c.Values.PRFeedbackAgent,
		ExcludePatterns:   append([]string(nil), c.Values.ExcludePatterns...),
	}
	cfg, err := domain.NewReviewConfiguration(values)
	if err != nil {
		return domain.ReviewConfiguration{}, fmt.Errorf("reconstruct stored review configuration: %w", err)
	}
	if cfg.Fingerprint() != c.Fingerprint {
		return domain.ReviewConfiguration{}, fmt.Errorf(
			"stored review configuration fingerprint %q does not match recomputed fingerprint %q; record may be corrupt",
			c.Fingerprint, cfg.Fingerprint(),
		)
	}
	return cfg, nil
}

// PolicySourceV1 records the provenance of a trusted control-plane input used
// to seed adjudication memory, budget policy, stop policy, or evaluation
// guidance. It mirrors config.SourceIdentity, the trust boundary established
// by issue #220, rather than inventing a second one. Only sources capable of
// resolving to a pinned, non-PR-relative input are accepted; a raw filesystem
// read (config.SourceKindFilesystem) is never a valid policy source because it
// resolves relative to whatever directory a caller passes at run time,
// including a reviewed PR's own worktree.
type PolicySourceV1 struct {
	Kind          string `json:"kind"`
	Locator       string `json:"locator"`
	Ref           string `json:"ref"`
	Revision      string `json:"revision"`
	ConfigPresent bool   `json:"config_present"`
	ConfigDigest  string `json:"config_digest"`
}

func ToPolicySourceSchema(s config.SourceIdentity) PolicySourceV1 {
	return PolicySourceV1{
		Kind:          s.Kind,
		Locator:       s.Locator,
		Ref:           s.Ref,
		Revision:      s.Revision,
		ConfigPresent: s.ConfigPresent,
		ConfigDigest:  s.ConfigDigest,
	}
}

func (s PolicySourceV1) ToConfigSourceIdentity() config.SourceIdentity {
	return config.SourceIdentity{
		Kind:          s.Kind,
		Locator:       s.Locator,
		Ref:           s.Ref,
		Revision:      s.Revision,
		ConfigPresent: s.ConfigPresent,
		ConfigDigest:  s.ConfigDigest,
	}
}

func (s PolicySourceV1) Validate() error {
	switch s.Kind {
	case config.SourceKindDisabled, config.SourceKindDefaults, config.SourceKindRepositoryRevision:
		return nil
	case config.SourceKindFilesystem:
		return fmt.Errorf("adjudication policy source kind %q resolves relative to a caller-supplied directory and can never be trusted for adjudication memory, budget policy, stop policy, or evaluation guidance", s.Kind)
	case "":
		return fmt.Errorf("adjudication policy source kind is required")
	default:
		return fmt.Errorf("adjudication policy source kind %q is not a recognized trusted configuration source", s.Kind)
	}
}

// ValidatePolicySourceOutsideReview rejects a policy source that resolves to
// the head revision of the pull request under review. A pinned
// repository-revision source is otherwise trusted, but it must not pin the
// very head the review is evaluating: that would let the reviewed PR content
// supply its own adjudication memory, budget policy, stop policy, or
// evaluation guidance.
func ValidatePolicySourceOutsideReview(source PolicySourceV1, target ReviewTargetV1) error {
	if err := source.Validate(); err != nil {
		return err
	}
	if source.Kind != config.SourceKindRepositoryRevision {
		return nil
	}
	if source.Revision == "" || target.Revision.HeadObjectID == "" {
		return nil
	}
	if source.Revision == target.Revision.HeadObjectID {
		return fmt.Errorf("adjudication policy source revision %q matches the reviewed pull request head; policy must come from a source outside the reviewed head and worktree", source.Revision)
	}
	return nil
}
