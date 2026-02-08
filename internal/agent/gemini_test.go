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

	result, err := agent.ExecuteReview(ctx, config)
	if err == nil {
		if result != nil {
			result.Close()
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

func TestGeminiAgent_ExecuteSummary_GeminiNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure gemini is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewGeminiAgent()
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "test prompt", []byte(`{"findings":[]}`))
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when gemini is not available")
	}

	if !strings.Contains(err.Error(), "gemini CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'gemini CLI not found'", err)
	}
}

func TestGeminiAgent_ExecuteReview_Args(t *testing.T) {
	tmpDir := t.TempDir()

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

	mockScript := filepath.Join(tmpDir, "gemini")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"ARG:$arg\"; done\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewGeminiAgent()
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
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

	outputStr := string(output)
	if !strings.Contains(outputStr, "ARG:-o") {
		t.Errorf("expected -o flag in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:json") {
		t.Errorf("expected json in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - flag (stdin mode) in args, got:\n%s", outputStr)
	}
}

func TestGeminiAgent_ExecuteReview_RefFileMode(t *testing.T) {
	tmpDir := t.TempDir()

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

	bigContent := strings.Repeat("// line of code\n", RefFileSizeThreshold/16+1)
	if err := os.WriteFile(testFile, []byte(bigContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockScript := filepath.Join(tmpDir, "gemini")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\ncat\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewGeminiAgent()
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
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

	outputStr := string(output)
	if !strings.Contains(outputStr, ".acr-diff-") {
		t.Errorf("expected ref-file path in prompt for large diff, got:\n%s", outputStr[:min(200, len(outputStr))])
	}

	result.Close()
	matches, _ := filepath.Glob(filepath.Join(tmpDir, ".acr-diff-*"))
	if len(matches) > 0 {
		t.Errorf("temp diff file not cleaned up: %v", matches)
	}
}

func TestGeminiAgent_ExecuteSummary_Args(t *testing.T) {
	tmpDir := t.TempDir()
	mockScript := filepath.Join(tmpDir, "gemini")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"ARG:$arg\"; done\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewGeminiAgent()
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, "summarize", []byte(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "ARG:-o") {
		t.Errorf("expected -o in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:json") {
		t.Errorf("expected json in args, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "ARG:-") {
		t.Errorf("expected - (stdin) in args, got:\n%s", outputStr)
	}
}
