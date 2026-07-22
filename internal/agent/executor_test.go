package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCappedBuffer_UnderLimit(t *testing.T) {
	buf := newCappedBuffer(100)
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}
	if buf.String() != "hello" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello")
	}
}

func TestCappedBuffer_AtLimit(t *testing.T) {
	buf := newCappedBuffer(5)
	buf.Write([]byte("hello"))

	n, err := buf.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Errorf("Write() should report full length even when discarded, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello")
	}
}

func TestCappedBuffer_PartialWrite(t *testing.T) {
	buf := newCappedBuffer(8)
	buf.Write([]byte("hello"))

	n, err := buf.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Errorf("Write() should report full length to avoid io.ErrShortWrite, got %d", n)
	}
	if buf.String() != "hello wo" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello wo")
	}
}

func TestCappedBuffer_ZeroLimit(t *testing.T) {
	buf := newCappedBuffer(0)
	n, err := buf.Write([]byte("anything"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 8 {
		t.Errorf("Write() should report full length, got %d", n)
	}
	if buf.String() != "" {
		t.Errorf("String() = %q, want empty", buf.String())
	}
}

func TestExecuteCommand_StartFailureCleansTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	tempPath := filepath.Join(tmpDir, ".acr-summary-input.json-test")
	if err := os.WriteFile(tempPath, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand(context.Background(), executeOptions{
		Command:      "definitely-not-a-real-acr-command",
		TempFilePath: tempPath,
	})
	if err == nil {
		t.Fatal("expected executeCommand to fail")
	}
	if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp file to be removed, stat err: %v", statErr)
	}
}

func TestExecuteCommand_StartFailureIncludesCleanupFailure(t *testing.T) {
	cleanupPath := filepath.Join(t.TempDir(), "not-empty")
	if err := os.Mkdir(cleanupPath, 0700); err != nil {
		t.Fatalf("create cleanup directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanupPath, "child"), []byte("data"), 0600); err != nil {
		t.Fatalf("create cleanup child: %v", err)
	}

	_, err := executeCommand(context.Background(), executeOptions{
		Command:      "definitely-not-a-real-acr-command",
		TempFilePath: cleanupPath,
	})

	if err == nil {
		t.Fatal("expected executeCommand to fail")
	}
	if !strings.Contains(err.Error(), "failed to start") || !strings.Contains(err.Error(), "failed to clean up temp file") {
		t.Fatalf("combined setup and cleanup error = %v", err)
	}
}

func TestExecuteCommand_CloseReturnsCleanupFailure(t *testing.T) {
	cleanupPath := filepath.Join(t.TempDir(), "not-empty")
	if err := os.Mkdir(cleanupPath, 0700); err != nil {
		t.Fatalf("create cleanup directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanupPath, "child"), []byte("data"), 0600); err != nil {
		t.Fatalf("create cleanup child: %v", err)
	}

	result, err := executeCommand(context.Background(), executeOptions{
		Command:      "true",
		TempFilePath: cleanupPath,
	})
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if _, err := io.ReadAll(result); err != nil {
		t.Fatalf("read command output: %v", err)
	}
	firstErr := result.Close()

	if firstErr == nil || !strings.Contains(firstErr.Error(), "failed to clean up temp file") {
		t.Fatalf("first Close() error = %v", firstErr)
	}
}
