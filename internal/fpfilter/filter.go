package fpfilter

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const DefaultThreshold = 75

type EvaluatedFinding struct {
	Index     int
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
	model     string
	agent     agent.Agent
	threshold int
	verbose   bool
	logger    *terminal.Logger
}

func New(agentName, model string, threshold int, verbose bool, logger *terminal.Logger) *Filter {
	if threshold < 1 || threshold > 100 {
		threshold = DefaultThreshold
	}
	return &Filter{
		agentName: agentName,
		model:     model,
		threshold: threshold,
		logger:    logger,
		verbose:   verbose,
	}
}

func NewWithAgent(ag agent.Agent, threshold int) *Filter {
	if threshold < 1 || threshold > 100 {
		threshold = DefaultThreshold
	}
	return &Filter{
		agent:     ag,
		threshold: threshold,
	}
}

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

func agreementBonus(reviewerCount, totalReviewers int) int {
	if totalReviewers <= 1 || reviewerCount <= 0 {
		return 0
	}

	ratio := float64(reviewerCount) / float64(totalReviewers)

	switch {
	case ratio < 0.2:
		return 15
	case ratio < 0.4:
		return 10
	default:
		return 0
	}
}

func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings, priorFeedback string, totalReviewers int) *Result {
	start := time.Now()

	if len(grouped.Findings) == 0 {
		return &Result{
			Grouped:  grouped,
			Duration: time.Since(start),
		}
	}

	ag := f.agent
	if ag == nil {
		var err error
		ag, err = agent.NewAgentWithModel(f.agentName, f.model)
		if err != nil {
			return skippedResult(grouped, start, "agent creation failed: "+err.Error())
		}
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
		return skippedResult(grouped, start, "request marshal failed: "+err.Error())
	}

	prompt := buildPromptWithFeedback(fpEvaluationPrompt, priorFeedback)
	execResult, err := ag.ExecuteSummary(ctx, prompt, payload)
	if err != nil {
		if ctx.Err() != nil {
			return skippedResult(grouped, start, "context canceled")
		}
		return skippedResult(grouped, start, "LLM execution failed: "+err.Error())
	}

	defer func() {
		if err := execResult.Close(); err != nil && f.verbose && f.logger != nil {
			f.logger.Logf(terminal.StyleDim, "fp-filter close error (non-fatal): %v", err)
		}
	}()

	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			return skippedResult(grouped, start, "context canceled")
		}
		return skippedResult(grouped, start, "response read failed: "+err.Error())
	}

	parser, err := agent.NewSummaryParser(ag.Name())
	if err != nil {
		return skippedResult(grouped, start, "parser creation failed: "+err.Error())
	}

	responseText, err := parser.ExtractText(output)
	if err != nil {
		return skippedResult(grouped, start, "response extraction failed: "+err.Error())
	}

	var response evaluationResponse
	if err := json.Unmarshal([]byte(responseText), &response); err != nil {
		r := skippedResult(grouped, start, "response parse failed: "+err.Error())
		r.EvalErrors = len(grouped.Findings)
		return r
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

		adjusted := min(eval.FPScore+agreementBonus(finding.ReviewerCount, totalReviewers), 100)

		if adjusted >= f.threshold {
			removed = append(removed, EvaluatedFinding{
				Index:     i,
				Finding:   finding,
				FPScore:   adjusted,
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
	}
}
