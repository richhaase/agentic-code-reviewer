package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewClaudeAgent(t *testing.T) {
	agent := NewClaudeAgent()
	if agent == nil {
		t.Fatal("NewClaudeAgent() returned nil")
	}
}

func TestClaudeAgent_Name(t *testing.T) {
	agent := NewClaudeAgent()
	got := agent.Name()
	want := "claude"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestClaudeAgent_IsAvailable(t *testing.T) {
	agent := NewClaudeAgent()
	err := agent.IsAvailable()

	// Check if claude is in PATH
	_, lookPathErr := exec.LookPath("claude")

	if lookPathErr != nil {
		// Claude not in PATH - should return error
		if err == nil {
			t.Error("IsAvailable() should return error when claude is not in PATH")
		}
		if !strings.Contains(err.Error(), "claude CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'claude CLI not found'", err)
		}
	} else {
		// Claude is in PATH - should return nil
		if err != nil {
			t.Errorf("IsAvailable() unexpected error = %v", err)
		}
	}
}

func TestClaudeAgent_ExecuteReview_ClaudeNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure claude is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewClaudeAgent()
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "main",
		WorkDir: ".",
	}

	reader, err := agent.ExecuteReview(ctx, config)
	if err == nil {
		if reader != nil {
			if closer, ok := reader.(io.Closer); ok {
				closer.Close()
			}
		}
		t.Error("ExecuteReview() should return error when claude is not available")
	}

	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'claude CLI not found'", err)
	}
}

func TestClaudeAgentInterface(t *testing.T) {
	var _ Agent = (*ClaudeAgent)(nil)
}
