package store

import (
	"fmt"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type ReviewerFailureKindV1 string

const (
	ReviewerFailureKindExecution   ReviewerFailureKindV1 = "execution"
	ReviewerFailureKindExit        ReviewerFailureKindV1 = "exit"
	ReviewerFailureKindTimeout     ReviewerFailureKindV1 = "timeout"
	ReviewerFailureKindAuth        ReviewerFailureKindV1 = "authentication"
	ReviewerFailureKindInterrupted ReviewerFailureKindV1 = "interrupted"
	ReviewerFailureKindParser      ReviewerFailureKindV1 = "parser"
)

func toReviewerFailureKindSchema(kind domain.ReviewerFailureKind) (ReviewerFailureKindV1, error) {
	switch kind {
	case domain.ReviewerFailureExecution:
		return ReviewerFailureKindExecution, nil
	case domain.ReviewerFailureExit:
		return ReviewerFailureKindExit, nil
	case domain.ReviewerFailureTimeout:
		return ReviewerFailureKindTimeout, nil
	case domain.ReviewerFailureAuth:
		return ReviewerFailureKindAuth, nil
	case domain.ReviewerFailureInterrupted:
		return ReviewerFailureKindInterrupted, nil
	case domain.ReviewerFailureParser:
		return ReviewerFailureKindParser, nil
	default:
		return "", fmt.Errorf("unknown reviewer failure kind %q", kind)
	}
}

func (k ReviewerFailureKindV1) ToDomain() (domain.ReviewerFailureKind, error) {
	switch k {
	case ReviewerFailureKindExecution:
		return domain.ReviewerFailureExecution, nil
	case ReviewerFailureKindExit:
		return domain.ReviewerFailureExit, nil
	case ReviewerFailureKindTimeout:
		return domain.ReviewerFailureTimeout, nil
	case ReviewerFailureKindAuth:
		return domain.ReviewerFailureAuth, nil
	case ReviewerFailureKindInterrupted:
		return domain.ReviewerFailureInterrupted, nil
	case ReviewerFailureKindParser:
		return domain.ReviewerFailureParser, nil
	default:
		return "", fmt.Errorf("unknown stored reviewer failure kind %q", k)
	}
}

type ReviewerFailureV1 struct {
	Kind    ReviewerFailureKindV1 `json:"kind"`
	Message string                `json:"message"`
}

func ToReviewerFailureSchema(f *domain.ReviewerFailure) (*ReviewerFailureV1, error) {
	if f == nil {
		return nil, nil
	}
	kind, err := toReviewerFailureKindSchema(f.Kind)
	if err != nil {
		return nil, err
	}
	return &ReviewerFailureV1{Kind: kind, Message: f.Message}, nil
}

func (f *ReviewerFailureV1) ToDomain() (*domain.ReviewerFailure, error) {
	if f == nil {
		return nil, nil
	}
	kind, err := f.Kind.ToDomain()
	if err != nil {
		return nil, err
	}
	return &domain.ReviewerFailure{Kind: kind, Message: f.Message}, nil
}

const ReviewerWarningKindCleanupV1 = "cleanup"

type ReviewerWarningV1 struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func ToReviewerWarningSchema(w domain.ReviewerWarning) (ReviewerWarningV1, error) {
	if w.Kind != domain.ReviewerWarningCleanup {
		return ReviewerWarningV1{}, fmt.Errorf("unknown reviewer warning kind %q", w.Kind)
	}
	return ReviewerWarningV1{Kind: ReviewerWarningKindCleanupV1, Message: w.Message}, nil
}

func (w ReviewerWarningV1) ToDomain() (domain.ReviewerWarning, error) {
	if w.Kind != ReviewerWarningKindCleanupV1 {
		return domain.ReviewerWarning{}, fmt.Errorf("unknown stored reviewer warning kind %q", w.Kind)
	}
	return domain.ReviewerWarning{Kind: domain.ReviewerWarningCleanup, Message: w.Message}, nil
}

func toReviewerWarningSchemaSlice(warnings []domain.ReviewerWarning) ([]ReviewerWarningV1, error) {
	out := make([]ReviewerWarningV1, 0, len(warnings))
	for _, w := range warnings {
		schema, err := ToReviewerWarningSchema(w)
		if err != nil {
			return nil, err
		}
		out = append(out, schema)
	}
	return out, nil
}

func toReviewerWarningDomainSlice(warnings []ReviewerWarningV1) ([]domain.ReviewerWarning, error) {
	out := make([]domain.ReviewerWarning, 0, len(warnings))
	for _, w := range warnings {
		d, err := w.ToDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

type ReviewerResultV1 struct {
	ReviewerID  int                 `json:"reviewer_id"`
	AgentName   string              `json:"agent_name"`
	Findings    []FindingV1         `json:"findings"`
	ExitCode    int                 `json:"exit_code"`
	Attempts    int                 `json:"attempts"`
	ParseErrors int                 `json:"parse_errors"`
	TimedOut    bool                `json:"timed_out"`
	AuthFailed  bool                `json:"auth_failed"`
	Duration    time.Duration       `json:"duration"`
	Failure     *ReviewerFailureV1  `json:"failure,omitempty"`
	Warnings    []ReviewerWarningV1 `json:"warnings,omitempty"`
}

func ToReviewerResultSchema(r domain.ReviewerResult) (ReviewerResultV1, error) {
	failure, err := ToReviewerFailureSchema(r.Failure)
	if err != nil {
		return ReviewerResultV1{}, fmt.Errorf("reviewer %d: %w", r.ReviewerID, err)
	}
	warnings, err := toReviewerWarningSchemaSlice(r.Warnings)
	if err != nil {
		return ReviewerResultV1{}, fmt.Errorf("reviewer %d: %w", r.ReviewerID, err)
	}
	return ReviewerResultV1{
		ReviewerID:  r.ReviewerID,
		AgentName:   r.AgentName,
		Findings:    toFindingSchemaSlice(r.Findings),
		ExitCode:    r.ExitCode,
		Attempts:    r.Attempts,
		ParseErrors: r.ParseErrors,
		TimedOut:    r.TimedOut,
		AuthFailed:  r.AuthFailed,
		Duration:    r.Duration,
		Failure:     failure,
		Warnings:    warnings,
	}, nil
}

func (r ReviewerResultV1) ToDomain() (domain.ReviewerResult, error) {
	failure, err := r.Failure.ToDomain()
	if err != nil {
		return domain.ReviewerResult{}, fmt.Errorf("reviewer %d: %w", r.ReviewerID, err)
	}
	warnings, err := toReviewerWarningDomainSlice(r.Warnings)
	if err != nil {
		return domain.ReviewerResult{}, fmt.Errorf("reviewer %d: %w", r.ReviewerID, err)
	}
	return domain.ReviewerResult{
		ReviewerID:  r.ReviewerID,
		AgentName:   r.AgentName,
		Findings:    toFindingDomainSlice(r.Findings),
		ExitCode:    r.ExitCode,
		Attempts:    r.Attempts,
		ParseErrors: r.ParseErrors,
		TimedOut:    r.TimedOut,
		AuthFailed:  r.AuthFailed,
		Duration:    r.Duration,
		Failure:     failure,
		Warnings:    warnings,
	}, nil
}

func toReviewerResultSchemaSlice(results []domain.ReviewerResult) ([]ReviewerResultV1, error) {
	out := make([]ReviewerResultV1, 0, len(results))
	for _, r := range results {
		schema, err := ToReviewerResultSchema(r)
		if err != nil {
			return nil, err
		}
		out = append(out, schema)
	}
	return out, nil
}

func toReviewerResultDomainSlice(results []ReviewerResultV1) ([]domain.ReviewerResult, error) {
	out := make([]domain.ReviewerResult, 0, len(results))
	for _, r := range results {
		d, err := r.ToDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

type ReviewStatsV1 struct {
	TotalReviewers      int                   `json:"total_reviewers"`
	SuccessfulReviewers int                   `json:"successful_reviewers"`
	FailedReviewers     []int                 `json:"failed_reviewers"`
	TimedOutReviewers   []int                 `json:"timed_out_reviewers"`
	AuthFailedReviewers []int                 `json:"auth_failed_reviewers"`
	ParseErrors         int                   `json:"parse_errors"`
	ReviewerDurations   map[int]time.Duration `json:"reviewer_durations"`
	ReviewerAgentNames  map[int]string        `json:"reviewer_agent_names"`
	WallClockDuration   time.Duration         `json:"wall_clock_duration"`
	SummarizerDuration  time.Duration         `json:"summarizer_duration"`
	FPFilterDuration    time.Duration         `json:"fp_filter_duration"`
	FPFilteredCount     int                   `json:"fp_filtered_count"`
}

func cloneIntDurationMap(m map[int]time.Duration) map[int]time.Duration {
	if m == nil {
		return nil
	}
	out := make(map[int]time.Duration, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneIntStringMap(m map[int]string) map[int]string {
	if m == nil {
		return nil
	}
	out := make(map[int]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func ToReviewStatsSchema(s domain.ReviewStats) ReviewStatsV1 {
	return ReviewStatsV1{
		TotalReviewers:      s.TotalReviewers,
		SuccessfulReviewers: s.SuccessfulReviewers,
		FailedReviewers:     append([]int(nil), s.FailedReviewers...),
		TimedOutReviewers:   append([]int(nil), s.TimedOutReviewers...),
		AuthFailedReviewers: append([]int(nil), s.AuthFailedReviewers...),
		ParseErrors:         s.ParseErrors,
		ReviewerDurations:   cloneIntDurationMap(s.ReviewerDurations),
		ReviewerAgentNames:  cloneIntStringMap(s.ReviewerAgentNames),
		WallClockDuration:   s.WallClockDuration,
		SummarizerDuration:  s.SummarizerDuration,
		FPFilterDuration:    s.FPFilterDuration,
		FPFilteredCount:     s.FPFilteredCount,
	}
}

func (s ReviewStatsV1) ToDomain() domain.ReviewStats {
	return domain.ReviewStats{
		TotalReviewers:      s.TotalReviewers,
		SuccessfulReviewers: s.SuccessfulReviewers,
		FailedReviewers:     append([]int(nil), s.FailedReviewers...),
		TimedOutReviewers:   append([]int(nil), s.TimedOutReviewers...),
		AuthFailedReviewers: append([]int(nil), s.AuthFailedReviewers...),
		ParseErrors:         s.ParseErrors,
		ReviewerDurations:   cloneIntDurationMap(s.ReviewerDurations),
		ReviewerAgentNames:  cloneIntStringMap(s.ReviewerAgentNames),
		WallClockDuration:   s.WallClockDuration,
		SummarizerDuration:  s.SummarizerDuration,
		FPFilterDuration:    s.FPFilterDuration,
		FPFilteredCount:     s.FPFilteredCount,
	}
}
