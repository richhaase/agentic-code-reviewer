package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewCodexAgent(t *testing.T) {
	agent := NewCodexAgent()
	if agent == nil {
		t.Fatal("NewCodexAgent() returned nil")
	}
}

func TestCodexAgent_Name(t *testing.T) {
	agent := NewCodexAgent()
	got := agent.Name()
	want := "codex"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestCodexAgent_IsAvailable(t *testing.T) {
	agent := NewCodexAgent()
	err := agent.IsAvailable()

	// Check if codex is in PATH
	_, lookPathErr := exec.LookPath("codex")

	if lookPathErr != nil {
		// Codex not in PATH - should return error
		if err == nil {
			t.Error("IsAvailable() should return error when codex is not in PATH")
		}
		if !strings.Contains(err.Error(), "codex CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'codex CLI not found'", err)
		}
	} else {
		// Codex is in PATH - should return nil
		if err != nil {
			t.Errorf("IsAvailable() unexpected error = %v", err)
		}
	}
}

func TestCodexAgent_ExecuteReview_CodexNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure codex is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewCodexAgent()
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
		t.Error("ExecuteReview() should return error when codex is not available")
	}

	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'codex CLI not found'", err)
	}
}

func TestAgentInterface(t *testing.T) {
	var _ Agent = (*CodexAgent)(nil)
}

func TestCodexAgent_ExecuteSummary_CodexNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure codex is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewCodexAgent()
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "test prompt", []byte(`{"findings":[]}`))
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when codex is not available")
	}

	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'codex CLI not found'", err)
	}
}

func TestBuildCodexRefFilePrompt(t *testing.T) {
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
				"Read the file contents",
			},
			wantMissing: []string{},
		},
		{
			name:         "empty custom prompt uses default ref-file prompt",
			customPrompt: "",
			diffPath:     "/tmp/test.patch",
			wantContains: []string{
				"/tmp/test.patch",
				"Read the file contents",
				"You are a code reviewer", // from default ref-file prompt
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
				"You are a code reviewer", // should NOT contain default prompt
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := BuildCodexRefFilePrompt(tt.customPrompt, tt.diffPath)

			for _, want := range tt.wantContains {
				if !strings.Contains(prompt, want) {
					t.Errorf("BuildCodexRefFilePrompt() prompt missing expected content %q\nGot: %s", want, prompt)
				}
			}

			for _, notWant := range tt.wantMissing {
				if strings.Contains(prompt, notWant) {
					t.Errorf("BuildCodexRefFilePrompt() prompt should not contain %q\nGot: %s", notWant, prompt)
				}
			}
		})
	}
}
