package feedback

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

type Summarizer struct {
	agentName string
	model     string
	verbose   bool
	logger    *terminal.Logger
}

func NewSummarizer(agentName, model string, verbose bool, logger *terminal.Logger) *Summarizer {
	return &Summarizer{
		agentName: agentName,
		model:     model,
		verbose:   verbose,
		logger:    logger,
	}
}

func (s *Summarizer) Summarize(ctx context.Context, prNumber string) (string, error) {
	if prNumber == "" {
		return "", fmt.Errorf("PR number is required")
	}

	prCtx, err := FetchPRContext(ctx, prNumber)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR context: %w", err)
	}

	if !prCtx.HasContent() {
		return "", nil
	}

	input := s.buildInput(prCtx)

	ag, err := agent.NewAgentWithModel(s.agentName, s.model)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	execResult, err := ag.ExecuteSummary(ctx, summarizePrompt, []byte(input))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("agent execution failed: %w", err)
	}
	defer func() {
		if err := execResult.Close(); err != nil && s.verbose {
			s.logger.Logf(terminal.StyleDim, "feedback close error (non-fatal): %v", err)
		}
	}()

	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("failed to read agent output: %w", err)
	}

	summary := strings.TrimSpace(string(output))

	summary = agent.StripMarkdownCodeFence(summary)

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
