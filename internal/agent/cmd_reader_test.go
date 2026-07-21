package agent

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCmdReader_Close(t *testing.T) {

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

	if err := reader.Close(); err != nil {
		t.Errorf("First Close() error = %v, want nil", err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Second Close() error = %v, want nil", err)
	}
}

func TestCmdReader_CloseReturnsStoredCleanupError(t *testing.T) {
	cleanupPath := filepath.Join(t.TempDir(), "not-empty")
	if err := os.Mkdir(cleanupPath, 0700); err != nil {
		t.Fatalf("create cleanup directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanupPath, "child"), []byte("data"), 0600); err != nil {
		t.Fatalf("create cleanup child: %v", err)
	}
	reader := &cmdReader{Reader: strings.NewReader(""), tempFilePath: cleanupPath}

	firstErr := reader.Close()
	secondErr := reader.Close()

	if firstErr == nil || !strings.Contains(firstErr.Error(), "failed to clean up temp file") {
		t.Fatalf("first Close() error = %v", firstErr)
	}
	if secondErr == nil || secondErr.Error() != firstErr.Error() {
		t.Fatalf("second Close() error = %v, want %v", secondErr, firstErr)
	}
}

func TestCmdReader_CloseWithNilProcess(t *testing.T) {

	cmd := exec.Command("true")

	reader := &cmdReader{
		Reader: strings.NewReader(""),
		cmd:    cmd,
		ctx:    context.Background(),
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdReader_CloseWithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := exec.CommandContext(ctx, "sleep", "10")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, _ := cmd.StdoutPipe()
	_ = cmd.Start()

	reader := &cmdReader{
		Reader: stdout,
		cmd:    cmd,
		ctx:    ctx,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
