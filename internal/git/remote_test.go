package git

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAddRemote_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Verify remote was added
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git remote -v failed: %v", err)
	}
	if !strings.Contains(string(out), "fork-testuser") {
		t.Error("remote 'fork-testuser' not found in git remote -v output")
	}
}

func TestAddRemote_AlreadyExists(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Add remote first time
	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("first AddRemote failed: %v", err)
	}

	// Add same remote again should fail
	err = AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err == nil {
		t.Error("expected error when adding duplicate remote")
	}
}

func TestRemoveRemote_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Add remote first
	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Remove it
	err = RemoveRemote(repoDir, "fork-testuser")
	if err != nil {
		t.Fatalf("RemoveRemote failed: %v", err)
	}

	// Verify remote was removed
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	if strings.Contains(string(out), "fork-testuser") {
		t.Error("remote 'fork-testuser' should have been removed")
	}
}

func TestRemoveRemote_NotExists(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Remove non-existent remote should fail
	err := RemoveRemote(repoDir, "nonexistent-remote")
	if err == nil {
		t.Error("expected error when removing non-existent remote")
	}
}
