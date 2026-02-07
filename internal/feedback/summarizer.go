package feedback

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

// Summarizer summarizes PR feedback for the FP filter.
type Summarizer struct {
	agentName string
	verbose   bool
}

// NewSummarizer creates a new PR feedback summarizer.
func NewSummarizer(agentName string, verbose bool) *Summarizer {
	return &Summarizer{
		agentName: agentName,
		verbose:   verbose,
	}
}

// Summarize fetches PR context and returns a structured summary of prior feedback.
func (s *Summarizer) Summarize(ctx context.Context, prNumber string) (string, error) {
	if prNumber == "" {
		return "", fmt.Errorf("PR number is required")
	}

	// Fetch PR context
	prCtx, err := FetchPRContext(ctx, prNumber)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR context: %w", err)
	}

	if !prCtx.HasContent() {
		return "", nil
	}

	// Build input for the LLM
	input := s.buildInput(prCtx)

	// Create agent
	ag, err := agent.NewAgent(s.agentName)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	// Execute summary
	execResult, err := ag.ExecuteSummary(ctx, summarizePrompt, []byte(input))
	if err != nil {
		if ctx.Err() != nil {
			return "", nil // Context canceled, return empty
		}
		return "", fmt.Errorf("agent execution failed: %w", err)
	}
	defer func() {
		if err := execResult.Close(); err != nil && s.verbose {
			log.Printf("[feedback] close error (non-fatal): %v", err)
		}
	}()

	// Read output
	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil
		}
		return "", fmt.Errorf("failed to read agent output: %w", err)
	}

	summary := strings.TrimSpace(string(output))

	// Clean up markdown code fences if present
	summary = agent.StripMarkdownCodeFence(summary)

	// Check for "no feedback" response
	if strings.Contains(strings.ToLower(summary), "no prior feedback") {
		return "", nil
	}

	return summary, nil
}

func (s *Summarizer) buildInput(prCtx *PRContext) string {
	var sb strings.Builder

	sb.WriteString("## PR Description\n\n")
	if prCtx.Description != "" {
		sb.WriteString(prCtx.Description)
	} else {
		sb.WriteString("(No description)")
	}
	sb.WriteString("\n\n")

	if len(prCtx.Comments) > 0 {
		sb.WriteString("## Comments\n\n")
		for _, c := range prCtx.Comments {
			fmt.Fprintf(&sb, "**%s**: %s\n\n", c.Author, c.Body)
			for _, r := range c.Replies {
				fmt.Fprintf(&sb, "  > **%s**: %s\n\n", r.Author, r.Body)
			}
		}
	}

	return sb.String()
}
