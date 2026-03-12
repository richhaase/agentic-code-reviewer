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
	agent := NewCodexAgent("")
	if agent == nil {
		t.Fatal("NewCodexAgent() returned nil")
	}
}

func TestCodexAgent_Name(t *testing.T) {
	agent := NewCodexAgent("")
	got := agent.Name()
	want := "codex"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestCodexAgent_IsAvailable(t *testing.T) {
	agent := NewCodexAgent("")
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

	agent := NewCodexAgent("")
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

	agent := NewCodexAgent("")
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

	agent := NewCodexAgent("")
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

	// Set up a git repo so executeDiffBasedReview can fetch a diff
	for _, cmd := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v failed: %v\n%s", cmd, err, out)
		}
	}

	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		c := exec.CommandContext(context.Background(), cmd[0], cmd[1:]...)
		c.Dir = tmpDir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit %v failed: %v\n%s", cmd, err, out)
		}
	}

	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock codex that prints args and stdin
	mockScript := filepath.Join(tmpDir, "codex")
	err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"ARG:$arg\"; done\ncat\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewCodexAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:  "HEAD",
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

	outputStr := string(output)

	// With guidance, should use diff-based review (no "review" or "--base" args)
	if strings.Contains(outputStr, "ARG:review") {
		t.Errorf("with guidance, should not use built-in 'review' subcommand, got:\n%s", outputStr)
	}
	if strings.Contains(outputStr, "ARG:--base") {
		t.Errorf("with guidance, should not use --base flag, got:\n%s", outputStr)
	}
	// Should use stdin mode
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - flag (stdin mode) in args, got:\n%s", outputStr)
	}
	// Should include guidance in the rendered prompt
	if !strings.Contains(outputStr, "Focus on security issues") {
		t.Errorf("expected guidance in stdin prompt, got:\n%s", outputStr)
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

	agent := NewCodexAgent("")
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
