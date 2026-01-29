// Package summarizer provides finding summarization via LLM.
package summarizer

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
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

// Result contains the output from the summarizer.
type Result struct {
	Grouped  domain.GroupedFindings
	ExitCode int
	Stderr   string
	RawOut   string
	Duration time.Duration
}

// inputItem represents a single finding for the summarizer input payload.
type inputItem struct {
	ID        int    `json:"id"`
	Text      string `json:"text"`
	Reviewers []int  `json:"reviewers"`
}

// Summarize summarizes the aggregated findings using an LLM.
// The agentName parameter specifies which agent to use for summarization.
func Summarize(ctx context.Context, agentName string, aggregated []domain.AggregatedFinding) (*Result, error) {
	start := time.Now()

	if len(aggregated) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			Duration: time.Since(start),
		}, nil
	}

	// Create agent
	ag, err := agent.NewAgent(agentName)
	if err != nil {
		return nil, err
	}

	// Build input payload
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

	// Check if context is already canceled
	if ctx.Err() != nil {
		return &Result{
			ExitCode: -1,
			Stderr:   "context canceled",
			Duration: time.Since(start),
		}, nil
	}

	// Execute summary via agent
	execResult, err := ag.ExecuteSummary(ctx, groupPrompt, payload)
	if err != nil {
		// Handle context cancellation
		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}
	defer execResult.Close()

	// Read all output
	output, err := io.ReadAll(execResult)
	if err != nil {
		// Handle context cancellation
		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, err
	}

	// Close to get exit code and stderr (defer will be a no-op due to sync.Once)
	_ = execResult.Close()
	exitCode := execResult.ExitCode()
	stderr := execResult.Stderr()
	duration := time.Since(start)

	if len(output) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: exitCode,
			Stderr:   stderr,
			Duration: duration,
		}, nil
	}

	// Create parser for this agent's output format
	parser, err := agent.NewSummaryParser(agentName)
	if err != nil {
		return nil, err
	}

	// Parse the output
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
			RawOut:   string(output),
			Duration: duration,
		}, nil
	}

	return &Result{
		Grouped:  *grouped,
		ExitCode: exitCode,
		Stderr:   stderr,
		RawOut:   string(output),
		Duration: duration,
	}, nil
}
