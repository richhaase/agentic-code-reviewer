package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type PullRequestKey struct {
	Host       string
	Owner      string
	Repository string
	Number     int
}

var pullRequestHostPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?$`)

var pullRequestPathComponentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (k PullRequestKey) Validate() error {
	var invalid []string
	if strings.TrimSpace(k.Host) == "" {
		invalid = append(invalid, "host is required")
	} else if strings.TrimSpace(k.Host) != k.Host || !pullRequestHostPattern.MatchString(k.Host) {
		invalid = append(invalid, "host contains invalid characters")
	}
	if strings.TrimSpace(k.Owner) == "" {
		invalid = append(invalid, "owner is required")
	} else if !validPullRequestPathComponent(k.Owner) {
		invalid = append(invalid, "owner contains invalid characters")
	}
	if strings.TrimSpace(k.Repository) == "" {
		invalid = append(invalid, "repository is required")
	} else if !validPullRequestPathComponent(k.Repository) {
		invalid = append(invalid, "repository contains invalid characters")
	}
	if k.Number < 1 {
		invalid = append(invalid, "pull request number must be positive")
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid pull request key: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func validPullRequestPathComponent(value string) bool {
	return strings.TrimSpace(value) == value && value != "." && value != ".." && pullRequestPathComponentPattern.MatchString(value)
}

func (k PullRequestKey) String() string {
	return fmt.Sprintf("%s/%s/%s#%d", k.Host, k.Owner, k.Repository, k.Number)
}

type RevisionEvidence struct {
	RequestedBaseRef string
	ResolvedBaseRef  string
	HeadObjectID     string
	BaseObjectID     string
}

type ReviewTarget struct {
	RepositoryRoot string
	WorktreeRoot   string
	Revision       RevisionEvidence
	PullRequest    *PullRequestKey
}

func (t ReviewTarget) Validate() error {
	var invalid []string
	if !filepath.IsAbs(t.RepositoryRoot) {
		invalid = append(invalid, "repository root must be absolute")
	}
	if !filepath.IsAbs(t.WorktreeRoot) {
		invalid = append(invalid, "worktree root must be absolute")
	}
	if strings.TrimSpace(t.Revision.RequestedBaseRef) == "" {
		invalid = append(invalid, "requested base ref is required")
	}
	if strings.TrimSpace(t.Revision.ResolvedBaseRef) == "" {
		invalid = append(invalid, "resolved base ref is required")
	}
	if t.PullRequest != nil {
		if err := t.PullRequest.Validate(); err != nil {
			invalid = append(invalid, err.Error())
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid review target: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func (t ReviewTarget) Clone() ReviewTarget {
	clone := t
	if t.PullRequest != nil {
		key := *t.PullRequest
		clone.PullRequest = &key
	}
	return clone
}

type ReviewTrigger string

const (
	ReviewTriggerManual ReviewTrigger = "manual"
	ReviewTriggerWatch  ReviewTrigger = "watch"
	ReviewTriggerDesk   ReviewTrigger = "desk"
)

func (t ReviewTrigger) Validate() error {
	switch t {
	case ReviewTriggerManual, ReviewTriggerWatch, ReviewTriggerDesk:
		return nil
	default:
		return fmt.Errorf("invalid review trigger %q", t)
	}
}

type ReviewStatus string

const (
	ReviewStatusCompleted   ReviewStatus = "completed"
	ReviewStatusFailed      ReviewStatus = "failed"
	ReviewStatusInterrupted ReviewStatus = "interrupted"
)

type ReviewConclusion string

const (
	ReviewConclusionNone      ReviewConclusion = ""
	ReviewConclusionNoChanges ReviewConclusion = "no_changes"
	ReviewConclusionClean     ReviewConclusion = "clean"
	ReviewConclusionFindings  ReviewConclusion = "findings"
)

type ReviewPhase string

const (
	ReviewPhaseInitialization      ReviewPhase = "initialization"
	ReviewPhaseRevisions           ReviewPhase = "revisions"
	ReviewPhaseDiff                ReviewPhase = "diff"
	ReviewPhaseReviewers           ReviewPhase = "reviewers"
	ReviewPhaseFeedback            ReviewPhase = "feedback"
	ReviewPhaseSummarization       ReviewPhase = "summarization"
	ReviewPhaseFalsePositiveFilter ReviewPhase = "false_positive_filter"
	ReviewPhaseExcludeFilter       ReviewPhase = "exclude_filter"
	ReviewPhaseComplete            ReviewPhase = "complete"
)

type ReviewEngine struct {
	Name    string
	Version string
}

func (e ReviewEngine) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("review engine name is required")
	}
	if strings.TrimSpace(e.Version) == "" {
		return fmt.Errorf("review engine version is required")
	}
	return nil
}

type ConfigurationSourceIdentity struct {
	Kind          string
	Locator       string
	Ref           string
	Revision      string
	ConfigPresent bool
	ConfigDigest  string
}

func (s ConfigurationSourceIdentity) Validate() error {
	if strings.TrimSpace(s.Kind) == "" {
		return fmt.Errorf("configuration source kind is required")
	}
	if s.ConfigPresent && strings.TrimSpace(s.ConfigDigest) == "" {
		return fmt.Errorf("configuration source digest is required when configuration is present")
	}
	if !s.ConfigPresent && s.ConfigDigest != "" {
		return fmt.Errorf("configuration source digest requires a present configuration")
	}
	return nil
}

type ReviewConfigurationValues struct {
	Reviewers         int
	Concurrency       int
	Timeout           time.Duration
	Retries           int
	ReviewerAgents    []string
	ReviewerModel     string
	SummarizerAgent   string
	SummarizerModel   string
	SummarizerTimeout time.Duration
	FPFilterTimeout   time.Duration
	Guidance          string
	UseRefFile        bool
	FPFilterEnabled   bool
	FPThreshold       int
	PRFeedbackEnabled bool
	PRFeedbackAgent   string
	ExcludePatterns   []string
}

type ReviewConfiguration struct {
	values      ReviewConfigurationValues
	fingerprint string
}

func NewReviewConfiguration(values ReviewConfigurationValues) (ReviewConfiguration, error) {
	values.ReviewerAgents = cloneStrings(values.ReviewerAgents)
	values.ExcludePatterns = cloneStrings(values.ExcludePatterns)

	if err := validateReviewConfiguration(values); err != nil {
		return ReviewConfiguration{}, err
	}

	payload, err := json.Marshal(values)
	if err != nil {
		return ReviewConfiguration{}, fmt.Errorf("fingerprint review configuration: %w", err)
	}
	sum := sha256.Sum256(payload)

	return ReviewConfiguration{
		values:      values,
		fingerprint: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func (c ReviewConfiguration) Values() ReviewConfigurationValues {
	values := c.values
	values.ReviewerAgents = slices.Clone(c.values.ReviewerAgents)
	values.ExcludePatterns = slices.Clone(c.values.ExcludePatterns)
	return values
}

func (c ReviewConfiguration) Fingerprint() string {
	return c.fingerprint
}

func (c ReviewConfiguration) Validate() error {
	if err := validateReviewConfiguration(c.values); err != nil {
		return err
	}
	if c.fingerprint == "" {
		return fmt.Errorf("review configuration fingerprint is required")
	}
	return nil
}

func validateReviewConfiguration(values ReviewConfigurationValues) error {
	var invalid []string
	if values.Reviewers < 1 {
		invalid = append(invalid, "reviewers must be positive")
	}
	if values.Concurrency < 1 {
		invalid = append(invalid, "concurrency must be positive")
	} else if values.Concurrency > values.Reviewers {
		invalid = append(invalid, "concurrency cannot exceed reviewers")
	}
	if values.Timeout <= 0 {
		invalid = append(invalid, "reviewer timeout must be positive")
	}
	if values.Retries < 0 {
		invalid = append(invalid, "retries cannot be negative")
	}
	if len(values.ReviewerAgents) == 0 {
		invalid = append(invalid, "at least one reviewer agent is required")
	}
	for i, name := range values.ReviewerAgents {
		if strings.TrimSpace(name) == "" {
			invalid = append(invalid, fmt.Sprintf("reviewer agent %d is empty", i+1))
		}
	}
	if strings.TrimSpace(values.SummarizerAgent) == "" {
		invalid = append(invalid, "summarizer agent is required")
	}
	if values.SummarizerTimeout <= 0 {
		invalid = append(invalid, "summarizer timeout must be positive")
	}
	if values.FPFilterTimeout <= 0 {
		invalid = append(invalid, "false-positive filter timeout must be positive")
	}
	if values.FPThreshold < 1 || values.FPThreshold > 100 {
		invalid = append(invalid, "false-positive threshold must be between 1 and 100")
	}
	for _, pattern := range values.ExcludePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			invalid = append(invalid, fmt.Sprintf("invalid exclude pattern %q: %v", pattern, err))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid review configuration: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return slices.Clone(values)
}

type ReviewFailure struct {
	Phase   ReviewPhase
	Message string
}

type SummarizerOutcome struct {
	ExitCode         int
	Stderr           string
	DiagnosticOutput string
	Duration         time.Duration
	Warnings         []string
}

type ReviewFindingKind string

const (
	ReviewFindingActionable    ReviewFindingKind = "actionable"
	ReviewFindingInformational ReviewFindingKind = "informational"
)

type ReviewFinding struct {
	ID          string
	Kind        ReviewFindingKind
	Group       FindingGroup
	Disposition Disposition
}

type FalsePositiveFilterOutcome struct {
	Enabled    bool
	Applied    bool
	Skipped    bool
	SkipReason string
	EvalErrors int
	Duration   time.Duration
	Removed    []ReviewFinding
}

type ExcludeFilterOutcome struct {
	Patterns []string
	Removed  []ReviewFinding
}

type ReviewRun struct {
	ID                       string
	Target                   ReviewTarget
	Trigger                  ReviewTrigger
	Engine                   ReviewEngine
	StartedAt                time.Time
	CompletedAt              time.Time
	Configuration            ReviewConfiguration
	ConfigurationSource      ConfigurationSourceIdentity
	ConfigurationFingerprint string
	Status                   ReviewStatus
	Conclusion               ReviewConclusion
	Failure                  *ReviewFailure
	ReviewerResults          []ReviewerResult
	Stats                    ReviewStats
	Summarizer               SummarizerOutcome
	RawFindings              []Finding
	AggregatedFindings       []AggregatedFinding
	PreFilterSummary         GroupedFindings
	FindingRecords           []ReviewFinding
	Findings                 []ReviewFinding
	Info                     []ReviewFinding
	FalsePositiveFilter      FalsePositiveFilterOutcome
	ExcludeFilter            ExcludeFilterOutcome
	Dispositions             map[int]Disposition
}

func (r ReviewRun) FinalGroupedFindings() GroupedFindings {
	grouped := GroupedFindings{
		Findings: make([]FindingGroup, 0, len(r.Findings)),
		Info:     make([]FindingGroup, 0, len(r.Info)),
	}
	for _, finding := range r.Findings {
		grouped.Findings = append(grouped.Findings, finding.Group)
	}
	for _, finding := range r.Info {
		grouped.Info = append(grouped.Info, finding.Group)
	}
	return grouped
}
