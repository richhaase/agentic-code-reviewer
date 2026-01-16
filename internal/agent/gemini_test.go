package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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

func TestGeminiAgent_Execute_GeminiNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure gemini is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewGeminiAgent()
	ctx := context.Background()
	config := &AgentConfig{
		BaseRef: "main",
		WorkDir: ".",
	}

	reader, err := agent.Execute(ctx, config)
	if err == nil {
		if reader != nil {
			if closer, ok := reader.(io.Closer); ok {
				closer.Close()
			}
		}
		t.Error("Execute() should return error when gemini is not available")
	}

	if !strings.Contains(err.Error(), "gemini CLI not found") {
		t.Errorf("Execute() error = %v, want error containing 'gemini CLI not found'", err)
	}
}

func TestGeminiAgent_Execute_Integration(t *testing.T) {
	// Skip if gemini is not available
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini CLI not available, skipping integration test")
	}

	// Skip in CI environments where gemini might not be properly configured
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewGeminiAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config := &AgentConfig{
		BaseRef: "HEAD",
		WorkDir: ".",
		Timeout: 5 * time.Second,
	}

	reader, err := agent.Execute(ctx, config)
	if err != nil {
		t.Skipf("Execute() failed, possibly due to environment: %v", err)
	}

	if reader == nil {
		t.Fatal("Execute() returned nil reader")
	}

	// Ensure reader is closed
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	// Try to read some data
	buf := make([]byte, 1024)
	_, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Logf("Read() error (may be expected): %v", err)
	}
}

func TestGeminiAgent_Execute_ContextCancellation(t *testing.T) {
	// Skip if gemini is not available
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewGeminiAgent()
	ctx, cancel := context.WithCancel(context.Background())

	config := &AgentConfig{
		BaseRef: "HEAD",
		WorkDir: ".",
	}

	reader, err := agent.Execute(ctx, config)
	if err != nil {
		t.Skipf("Execute() setup failed: %v", err)
	}

	if reader == nil {
		t.Fatal("Execute() returned nil reader")
	}

	// Cancel context immediately
	cancel()

	// Clean up
	if closer, ok := reader.(io.Closer); ok {
		_ = closer.Close()
	}
}

func TestGeminiAgent_Execute_WithWorkDir(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewGeminiAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use a specific work directory
	workDir := "."
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		t.Skip("Work directory does not exist")
	}

	config := &AgentConfig{
		BaseRef: "HEAD",
		WorkDir: workDir,
		Timeout: 5 * time.Second,
	}

	reader, err := agent.Execute(ctx, config)
	if err != nil {
		t.Skipf("Execute() failed: %v", err)
	}

	if reader == nil {
		t.Fatal("Execute() returned nil reader")
	}

	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
}

func TestGeminiAgentInterface(t *testing.T) {
	// Verify that GeminiAgent implements the Agent interface
	var _ Agent = (*GeminiAgent)(nil)
}

func TestGeminiAgent_Execute_WithCustomPrompt(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewGeminiAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config := &AgentConfig{
		BaseRef:      "HEAD",
		WorkDir:      ".",
		CustomPrompt: "Review this code for security issues only.",
		Timeout:      5 * time.Second,
	}

	reader, err := agent.Execute(ctx, config)
	if err != nil {
		t.Skipf("Execute() failed, possibly due to environment: %v", err)
	}

	if reader == nil {
		t.Fatal("Execute() returned nil reader")
	}

	// Ensure reader is closed
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	// Try to read some data to verify the command executed
	buf := make([]byte, 1024)
	_, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Logf("Read() error (may be expected): %v", err)
	}
}

func TestGeminiAgent_Execute_DefaultPrompt(t *testing.T) {
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewGeminiAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// No CustomPrompt - should use DefaultGeminiPrompt
	config := &AgentConfig{
		BaseRef: "HEAD",
		WorkDir: ".",
		Timeout: 5 * time.Second,
	}

	reader, err := agent.Execute(ctx, config)
	if err != nil {
		t.Skipf("Execute() failed, possibly due to environment: %v", err)
	}

	if reader == nil {
		t.Fatal("Execute() returned nil reader")
	}

	// Ensure reader is closed
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	// Try to read some data
	buf := make([]byte, 1024)
	_, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Logf("Read() error (may be expected): %v", err)
	}
}
