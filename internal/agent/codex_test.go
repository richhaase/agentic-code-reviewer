package agent

import (
	"context"
	"io"
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

func TestAgentInterface(t *testing.T) {
	var _ Agent = (*CodexAgent)(nil)
}

func TestCmdReaderExitCoderInterface(t *testing.T) {
	var _ ExitCoder = (*cmdReader)(nil)
}

func TestCmdReader_ExitCode_Success(t *testing.T) {
	cmd := exec.Command("true")
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

	_, _ = io.ReadAll(reader)
	_ = reader.Close()

	if got := reader.ExitCode(); got != 0 {
		t.Errorf("ExitCode() = %d, want 0", got)
	}
}

func TestCmdReader_ExitCode_Failure(t *testing.T) {
	cmd := exec.Command("false")
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

	_, _ = io.ReadAll(reader)
	_ = reader.Close()

	if got := reader.ExitCode(); got == 0 {
		t.Error("ExitCode() = 0, want non-zero for failed command")
	}
}

func TestCmdReader_Close_Idempotent(t *testing.T) {
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

	_, _ = io.ReadAll(reader)

	// Close multiple times should not error
	if err := reader.Close(); err != nil {
		t.Errorf("First Close() error = %v, want nil", err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Second Close() error = %v, want nil", err)
	}
}
