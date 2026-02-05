package feedback

import (
	"context"
	"testing"
)

func TestSummarizer_EmptyPRNumber(t *testing.T) {
	s := NewSummarizer("codex", false)
	_, err := s.Summarize(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}

func TestNewSummarizer(t *testing.T) {
	s := NewSummarizer("claude", true)
	if s == nil {
		t.Fatal("NewSummarizer returned nil")
	}
}
