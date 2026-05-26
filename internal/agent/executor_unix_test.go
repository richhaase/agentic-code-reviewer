//go:build !windows

package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecuteCommand_ContextCancelKillsProcessGroup(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := executeCommand(ctx, executeOptions{
		Command: "sh",
		Args:    []string{"-c", fmt.Sprintf("sleep 10 & echo $! > %s; wait", strconv.Quote(pidFile))},
	})
	if err != nil {
		t.Fatalf("executeCommand() error: %v", err)
	}
	defer result.Close()

	done := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(result)
		done <- readErr
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reading command output did not unblock after context cancellation")
	}

	result.Close()
	if result.ExitCode() == 0 {
		t.Fatal("expected non-zero exit after context cancellation")
	}

	childPIDBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read child pid file: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(childPIDBytes)))
	if err != nil {
		t.Fatalf("failed to parse child pid: %v", err)
	}
	assertProcessExited(t, childPID)
}

func assertProcessExited(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d still exists after process group cancellation", pid)
}
