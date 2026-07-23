package store

import (
	"fmt"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type ReviewPhaseV1 string

const (
	ReviewPhaseInitializationV1      ReviewPhaseV1 = "initialization"
	ReviewPhaseRevisionsV1           ReviewPhaseV1 = "revisions"
	ReviewPhaseDiffV1                ReviewPhaseV1 = "diff"
	ReviewPhaseReviewersV1           ReviewPhaseV1 = "reviewers"
	ReviewPhaseFeedbackV1            ReviewPhaseV1 = "feedback"
	ReviewPhaseSummarizationV1       ReviewPhaseV1 = "summarization"
	ReviewPhaseFalsePositiveFilterV1 ReviewPhaseV1 = "false_positive_filter"
	ReviewPhaseExcludeFilterV1       ReviewPhaseV1 = "exclude_filter"
	ReviewPhaseCompleteV1            ReviewPhaseV1 = "complete"
)

func toReviewPhaseSchema(phase domain.ReviewPhase) (ReviewPhaseV1, error) {
	switch phase {
	case domain.ReviewPhaseInitialization:
		return ReviewPhaseInitializationV1, nil
	case domain.ReviewPhaseRevisions:
		return ReviewPhaseRevisionsV1, nil
	case domain.ReviewPhaseDiff:
		return ReviewPhaseDiffV1, nil
	case domain.ReviewPhaseReviewers:
		return ReviewPhaseReviewersV1, nil
	case domain.ReviewPhaseFeedback:
		return ReviewPhaseFeedbackV1, nil
	case domain.ReviewPhaseSummarization:
		return ReviewPhaseSummarizationV1, nil
	case domain.ReviewPhaseFalsePositiveFilter:
		return ReviewPhaseFalsePositiveFilterV1, nil
	case domain.ReviewPhaseExcludeFilter:
		return ReviewPhaseExcludeFilterV1, nil
	case domain.ReviewPhaseComplete:
		return ReviewPhaseCompleteV1, nil
	default:
		return "", fmt.Errorf("unknown review phase %q", phase)
	}
}

func (p ReviewPhaseV1) ToDomain() (domain.ReviewPhase, error) {
	switch p {
	case ReviewPhaseInitializationV1:
		return domain.ReviewPhaseInitialization, nil
	case ReviewPhaseRevisionsV1:
		return domain.ReviewPhaseRevisions, nil
	case ReviewPhaseDiffV1:
		return domain.ReviewPhaseDiff, nil
	case ReviewPhaseReviewersV1:
		return domain.ReviewPhaseReviewers, nil
	case ReviewPhaseFeedbackV1:
		return domain.ReviewPhaseFeedback, nil
	case ReviewPhaseSummarizationV1:
		return domain.ReviewPhaseSummarization, nil
	case ReviewPhaseFalsePositiveFilterV1:
		return domain.ReviewPhaseFalsePositiveFilter, nil
	case ReviewPhaseExcludeFilterV1:
		return domain.ReviewPhaseExcludeFilter, nil
	case ReviewPhaseCompleteV1:
		return domain.ReviewPhaseComplete, nil
	default:
		return "", fmt.Errorf("unknown stored review phase %q", p)
	}
}

type EngineV1 struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func ToEngineSchema(e domain.ReviewEngine) EngineV1 {
	return EngineV1{Name: e.Name, Version: e.Version}
}

func (e EngineV1) ToDomain() domain.ReviewEngine {
	return domain.ReviewEngine{Name: e.Name, Version: e.Version}
}

type RunFailureV1 struct {
	Phase   ReviewPhaseV1 `json:"phase"`
	Message string        `json:"message"`
}

func ToRunFailureSchema(f *domain.ReviewFailure) (*RunFailureV1, error) {
	if f == nil {
		return nil, nil
	}
	phase, err := toReviewPhaseSchema(f.Phase)
	if err != nil {
		return nil, err
	}
	return &RunFailureV1{Phase: phase, Message: f.Message}, nil
}

func (f *RunFailureV1) ToDomain() (*domain.ReviewFailure, error) {
	if f == nil {
		return nil, nil
	}
	phase, err := f.Phase.ToDomain()
	if err != nil {
		return nil, err
	}
	return &domain.ReviewFailure{Phase: phase, Message: f.Message}, nil
}

type SummarizerOutcomeV1 struct {
	ExitCode         int           `json:"exit_code"`
	Stderr           string        `json:"stderr"`
	DiagnosticOutput string        `json:"diagnostic_output"`
	Duration         time.Duration `json:"duration"`
	Warnings         []string      `json:"warnings,omitempty"`
}

func ToSummarizerOutcomeSchema(s domain.SummarizerOutcome) SummarizerOutcomeV1 {
	return SummarizerOutcomeV1{
		ExitCode:         s.ExitCode,
		Stderr:           s.Stderr,
		DiagnosticOutput: s.DiagnosticOutput,
		Duration:         s.Duration,
		Warnings:         append([]string(nil), s.Warnings...),
	}
}

func (s SummarizerOutcomeV1) ToDomain() domain.SummarizerOutcome {
	return domain.SummarizerOutcome{
		ExitCode:         s.ExitCode,
		Stderr:           s.Stderr,
		DiagnosticOutput: s.DiagnosticOutput,
		Duration:         s.Duration,
		Warnings:         append([]string(nil), s.Warnings...),
	}
}

type FalsePositiveFilterOutcomeV1 struct {
	Enabled    bool              `json:"enabled"`
	Applied    bool              `json:"applied"`
	Skipped    bool              `json:"skipped"`
	SkipReason string            `json:"skip_reason"`
	EvalErrors int               `json:"eval_errors"`
	Duration   time.Duration     `json:"duration"`
	Warnings   []string          `json:"warnings,omitempty"`
	Removed    []ReviewFindingV1 `json:"removed"`
}

func ToFalsePositiveFilterOutcomeSchema(o domain.FalsePositiveFilterOutcome) (FalsePositiveFilterOutcomeV1, error) {
	removed, err := toReviewFindingSchemaSlice(o.Removed)
	if err != nil {
		return FalsePositiveFilterOutcomeV1{}, fmt.Errorf("false positive filter outcome: %w", err)
	}
	return FalsePositiveFilterOutcomeV1{
		Enabled:    o.Enabled,
		Applied:    o.Applied,
		Skipped:    o.Skipped,
		SkipReason: o.SkipReason,
		EvalErrors: o.EvalErrors,
		Duration:   o.Duration,
		Warnings:   append([]string(nil), o.Warnings...),
		Removed:    removed,
	}, nil
}

func (o FalsePositiveFilterOutcomeV1) ToDomain() (domain.FalsePositiveFilterOutcome, error) {
	removed, err := toReviewFindingDomainSlice(o.Removed)
	if err != nil {
		return domain.FalsePositiveFilterOutcome{}, fmt.Errorf("false positive filter outcome: %w", err)
	}
	return domain.FalsePositiveFilterOutcome{
		Enabled:    o.Enabled,
		Applied:    o.Applied,
		Skipped:    o.Skipped,
		SkipReason: o.SkipReason,
		EvalErrors: o.EvalErrors,
		Duration:   o.Duration,
		Warnings:   append([]string(nil), o.Warnings...),
		Removed:    removed,
	}, nil
}

type ExcludeFilterOutcomeV1 struct {
	Patterns []string          `json:"patterns"`
	Removed  []ReviewFindingV1 `json:"removed"`
}

func ToExcludeFilterOutcomeSchema(o domain.ExcludeFilterOutcome) (ExcludeFilterOutcomeV1, error) {
	removed, err := toReviewFindingSchemaSlice(o.Removed)
	if err != nil {
		return ExcludeFilterOutcomeV1{}, fmt.Errorf("exclude filter outcome: %w", err)
	}
	return ExcludeFilterOutcomeV1{
		Patterns: append([]string(nil), o.Patterns...),
		Removed:  removed,
	}, nil
}

func (o ExcludeFilterOutcomeV1) ToDomain() (domain.ExcludeFilterOutcome, error) {
	removed, err := toReviewFindingDomainSlice(o.Removed)
	if err != nil {
		return domain.ExcludeFilterOutcome{}, fmt.Errorf("exclude filter outcome: %w", err)
	}
	return domain.ExcludeFilterOutcome{
		Patterns: append([]string(nil), o.Patterns...),
		Removed:  removed,
	}, nil
}

// RenderedOutcomeV1 preserves the exact review and LGTM bodies that were
// rendered for this run, if any were rendered. Text is preserved verbatim
// rather than being re-derived from other stored fields on read, so a later
// change to rendering logic cannot rewrite what a user actually saw or
// posted. Both fields are commonly empty: many runs are never rendered for
// posting (for example a clean run, or a run inspected only in history).
type RenderedOutcomeV1 struct {
	ReviewBody string `json:"review_body"`
	LGTMBody   string `json:"lgtm_body"`
}

// RunLifecycleV1 records desk-level transitions applied to an already
// completed, failed, or interrupted run after the fact. These transitions
// never rewrite the original terminal outcome; they are recorded alongside
// it so history stays honest about both what happened during execution and
// what the desk later learned (for example, that the head moved and the
// result is now stale).
type RunLifecycleV1 struct {
	Stale             bool      `json:"stale"`
	StaleReason       string    `json:"stale_reason,omitempty"`
	StaleAt           time.Time `json:"stale_at,omitempty"`
	SupersededByRunID string    `json:"superseded_by_run_id,omitempty"`
	SupersededAt      time.Time `json:"superseded_at,omitempty"`
}

type ReviewRunV1 struct {
	SchemaVersion            int                           `json:"schema_version"`
	ID                       string                        `json:"id"`
	Target                   ReviewTargetV1                `json:"target"`
	Trigger                  string                        `json:"trigger"`
	Engine                   EngineV1                      `json:"engine"`
	StartedAt                time.Time                     `json:"started_at"`
	CompletedAt              time.Time                     `json:"completed_at"`
	Configuration            ReviewConfigurationV1         `json:"configuration"`
	ConfigurationSource      ConfigurationSourceIdentityV1 `json:"configuration_source"`
	ConfigurationFingerprint string                        `json:"configuration_fingerprint"`
	Status                   string                        `json:"status"`
	Conclusion               string                        `json:"conclusion"`
	Failure                  *RunFailureV1                 `json:"failure,omitempty"`
	ReviewerResults          []ReviewerResultV1            `json:"reviewer_results"`
	Stats                    ReviewStatsV1                 `json:"stats"`
	Summarizer               SummarizerOutcomeV1           `json:"summarizer"`
	RawFindings              []FindingV1                   `json:"raw_findings"`
	AggregatedFindings       []AggregatedFindingV1         `json:"aggregated_findings"`
	PreFilterSummary         GroupedFindingsV1             `json:"pre_filter_summary"`
	FindingRecords           []ReviewFindingV1             `json:"finding_records"`
	Findings                 []ReviewFindingV1             `json:"findings"`
	Info                     []ReviewFindingV1             `json:"info"`
	FalsePositiveFilter      FalsePositiveFilterOutcomeV1  `json:"false_positive_filter"`
	ExcludeFilter            ExcludeFilterOutcomeV1        `json:"exclude_filter"`
	Dispositions             map[int]DispositionV1         `json:"dispositions"`
	Rendered                 RenderedOutcomeV1             `json:"rendered"`
	Lifecycle                RunLifecycleV1                `json:"lifecycle"`
}

func ToReviewRunSchema(run domain.ReviewRun, rendered RenderedOutcomeV1) (ReviewRunV1, error) {
	failure, err := ToRunFailureSchema(run.Failure)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	reviewerResults, err := toReviewerResultSchemaSlice(run.ReviewerResults)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	findingRecords, err := toReviewFindingSchemaSlice(run.FindingRecords)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	findings, err := toReviewFindingSchemaSlice(run.Findings)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	info, err := toReviewFindingSchemaSlice(run.Info)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	fpFilter, err := ToFalsePositiveFilterOutcomeSchema(run.FalsePositiveFilter)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	excludeFilter, err := ToExcludeFilterOutcomeSchema(run.ExcludeFilter)
	if err != nil {
		return ReviewRunV1{}, fmt.Errorf("run %s: %w", run.ID, err)
	}
	dispositions := make(map[int]DispositionV1, len(run.Dispositions))
	for id, disposition := range run.Dispositions {
		schema, err := ToDispositionSchema(disposition)
		if err != nil {
			return ReviewRunV1{}, fmt.Errorf("run %s: disposition %d: %w", run.ID, id, err)
		}
		dispositions[id] = schema
	}

	return ReviewRunV1{
		SchemaVersion:            CurrentSchemaVersion,
		ID:                       run.ID,
		Target:                   ToReviewTargetSchema(run.Target),
		Trigger:                  string(run.Trigger),
		Engine:                   ToEngineSchema(run.Engine),
		StartedAt:                run.StartedAt,
		CompletedAt:              run.CompletedAt,
		Configuration:            ToReviewConfigurationSchema(run.Configuration),
		ConfigurationSource:      ToConfigurationSourceIdentitySchema(run.ConfigurationSource),
		ConfigurationFingerprint: run.ConfigurationFingerprint,
		Status:                   string(run.Status),
		Conclusion:               string(run.Conclusion),
		Failure:                  failure,
		ReviewerResults:          reviewerResults,
		Stats:                    ToReviewStatsSchema(run.Stats),
		Summarizer:               ToSummarizerOutcomeSchema(run.Summarizer),
		RawFindings:              toFindingSchemaSlice(run.RawFindings),
		AggregatedFindings:       toAggregatedFindingSchemaSlice(run.AggregatedFindings),
		PreFilterSummary:         ToGroupedFindingsSchema(run.PreFilterSummary),
		FindingRecords:           findingRecords,
		Findings:                 findings,
		Info:                     info,
		FalsePositiveFilter:      fpFilter,
		ExcludeFilter:            excludeFilter,
		Dispositions:             dispositions,
		Rendered:                 rendered,
	}, nil
}

func FromReviewRunSchema(schema ReviewRunV1) (domain.ReviewRun, RenderedOutcomeV1, error) {
	if err := validateSchemaVersion("review run", schema.SchemaVersion); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, err
	}
	if err := validateNonEmpty("review run id", schema.ID); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, err
	}

	target := schema.Target.ToDomain()
	if err := target.Validate(); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}

	trigger := domain.ReviewTrigger(schema.Trigger)
	if err := trigger.Validate(); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}

	engine := schema.Engine.ToDomain()
	if err := engine.Validate(); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}

	configuration, err := schema.Configuration.ToDomain()
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}

	configurationSource := schema.ConfigurationSource.ToDomain()
	if err := configurationSource.Validate(); err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}

	status := domain.ReviewStatus(schema.Status)
	switch status {
	case domain.ReviewStatusCompleted, domain.ReviewStatusFailed, domain.ReviewStatusInterrupted:
	default:
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: unknown stored review status %q", schema.ID, schema.Status)
	}

	conclusion := domain.ReviewConclusion(schema.Conclusion)
	switch conclusion {
	case domain.ReviewConclusionNone, domain.ReviewConclusionNoChanges, domain.ReviewConclusionClean, domain.ReviewConclusionFindings:
	default:
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: unknown stored review conclusion %q", schema.ID, schema.Conclusion)
	}

	failure, err := schema.Failure.ToDomain()
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	reviewerResults, err := toReviewerResultDomainSlice(schema.ReviewerResults)
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	findingRecords, err := toReviewFindingDomainSlice(schema.FindingRecords)
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	findings, err := toReviewFindingDomainSlice(schema.Findings)
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	info, err := toReviewFindingDomainSlice(schema.Info)
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	fpFilter, err := schema.FalsePositiveFilter.ToDomain()
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	excludeFilter, err := schema.ExcludeFilter.ToDomain()
	if err != nil {
		return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: %w", schema.ID, err)
	}
	dispositions := make(map[int]domain.Disposition, len(schema.Dispositions))
	for id, disposition := range schema.Dispositions {
		d, err := disposition.ToDomain()
		if err != nil {
			return domain.ReviewRun{}, RenderedOutcomeV1{}, fmt.Errorf("run %s: disposition %d: %w", schema.ID, id, err)
		}
		dispositions[id] = d
	}

	run := domain.ReviewRun{
		ID:                       schema.ID,
		Target:                   target,
		Trigger:                  trigger,
		Engine:                   engine,
		StartedAt:                schema.StartedAt,
		CompletedAt:              schema.CompletedAt,
		Configuration:            configuration,
		ConfigurationSource:      configurationSource,
		ConfigurationFingerprint: schema.ConfigurationFingerprint,
		Status:                   status,
		Conclusion:               conclusion,
		Failure:                  failure,
		ReviewerResults:          reviewerResults,
		Stats:                    schema.Stats.ToDomain(),
		Summarizer:               schema.Summarizer.ToDomain(),
		RawFindings:              toFindingDomainSlice(schema.RawFindings),
		AggregatedFindings:       toAggregatedFindingDomainSlice(schema.AggregatedFindings),
		PreFilterSummary:         schema.PreFilterSummary.ToDomain(),
		FindingRecords:           findingRecords,
		Findings:                 findings,
		Info:                     info,
		FalsePositiveFilter:      fpFilter,
		ExcludeFilter:            excludeFilter,
		Dispositions:             dispositions,
	}
	return run, schema.Rendered, nil
}
