package agent

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

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

func TestCmdReader_CloseWithNilProcess(t *testing.T) {
	// cmdReader with cmd set but Process is nil (Start() was never called)
	cmd := exec.Command("true") // Don't start it

	reader := &cmdReader{
		Reader: strings.NewReader(""),
		cmd:    cmd,
		ctx:    context.Background(),
	}

	// Should not panic
	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdReader_CloseWithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a long-running command
	cmd := exec.CommandContext(ctx, "sleep", "10")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, _ := cmd.StdoutPipe()
	_ = cmd.Start()

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}

	// Should kill process group and not panic
	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
