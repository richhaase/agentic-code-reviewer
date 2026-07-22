package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
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
	return s.SummarizeFromDir(ctx, prNumber, "")
}

func (s *Summarizer) SummarizeFromDir(ctx context.Context, prNumber, workDir string) (string, error) {
	if prNumber == "" {
		return "", fmt.Errorf("PR number is required")
	}

	prCtx, err := FetchPRContextFromDir(ctx, prNumber, workDir)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR context: %w", err)
	}
	return s.summarizeContext(ctx, prCtx, workDir)
}

func (s *Summarizer) SummarizePullRequest(ctx context.Context, key domain.PullRequestKey, workDir string) (string, error) {
	prCtx, err := FetchPRContextForPullRequest(ctx, key, workDir)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR context: %w", err)
	}
	return s.summarizeContext(ctx, prCtx, workDir)
}

func (s *Summarizer) summarizeContext(ctx context.Context, prCtx *PRContext, workDir string) (string, error) {
	if !prCtx.HasContent() {
		return "", nil
	}

	input := s.buildInput(prCtx)

	ag, err := agent.NewAgentWithModel(s.agentName, s.model)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	execResult, err := ag.ExecuteSummary(ctx, &agent.SummaryConfig{Prompt: summarizePrompt, Input: []byte(input), WorkDir: workDir})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("agent execution failed: %w", err)
	}
	output, err := io.ReadAll(execResult)
	closeErr := execResult.Close()
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Join(ctx.Err(), closeErr)
		}
		return "", errors.Join(fmt.Errorf("failed to read agent output: %w", err), closeErr)
	}

	summary := strings.TrimSpace(string(output))

	summary = agent.StripMarkdownCodeFence(summary)

	if strings.Contains(strings.ToLower(summary), "no prior feedback") {
		summary = ""
	}

	return summary, s.handleCloseError(closeErr)
}

func (s *Summarizer) handleCloseError(err error) error {
	if err == nil {
		return nil
	}
	if s.verbose && s.logger != nil {
		s.logger.Logf(terminal.StyleDim, "feedback close error (non-fatal): %v", err)
		return nil
	}
	return fmt.Errorf("feedback cleanup failed: %w", err)
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
