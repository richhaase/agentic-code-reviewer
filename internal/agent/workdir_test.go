package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewIsolatedWorkDirCreatesAndCleansDirectory(t *testing.T) {
	dir, cleanup, err := NewIsolatedWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("cleanup is nil")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("work path %q is not a directory", dir)
	}
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("git worktree status = %q", out)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("work directory remains after cleanup: %v", err)
	}
}
