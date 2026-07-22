package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAntigravityAgent(t *testing.T) {
	agent := NewAntigravityAgent("")
	if agent == nil {
		t.Fatal("NewAntigravityAgent() returned nil")
	}
}

func TestAntigravityAgent_Name(t *testing.T) {
	agent := NewAntigravityAgent("")
	got := agent.Name()
	want := "agy"
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAntigravityAgent_IsAvailable(t *testing.T) {
	agent := NewAntigravityAgent("")
	err := agent.IsAvailable()

	_, lookPathErr := exec.LookPath("agy")

	if lookPathErr != nil {
		if err == nil {
			t.Error("IsAvailable() should return error when agy is not in PATH")
		}
		if !strings.Contains(err.Error(), "agy CLI not found") {
			t.Errorf("IsAvailable() error = %v, want error containing 'agy CLI not found'", err)
		}
	} else if err != nil {
		t.Errorf("IsAvailable() unexpected error = %v", err)
	}
}

func TestAntigravityAgent_ExecuteReview_AgyNotAvailable(t *testing.T) {
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewAntigravityAgent("")
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
		t.Error("ExecuteReview() should return error when agy is not available")
	}

	if !strings.Contains(err.Error(), "agy CLI not found") {
		t.Errorf("ExecuteReview() error = %v, want error containing 'agy CLI not found'", err)
	}
}

func TestAntigravityAgentInterface(t *testing.T) {
	var _ Agent = (*AntigravityAgent)(nil)
}

func TestAntigravityAgent_ExecuteSummary_AgyNotAvailable(t *testing.T) {
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewAntigravityAgent("")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, &SummaryConfig{Prompt: "test prompt", Input: []byte(`{"findings":[]}`)})
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Error("ExecuteSummary() should return error when agy is not available")
	}

	if !strings.Contains(err.Error(), "agy CLI not found") {
		t.Errorf("ExecuteSummary() error = %v, want error containing 'agy CLI not found'", err)
	}
}

func TestAntigravityAgent_ExecuteReview_Args(t *testing.T) {
	tmpDir := t.TempDir()
	initTestRepo(t, tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mockScript := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"ARG:$arg\"; done\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewAntigravityAgent("")
	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef: "HEAD",
		Timeout: 10 * time.Minute,
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
	if !strings.Contains(outputStr, "ARG:--print=-\nARG:--print-timeout\nARG:605s\n") {
		t.Errorf("expected agy print stdin args with configured timeout, got:\n%s", outputStr)
	}
}

func TestAntigravityPrintArgs_FormatsTimeoutForCLI(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    []string
	}{
		{
			name:    "whole minutes",
			timeout: 10 * time.Minute,
			want:    []string{"--print=-", "--print-timeout", "10m"},
		},
		{
			name:    "whole seconds",
			timeout: 90 * time.Second,
			want:    []string{"--print=-", "--print-timeout", "90s"},
		},
		{
			name:    "fractional seconds round up",
			timeout: 1500 * time.Millisecond,
			want:    []string{"--print=-", "--print-timeout", "2s"},
		},
		{
			name:    "default",
			timeout: 0,
			want:    []string{"--print=-", "--print-timeout", "30m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := antigravityPrintArgs(tt.timeout)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestAntigravityPrintTimeoutCeilingFromContext(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	t.Run("uses deadline plus grace so context controls timeout", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), now.Add(10*time.Minute))
		defer cancel()

		got := antigravityPrintTimeoutCeilingFromContext(ctx, now)
		want := 10*time.Minute + antigravityPrintTimeoutGrace
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("no deadline uses default", func(t *testing.T) {
		got := antigravityPrintTimeoutCeilingFromContext(context.Background(), now)
		if got != 0 {
			t.Fatalf("got %s, want 0", got)
		}
	})

	t.Run("expired deadline uses minimum", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), now.Add(-time.Second))
		defer cancel()

		got := antigravityPrintTimeoutCeilingFromContext(ctx, now)
		if got != time.Second {
			t.Fatalf("got %s, want 1s", got)
		}
	})
}

func TestAntigravityAgent_ExecuteReview_RefFileMode(t *testing.T) {
	tmpDir := t.TempDir()
	initTestRepo(t, tmpDir)

	testFile := filepath.Join(tmpDir, "test.go")
	bigContent := strings.Repeat("var value = 1\n", RefFileSizeThreshold/14+1)
	if err := os.WriteFile(testFile, []byte(bigContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockScript := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\ncat\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewAntigravityAgent("")
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
		t.Errorf("expected ref-file path in prompt, got:\n%s", outputStr[:min(200, len(outputStr))])
	}

	result.Close()
	matches, _ := filepath.Glob(filepath.Join(tmpDir, ".acr-diff-*"))
	if len(matches) > 0 {
		t.Errorf("temp diff file not cleaned up: %v", matches)
	}
}

func TestAntigravityAgent_ExecuteSummary_Args(t *testing.T) {
	tmpDir := t.TempDir()
	mockScript := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(mockScript, []byte("#!/bin/sh\nfor arg in \"$@\"; do echo \"ARG:$arg\"; done\ncat\n"), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", tmpDir+":"+originalPath)

	agent := NewAntigravityAgent("ignored-model")
	ctx := context.Background()

	result, err := agent.ExecuteSummary(ctx, &SummaryConfig{Prompt: "summarize", Input: []byte(`{"findings":[]}`), WorkDir: tmpDir})
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "ARG:--print=-\nARG:--print-timeout\nARG:30m\n") {
		t.Errorf("expected agy print stdin args with default timeout, got:\n%s", outputStr)
	}
	if strings.Contains(outputStr, "ignored-model") {
		t.Errorf("agy should not receive unsupported model arg, got:\n%s", outputStr)
	}
	if !strings.Contains(outputStr, "INPUT JSON:") {
		t.Errorf("expected input JSON in stdin, got:\n%s", outputStr)
	}
}

func initTestRepo(t *testing.T, tmpDir string) {
	t.Helper()

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
}
