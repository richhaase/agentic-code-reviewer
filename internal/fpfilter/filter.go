// Package fpfilter provides false positive filtering for code review findings.
package fpfilter

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// Verbose enables debug logging for the fpfilter package.
// Set this to true to log non-fatal errors like Close() failures.
var Verbose bool

// logVerbose logs a message if Verbose is enabled.
func logVerbose(format string, args ...interface{}) {
	if Verbose {
		log.Printf("[fpfilter] "+format, args...)
	}
}

// DefaultThreshold is the minimum confidence score (0-100) for a finding to
// be considered a true positive. Findings below this threshold are filtered
// as likely false positives. 75 was chosen based on empirical testing to
// balance precision (fewer false positives) with recall (keeping real issues).
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
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Messages []string `json:"messages"`
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
		return nil, err
	}

	req := evaluationRequest{
		Findings: make([]findingInput, len(grouped.Findings)),
	}
	for i, finding := range grouped.Findings {
		req.Findings[i] = findingInput{
			ID:       i,
			Title:    finding.Title,
			Summary:  finding.Summary,
			Messages: finding.Messages,
		}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	execResult, err := ag.ExecuteSummary(ctx, fpEvaluationPrompt, payload)
	if err != nil {
		if ctx.Err() != nil {
			return &Result{
				Grouped:  grouped,
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}
	// Close errors are non-fatal; they only occur on process cleanup issues.
	defer func() {
		if err := execResult.Close(); err != nil {
			logVerbose("close error (non-fatal): %v", err)
		}
	}()

	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			return &Result{
				Grouped:  grouped,
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	cleanedOutput := agent.StripMarkdownCodeFence(string(output))

	var response evaluationResponse
	if err := json.Unmarshal([]byte(cleanedOutput), &response); err != nil {
		return &Result{
			Grouped:    grouped,
			Duration:   time.Since(start),
			EvalErrors: len(grouped.Findings),
		}, nil
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
