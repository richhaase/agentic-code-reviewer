package repos

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTrustedSource_RejectsNonReviewableRepository(t *testing.T) {
	for _, status := range []Status{StatusMissing, StatusAmbiguous, StatusExcluded, StatusInvalid} {
		resolved := ResolvedRepository{Identity: Identity{Owner: "acme", Name: "widgets"}, Status: status}
		if _, err := TrustedSource(context.Background(), resolved); err == nil {
			t.Errorf("expected an error for status %s", status)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
	return string(out)
}

func TestTrustedSource_ResolvesFromTheRepositoryItselfWithNoNetworkAccess(t *testing.T) {
	bareDir := filepath.Join(t.TempDir(), "upstream.git")
	runGit(t, ".", "init", "--bare", "-q", bareDir)

	workDir := t.TempDir()
	runGit(t, workDir, "init", "-q")
	runGit(t, workDir, "checkout", "-q", "-b", "main")
	runGit(t, workDir, "commit", "-q", "--allow-empty", "-m", "init",
		"--author=Test <test@example.com>")
	runGit(t, workDir, "remote", "add", "origin", bareDir)
	runGit(t, workDir, "push", "-q", "origin", "main")
	runGit(t, bareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	resolved := ResolvedRepository{
		Identity:  Identity{Owner: "acme", Name: "widgets"},
		Status:    StatusReviewable,
		LocalPath: workDir,
		Remote:    "origin",
	}

	source, err := TrustedSource(context.Background(), resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := source.LoadWithWarnings(context.Background())
	if err != nil {
		t.Fatalf("unexpected error loading from trusted source: %v", err)
	}
	if result.Source.Locator != workDir {
		t.Errorf("expected trusted source to resolve from the repository root %q, got %q", workDir, result.Source.Locator)
	}
	if result.Source.Ref != "refs/acr/trusted-config/origin/main" {
		t.Errorf("expected trusted source to resolve the remote default branch, got ref %q", result.Source.Ref)
	}
}
