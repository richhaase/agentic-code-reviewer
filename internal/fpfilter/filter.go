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
}

func New(agentName string) *Filter {
	return &Filter{
		agentName: agentName,
	}
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

	// Build EvaluationPayload with null IsFalsePositive and Reasoning fields
	req := EvaluationPayload{
		Findings: make([]FindingEvaluation, len(grouped.Findings)),
	}
	for i, finding := range grouped.Findings {
		req.Findings[i] = FindingEvaluation{
			ID:              i,
			Title:           finding.Title,
			Summary:         finding.Summary,
			Messages:        finding.Messages,
			ReviewerCount:   finding.ReviewerCount,
			IsFalsePositive: nil, // Explicitly null - agent will fill this
			Reasoning:       nil, // Explicitly null - agent will fill this
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

	// Parse response back into same EvaluationPayload structure
	var response EvaluationPayload
	if err := json.Unmarshal([]byte(cleanedOutput), &response); err != nil {
		// Complete parse failure - keep all findings
		return &Result{
			Kept:        grouped,
			Duration:    time.Since(start),
			ParseErrors: len(grouped.Findings),
		}, nil
	}

	// Build map for efficient lookup by ID
	evalMap := make(map[int]FindingEvaluation)
	for _, eval := range response.Findings {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var filtered []FilteredFinding
	parseErrors := 0

	for i, finding := range grouped.Findings {
		eval, ok := evalMap[i]
		if !ok {
			// Agent didn't return this finding - keep it and count as parse error
			kept = append(kept, finding)
			parseErrors++
			continue
		}

		// If agent didn't fill in IsFalsePositive, treat as parse error and keep finding
		if eval.IsFalsePositive == nil {
			kept = append(kept, finding)
			parseErrors++
			continue
		}

		// Filter by boolean: true = false positive (filter out), false = real issue (keep)
		if *eval.IsFalsePositive {
			reasoning := ""
			if eval.Reasoning != nil {
				reasoning = *eval.Reasoning
			}
			filtered = append(filtered, FilteredFinding{
				Finding:   finding,
				Reasoning: reasoning,
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
