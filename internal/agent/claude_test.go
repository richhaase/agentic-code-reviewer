package agent

import (
	"context"
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

	result, err := agent.ExecuteReview(ctx, config)
	if err == nil {
		if result != nil {
			result.Close()
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

func TestClaudeAgent_ExecuteSummary_ClaudeNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure claude is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewClaudeAgent()
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "test prompt", []byte(`{"findings":[]}`))
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when claude is not available")
	}

	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'claude CLI not found'", err)
	}
}

func TestBuildRefFilePrompt(t *testing.T) {
	tests := []struct {
		name         string
		customPrompt string
		diffPath     string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:         "custom prompt is preserved in ref-file mode",
			customPrompt: "My custom security review prompt with special instructions",
			diffPath:     "/tmp/test.patch",
			wantContains: []string{
				"My custom security review prompt with special instructions",
				"/tmp/test.patch",
				"Read tool",
			},
			wantMissing: []string{},
		},
		{
			name:         "empty custom prompt uses default ref-file prompt",
			customPrompt: "",
			diffPath:     "/tmp/test.patch",
			wantContains: []string{
				"/tmp/test.patch",
				"Read tool",
				"Review this git diff for bugs", // from DefaultClaudeRefFilePrompt
			},
			wantMissing: []string{},
		},
		{
			name:         "custom prompt not overwritten by default",
			customPrompt: "CUSTOM_MARKER_12345",
			diffPath:     "/path/to/diff.patch",
			wantContains: []string{
				"CUSTOM_MARKER_12345",
			},
			wantMissing: []string{
				"Review this git diff for bugs", // should NOT contain default prompt
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildRefFilePrompt(tt.customPrompt, tt.diffPath)

			for _, want := range tt.wantContains {
				if !strings.Contains(prompt, want) {
					t.Errorf("BuildRefFilePrompt() prompt missing expected content %q\nGot: %s", want, prompt)
				}
			}

			for _, notWant := range tt.wantMissing {
				if strings.Contains(prompt, notWant) {
					t.Errorf("BuildRefFilePrompt() prompt should not contain %q\nGot: %s", notWant, prompt)
				}
			}
		})
	}
}
