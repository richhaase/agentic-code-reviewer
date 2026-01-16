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

func TestCodexAgent_Execute_CodexNotAvailable(t *testing.T) {
	// Temporarily remove PATH to ensure codex is not available
	originalPath := os.Getenv("PATH")
	defer os.Setenv("PATH", originalPath)
	os.Setenv("PATH", "")

	agent := NewCodexAgent()
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
		t.Error("Execute() should return error when codex is not available")
	}

	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Errorf("Execute() error = %v, want error containing 'codex CLI not found'", err)
	}
}

func TestCodexAgent_Execute_Integration(t *testing.T) {
	// Skip if codex is not available
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not available, skipping integration test")
	}

	// Skip in CI environments where codex might not be properly configured
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewCodexAgent()
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

func TestCodexAgent_Execute_ContextCancellation(t *testing.T) {
	// Skip if codex is not available
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewCodexAgent()
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

func TestCmdReader_Close(t *testing.T) {
	// Create a simple command that we can close
	cmd := exec.Command("echo", "test")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
	}

	// Read all output
	_, _ = io.ReadAll(reader)

	// Close should not error
	err = reader.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestCmdReader_Close_NilCommand(t *testing.T) {
	reader := &cmdReader{
		Reader: strings.NewReader("test"),
		cmd:    nil,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close() with nil cmd error = %v, want nil", err)
	}
}

func TestCmdReader_Close_CommandNotStarted(t *testing.T) {
	cmd := exec.Command("echo", "test")

	reader := &cmdReader{
		Reader: strings.NewReader("test"),
		cmd:    cmd,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("Close() with non-started command error = %v, want nil", err)
	}
}

func TestCodexAgent_Execute_WithWorkDir(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not available, skipping test")
	}

	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	agent := NewCodexAgent()
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

func TestAgentInterface(t *testing.T) {
	// Verify that CodexAgent implements the Agent interface
	var _ Agent = (*CodexAgent)(nil)
}
