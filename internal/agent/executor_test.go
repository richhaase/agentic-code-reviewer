package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestExecuteCommand(t *testing.T) {
	ctx := context.Background()

	result, err := executeCommand(ctx, executeOptions{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if !bytes.Contains(output, []byte("hello")) {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

func TestExecuteCommand_WithStdin(t *testing.T) {
	ctx := context.Background()

	result, err := executeCommand(ctx, executeOptions{
		Command: "cat",
		Stdin:   strings.NewReader("test input"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if string(output) != "test input" {
		t.Errorf("expected 'test input', got: %s", output)
	}
}

func TestExecuteCommand_ExitCode(t *testing.T) {
	ctx := context.Background()

	result, err := executeCommand(ctx, executeOptions{
		Command: "sh",
		Args:    []string{"-c", "exit 42"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain output
	_, _ = io.ReadAll(result)
	result.Close()

	if result.ExitCode() != 42 {
		t.Errorf("expected exit code 42, got: %d", result.ExitCode())
	}
}

func TestExecuteCommand_CapturesStderr(t *testing.T) {
	ctx := context.Background()

	result, err := executeCommand(ctx, executeOptions{
		Command: "sh",
		Args:    []string{"-c", "echo error message >&2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain output
	_, _ = io.ReadAll(result)
	result.Close()

	stderr := result.Stderr()
	if !strings.Contains(stderr, "error message") {
		t.Errorf("expected stderr to contain 'error message', got: %s", stderr)
	}
}

func TestExecuteCommand_InvalidCommand(t *testing.T) {
	ctx := context.Background()

	_, err := executeCommand(ctx, executeOptions{
		Command: "nonexistent-command-12345",
	})
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
	if !strings.Contains(err.Error(), "failed to start") {
		t.Errorf("expected error to mention 'failed to start', got: %v", err)
	}
}

func TestExecuteCommand_WorkDir(t *testing.T) {
	ctx := context.Background()

	result, err := executeCommand(ctx, executeOptions{
		Command: "pwd",
		WorkDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// On macOS, /tmp is a symlink to /private/tmp
	outputStr := strings.TrimSpace(string(output))
	if outputStr != "/tmp" && outputStr != "/private/tmp" {
		t.Errorf("expected working directory '/tmp' or '/private/tmp', got: %s", outputStr)
	}
}

func TestExecuteCommand_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	result, err := executeCommand(ctx, executeOptions{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cancel the context immediately
	cancel()

	// Close should handle the cancellation gracefully
	err = result.Close()
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}

	// The process should have been killed
	// Exit code will be non-zero due to SIGKILL
	exitCode := result.ExitCode()
	if exitCode == 0 {
		t.Error("expected non-zero exit code after cancellation")
	}
}
