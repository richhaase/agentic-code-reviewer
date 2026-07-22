package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestPriorFeedbackFailureWarning(t *testing.T) {
	tests := []struct {
		name      string
		parentErr error
		taskErr   error
		failure   error
		want      string
	}{
		{
			name:      "deliberate teardown",
			parentErr: context.Canceled,
			taskErr:   context.Canceled,
			failure:   context.Canceled,
		},
		{
			name:    "timeout",
			taskErr: context.DeadlineExceeded,
			failure: context.DeadlineExceeded,
			want:    "PR feedback summarizer timed out after 5s",
		},
		{
			name:    "failure",
			failure: errors.New("agent failed"),
			want:    "PR feedback summarizer failed: agent failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := priorFeedbackFailureWarning(tt.parentErr, tt.taskErr, tt.failure, 5*time.Second)
			if got != tt.want {
				t.Fatalf("warning = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUsesGeminiAgent(t *testing.T) {
	tests := []struct {
		name string
		opts ReviewOpts
		want bool
	}{
		{
			name: "reviewer agent gemini",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:  []string{"agy", "gemini", "codex"},
					SummarizerAgent: "codex",
				},
			},
			want: true,
		},
		{
			name: "summarizer agent gemini",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:  []string{"agy", "codex"},
					SummarizerAgent: "gemini",
				},
			},
			want: true,
		},
		{
			name: "explicit pr feedback agent gemini",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:    []string{"agy", "codex"},
					SummarizerAgent:   "codex",
					PRFeedbackEnabled: true,
					PRFeedbackAgent:   "gemini",
					FPFilterEnabled:   true,
				},
				DetectedPR: "123",
			},
			want: true,
		},
		{
			name: "pr feedback agent gemini without detected pr is not used",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:    []string{"agy", "codex"},
					SummarizerAgent:   "codex",
					PRFeedbackEnabled: true,
					PRFeedbackAgent:   "gemini",
					FPFilterEnabled:   true,
				},
			},
			want: false,
		},
		{
			name: "pr feedback agent gemini with fp filter disabled is not used",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:    []string{"agy", "codex"},
					SummarizerAgent:   "codex",
					PRFeedbackEnabled: true,
					PRFeedbackAgent:   "gemini",
					FPFilterEnabled:   false,
				},
				DetectedPR: "123",
			},
			want: false,
		},
		{
			name: "disabled pr feedback agent gemini is not used",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:    []string{"agy", "codex"},
					SummarizerAgent:   "codex",
					PRFeedbackEnabled: false,
					PRFeedbackAgent:   "gemini",
				},
			},
			want: false,
		},
		{
			name: "no gemini",
			opts: ReviewOpts{
				ResolvedConfig: config.ResolvedConfig{
					ReviewerAgents:    []string{"agy", "codex", "claude"},
					SummarizerAgent:   "codex",
					PRFeedbackEnabled: true,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usesGeminiAgent(tt.opts); got != tt.want {
				t.Errorf("usesGeminiAgent() = %v, want %v", got, tt.want)
			}
		})
	}
}
