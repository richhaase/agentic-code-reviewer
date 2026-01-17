// Package summarizer provides finding summarization via LLM.
package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

const groupPrompt = `# Codex Review Summarizer

You are grouping results from repeated Codex review runs.

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

// Summarize summarizes the aggregated findings using an LLM.
func Summarize(ctx context.Context, aggregated []domain.AggregatedFinding) (*Result, error) {
	start := time.Now()

	if len(aggregated) == 0 {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			Duration: time.Since(start),
		}, nil
	}

	// Build input payload
	type inputItem struct {
		ID        int    `json:"id"`
		Text      string `json:"text"`
		Reviewers []int  `json:"reviewers"`
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
		return nil, fmt.Errorf("failed to marshal aggregated findings: %w", err)
	}

	fullPrompt := groupPrompt + "\n\nINPUT JSON:\n" + string(payload) + "\n"

	// Check if context is already canceled
	if ctx.Err() != nil {
		return &Result{
			ExitCode: -1,
			Stderr:   "context canceled",
			Duration: time.Since(start),
		}, nil
	}

	cmd := exec.CommandContext(ctx, "codex", "exec", "--color", "never", "-")
	cmd.Stdin = bytes.NewReader([]byte(fullPrompt))

	// Set process group for proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err = cmd.Start(); err != nil {
		// Handle context cancellation during start
		if ctx.Err() != nil {
			return &Result{
				ExitCode: -1,
				Stderr:   "context canceled",
				Duration: time.Since(start),
			}, nil
		}
		return nil, fmt.Errorf("failed to start summarizer: %w", err)
	}

	// Wait for completion, but kill process group if context is canceled
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Kill the entire process group
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-waitDone // Wait for process to be reaped
		return &Result{
			ExitCode: -1,
			Stderr:   "context canceled",
			Duration: time.Since(start),
		}, nil
	case err = <-waitDone:
		// Process completed normally
	}

	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	output := stdout.String()
	stderrStr := stderr.String()

	if output == "" {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: exitCode,
			Stderr:   stderrStr,
			Duration: duration,
		}, nil
	}

	var grouped domain.GroupedFindings
	if err := json.Unmarshal([]byte(output), &grouped); err != nil {
		return &Result{
			Grouped:  domain.GroupedFindings{},
			ExitCode: 1,
			Stderr:   "failed to parse summarizer JSON output",
			RawOut:   output,
			Duration: duration,
		}, nil
	}

	return &Result{
		Grouped:  grouped,
		ExitCode: exitCode,
		Stderr:   stderrStr,
		RawOut:   output,
		Duration: duration,
	}, nil
}
