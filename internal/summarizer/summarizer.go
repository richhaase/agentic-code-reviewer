package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const groupPrompt = `# Code Review Summarizer

You are grouping results from repeated code review runs.

Input: a JSON array of objects, each with "id" (input identifier), "text" (the finding),
and "reviewers" (list of reviewer IDs that found it).

Task:
- Cluster messages that describe the same underlying issue.
- Create a short, precise title per group.
- Keep groups distinct; do not merge different issues.
- If something is unique, keep it as its own group.
- Sum up unique reviewer IDs across clustered messages for reviewer_count.
- Track which input ids are represented in each group via "sources".

Output format (JSON only, no extra prose):
{
  "findings": [
    {
      "title": "Short issue title",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [0, 2]
    }
  ],
  "info": [
    {
      "title": "Informational note",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [1]
    }
  ]
}

Rules:
- Return ONLY valid JSON.
- Keep excerpts under ~200 characters each.
- Preserve file paths, line numbers, flags, branch names, and commands in excerpts when present.
- If a message includes a file path with line numbers, keep that exact location text in the excerpt.
- "sources" must include all input ids represented in each group.
- reviewer_count = number of unique reviewers that reported any message in this cluster.
- Put non-actionable outcomes (e.g., "no diffs", "no changes to review") in "info".
- If the input is empty, return: {"findings": [], "info": []}`

type Result struct {
	Grouped  domain.GroupedFindings
	ExitCode int
	Stderr   string
	RawOut   string
	Duration time.Duration
}

type inputItem struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	Reviewers []int  `json:"reviewers"`
}

func Summarize(ctx context.Context, agentName, model string, aggregated []domain.AggregatedFinding, verbose bool, logger *terminal.Logger) (*Result, error) {
	start := time.Now()

	if len(aggregated) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			Duration: time.Since(start),
		}, nil
	}

	ag, err := agent.NewAgentWithModel(agentName, model)
	if err != nil {
		return nil, err
	}

	items := make([]inputItem, len(aggregated))
	for i, a := range aggregated {
		items[i] = inputItem{
			ID:        i,
			Text:      a.Text,
			Reviewers: a.Reviewers,
		}
	}

	payload, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return &Result{
			ExitCode: -1,
			Stderr:   "context canceled",
			Duration: time.Since(start),
		}, nil
	}

	execResult, err := ag.ExecuteSummary(ctx, groupPrompt, payload)
	if err != nil {

		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	defer func() {
		if err := execResult.Close(); err != nil && verbose {
			logger.Logf(terminal.StyleDim, "summarizer close error (non-fatal): %v", err)
		}
	}()

	output, err := io.ReadAll(execResult)
	if err != nil {

		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	if err := execResult.Close(); err != nil && verbose {
		logger.Logf(terminal.StyleDim, "summarizer close error (non-fatal): %v", err)
	}
	exitCode := execResult.ExitCode()
	stderr := execResult.Stderr()
	duration := time.Since(start)
	rawOut := string(output)

	if exitCode != 0 && agent.IsAuthFailure(agentName, exitCode, stderr, rawOut) {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: exitCode,
			Stderr:   fmt.Sprintf("%s authentication failed: %s", agentName, agent.AuthHint(agentName)),
			RawOut:   rawOut,
			Duration: duration,
		}, nil
	}

	if len(output) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: exitCode,
			Stderr:   stderr,
			Duration: duration,
		}, nil
	}

	parser, err := agent.NewSummaryParser(agentName)
	if err != nil {
		return nil, err
	}

	grouped, err := parser.Parse(output)
	if err != nil {
		parseErr := "failed to parse summarizer output: " + err.Error()
		if stderr != "" {
			parseErr = stderr + "\n" + parseErr
		}
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: 1,
			Stderr:   parseErr,
			RawOut:   rawOut,
			Duration: duration,
		}, nil
	}

	return &Result{
		Grouped:  *grouped,
		ExitCode: exitCode,
		Stderr:   stderr,
		RawOut:   rawOut,
		Duration: duration,
	}, nil
}
