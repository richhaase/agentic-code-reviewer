package main

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

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
