package feedback

import (
	"context"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestSummarizer_EmptyPRNumber(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	_, err := s.Summarize(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}

func TestNewSummarizer(t *testing.T) {
	s := NewSummarizer("claude", true, terminal.NewLogger())
	if s == nil {
		t.Fatal("NewSummarizer returned nil")
	}
}
