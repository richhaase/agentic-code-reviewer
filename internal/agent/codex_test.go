package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func TestCodexAgent_ExecuteReview_ArgsWithoutGuidance(t *testing.T) {
	tmpDir := t.TempDir()
	mockScript := filepath.Join(tmpDir, "codex")
	err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"$arg\"; done\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir)

	agent := NewCodexAgent()
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "main",
		WorkDir: tmpDir,
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	expected := []string{"exec", "--json", "--color", "never", "review", "--base", "main"}
	if len(args) != len(expected) {
		t.Fatalf("got %d args %v, want %d args %v", len(args), args, len(expected), expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestCodexAgent_ExecuteReview_ArgsWithGuidance(t *testing.T) {
	tmpDir := t.TempDir()
	mockScript := filepath.Join(tmpDir, "codex")
	err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"$arg\"; done\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir)

	agent := NewCodexAgent()
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:  "develop",
		WorkDir:  tmpDir,
		Guidance: "Focus on security issues",
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	expected := []string{"exec", "--json", "--color", "never", "review", "--base", "develop", "-"}
	if len(args) != len(expected) {
		t.Fatalf("got %d args %v, want %d args %v", len(args), args, len(expected), expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestCodexAgent_ExecuteSummary_Args(t *testing.T) {
	tmpDir := t.TempDir()
	mockScript := filepath.Join(tmpDir, "codex")
	err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"$arg\"; done\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir)

	agent := NewCodexAgent()
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize this", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	args := strings.Split(strings.TrimSpace(string(output)), "\n")
	expected := []string{"exec", "--json", "--color", "never", "-"}
	if len(args) != len(expected) {
		t.Fatalf("got %d args %v, want %d args %v", len(args), args, len(expected), expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}
