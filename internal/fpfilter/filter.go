// Package fpfilter provides false positive filtering for code review findings.
package fpfilter

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

const DefaultThreshold = 75

// FindingEvaluation represents a finding with fields for in-place evaluation.
// The LLM agent receives this with IsFalsePositive and Reasoning as null,
// and returns the same structure with those fields filled in.
type FindingEvaluation struct {
	ID              int      `json:"id"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Messages        []string `json:"messages"`
	ReviewerCount   int      `json:"reviewer_count"`
	IsFalsePositive *bool    `json:"is_false_positive,omitempty"`
	Reasoning       *string  `json:"reasoning,omitempty"`
}

// EvaluationPayload wraps the findings array for LLM input/output.
type EvaluationPayload struct {
	Findings []FindingEvaluation `json:"findings"`
}

// FilteredFinding represents a finding that was filtered out as a false positive.
type FilteredFinding struct {
	Finding   domain.FindingGroup
	Reasoning string
}

// Result represents the output of the false positive filter.
type Result struct {
	Kept          domain.GroupedFindings
	Filtered      []FilteredFinding
	FilteredCount int
	Duration      time.Duration
	ParseErrors   int
}

// EvaluatedFinding represents a finding with its evaluation score (deprecated).
type EvaluatedFinding struct {
	Finding   domain.FindingGroup
	FPScore   int
	Reasoning string
}

type Filter struct {
	agentName string
	threshold int
}

func New(agentName string, threshold int) *Filter {
	if threshold < 1 || threshold > 100 {
		threshold = DefaultThreshold
	}
	return &Filter{
		agentName: agentName,
		threshold: threshold,
	}
}

type evaluationRequest struct {
	Findings []findingInput `json:"findings"`
}

type findingInput struct {
	ID            int      `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Messages      []string `json:"messages"`
	ReviewerCount int      `json:"reviewer_count"`
}

type evaluationResponse struct {
	Evaluations []findingEvaluation `json:"evaluations"`
}

type findingEvaluation struct {
	ID        int    `json:"id"`
	FPScore   int    `json:"fp_score"`
	Reasoning string `json:"reasoning"`
}

func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings) (*Result, error) {
	start := time.Now()

	if len(grouped.Findings) == 0 {
		return &Result{
			Kept:     grouped,
			Duration: time.Since(start),
		}, nil
	}

	ag, err := agent.NewAgent(f.agentName)
	if err != nil {
		return nil, err
	}

	req := evaluationRequest{
		Findings: make([]findingInput, len(grouped.Findings)),
	}
	for i, finding := range grouped.Findings {
		req.Findings[i] = findingInput{
			ID:            i,
			Title:         finding.Title,
			Summary:       finding.Summary,
			Messages:      finding.Messages,
			ReviewerCount: finding.ReviewerCount,
		}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	reader, err := ag.ExecuteSummary(ctx, fpEvaluationPrompt, payload)
	if err != nil {
		if ctx.Err() != nil {
			return &Result{
				Kept:     grouped,
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		if ctx.Err() != nil {
			return &Result{
				Kept:     grouped,
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	if closer, ok := reader.(io.Closer); ok {
		_ = closer.Close()
	}

	cleanedOutput := agent.StripMarkdownCodeFence(string(output))

	var response evaluationResponse
	if err := json.Unmarshal([]byte(cleanedOutput), &response); err != nil {
		return &Result{
			Kept:        grouped,
			Duration:    time.Since(start),
			ParseErrors: len(grouped.Findings),
		}, nil
	}

	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var filtered []FilteredFinding
	parseErrors := 0

	for i, finding := range grouped.Findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			parseErrors++
			continue
		}

		if eval.FPScore >= f.threshold {
			filtered = append(filtered, FilteredFinding{
				Finding:   finding,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	return &Result{
		Kept: domain.GroupedFindings{
			Findings: kept,
			Info:     grouped.Info,
		},
		Filtered:      filtered,
		FilteredCount: len(filtered),
		Duration:      time.Since(start),
		ParseErrors:   parseErrors,
	}, nil
}
