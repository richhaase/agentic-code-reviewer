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

type EvaluatedFinding struct {
	Finding   domain.FindingGroup
	FPScore   int
	Reasoning string
}

type Result struct {
	Grouped      domain.GroupedFindings
	Removed      []EvaluatedFinding
	RemovedCount int
	Duration     time.Duration
	EvalErrors   int
	Skipped      bool
	SkipReason   string
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

// skippedResult returns a Result that passes through all findings unfiltered.
// Used for fail-open behavior when errors occur.
func skippedResult(grouped domain.GroupedFindings, start time.Time, reason string) *Result {
	return &Result{
		Grouped:    grouped,
		Duration:   time.Since(start),
		Skipped:    true,
		SkipReason: reason,
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
			Grouped:  grouped,
			Duration: time.Since(start),
		}, nil
	}

	ag, err := agent.NewAgent(f.agentName)
	if err != nil {
		return skippedResult(grouped, start, "agent creation failed: "+err.Error()), nil
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
		return skippedResult(grouped, start, "request marshal failed: "+err.Error()), nil
	}

	reader, err := ag.ExecuteSummary(ctx, fpEvaluationPrompt, payload)
	if err != nil {
		if ctx.Err() != nil {
			return skippedResult(grouped, start, "context cancelled"), nil
		}
		return skippedResult(grouped, start, "LLM execution failed: "+err.Error()), nil
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		if ctx.Err() != nil {
			return skippedResult(grouped, start, "context cancelled"), nil
		}
		return skippedResult(grouped, start, "response read failed: "+err.Error()), nil
	}

	if closer, ok := reader.(io.Closer); ok {
		_ = closer.Close()
	}

	cleanedOutput := agent.StripMarkdownCodeFence(string(output))

	var response evaluationResponse
	if err := json.Unmarshal([]byte(cleanedOutput), &response); err != nil {
		r := skippedResult(grouped, start, "response parse failed: "+err.Error())
		r.EvalErrors = len(grouped.Findings)
		return r, nil
	}

	evalMap := make(map[int]findingEvaluation)
	for _, eval := range response.Evaluations {
		evalMap[eval.ID] = eval
	}

	var kept []domain.FindingGroup
	var removed []EvaluatedFinding
	evalErrors := 0

	for i, finding := range grouped.Findings {
		eval, ok := evalMap[i]
		if !ok {
			kept = append(kept, finding)
			evalErrors++
			continue
		}

		if eval.FPScore >= f.threshold {
			removed = append(removed, EvaluatedFinding{
				Finding:   finding,
				FPScore:   eval.FPScore,
				Reasoning: eval.Reasoning,
			})
		} else {
			kept = append(kept, finding)
		}
	}

	return &Result{
		Grouped: domain.GroupedFindings{
			Findings: kept,
			Info:     grouped.Info,
		},
		Removed:      removed,
		RemovedCount: len(removed),
		Duration:     time.Since(start),
		EvalErrors:   evalErrors,
	}, nil
}
