package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewGeminiAgent(t *testing.T) {
	agent := NewGeminiAgent()
	if agent == nil {
		t.Fatal("NewGeminiAgent() returned nil")
	}
}

func TestGeminiAgent_Name(t *testing.T) {
	agent := NewGeminiAgent()
	got := agent.Name()
	want := "gemini"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestGeminiAgent_IsAvailable(t *testing.T) {
	agent := NewGeminiAgent()
	err := agent.IsAvailable()

	// Check if gemini is in PATH
	_, lookPathErr := exec.LookPath("gemini")

	if lookPathErr != nil {
		// Gemini not in PATH - should return error
		if err == nil {
			t.Error("IsAvailable() should return error when gemini is not in PATH")
		}
		if !strings.Contains(err.Error(), "gemini CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'gemini CLI not found'", err)
		}
	} else {
		// Gemini is in PATH - should return nil
		if err != nil {
			t.Errorf("IsAvailable() unexpected error = %v", err)
		}
	}
}

func TestGeminiAgent_ExecuteReview_GeminiNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure gemini is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewGeminiAgent()
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
		t.Error("ExecuteReview() should return error when gemini is not available")
	}

	if !strings.Contains(err.Error(), "gemini CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'gemini CLI not found'", err)
	}
}

func TestGeminiAgentInterface(t *testing.T) {
	var _ Agent = (*GeminiAgent)(nil)
}
