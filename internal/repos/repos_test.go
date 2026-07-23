package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func initGitRepoWithRemote(t *testing.T, dir, remoteURL string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remoteURL).CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}
}

func TestResolve_FindsReviewableRepository(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %d: %+v", len(resolution.Repositories), resolution.Repositories)
	}
	got := resolution.Repositories[0]
	if got.Identity != (Identity{Owner: "acme", Name: "widgets"}) {
		t.Errorf("unexpected identity: %+v", got.Identity)
	}
	if got.Status != StatusReviewable {
		t.Errorf("expected reviewable, got %s", got.Status)
	}
	if got.LocalPath != filepath.Join(root, "widgets") {
		t.Errorf("unexpected local path: %s", got.LocalPath)
	}
	if got.Remote != "origin" {
		t.Errorf("expected remote origin, got %s", got.Remote)
	}
}

func TestResolve_TwoClonesOfSameRepoAreAmbiguous(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-a"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-b"), "git@github.com:acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository entry, got %d", len(resolution.Repositories))
	}
	if resolution.Repositories[0].Status != StatusAmbiguous {
		t.Fatalf("expected ambiguous, got %s", resolution.Repositories[0].Status)
	}
	if resolution.Repositories[0].LocalPath != "" {
		t.Errorf("ambiguous entries must not pick a local path heuristically, got %q", resolution.Repositories[0].LocalPath)
	}
}

func TestResolve_PathOverrideMissing(t *testing.T) {
	root := t.TempDir()

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/gone": filepath.Join(root, "does-not-exist")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusMissing {
		t.Fatalf("expected a single missing repository, got %+v", resolution.Repositories)
	}
	if resolution.Repositories[0].Reason == "" {
		t.Error("expected a non-empty reason")
	}
}

func TestResolve_PathOverrideInvalidWithoutOriginRemote(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "no-origin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/no-origin": dir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusInvalid {
		t.Fatalf("expected invalid status for a checkout without an origin remote, got %+v", resolution.Repositories)
	}
}

func TestResolve_OverlappingRootsDoNotFalselyReportAmbiguity(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "widgets")
	initGitRepoWithRemote(t, repoDir, "https://github.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root, repoDir, root},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected exactly 1 repository entry, got %d: %+v", len(resolution.Repositories), resolution.Repositories)
	}
	if resolution.Repositories[0].Status != StatusReviewable {
		t.Fatalf("expected reviewable, got %s (%s)", resolution.Repositories[0].Status, resolution.Repositories[0].Reason)
	}
}

func TestResolve_RejectsMalformedExcludePattern(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")

	_, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Exclude:         []string{"acme/[widgets"},
	})
	if err == nil {
		t.Fatal("expected an error for a malformed exclude pattern")
	}
}

func TestResolve_RejectsMalformedIncludePattern(t *testing.T) {
	_, err := Resolve(context.Background(), workspace.ScopeConfig{
		Include: []string{"acme/[widgets"},
	})
	if err == nil {
		t.Fatal("expected an error for a malformed include pattern")
	}
}

func TestResolve_PathOverrideInvalidNotAGitRepo(t *testing.T) {
	root := t.TempDir()
	plainDir := filepath.Join(root, "plain")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/plain": plainDir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusInvalid {
		t.Fatalf("expected a single invalid repository, got %+v", resolution.Repositories)
	}
}

func TestResolve_PathOverrideWinsOverAmbiguousDiscovery(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-a"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-b"), "https://github.com/acme/widgets.git")
	override := filepath.Join(root, "widgets-a")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		PathOverrides:   map[string]string{"acme/widgets": override},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository entry, got %d", len(resolution.Repositories))
	}
	got := resolution.Repositories[0]
	if got.Status != StatusReviewable {
		t.Fatalf("expected the explicit override to win and be reviewable, got %s (%s)", got.Status, got.Reason)
	}
	if got.LocalPath != override {
		t.Errorf("expected override path to win, got %q", got.LocalPath)
	}
}

func TestResolve_ExcludePatternWins(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Exclude:         []string{"acme/*"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusExcluded {
		t.Fatalf("expected excluded, got %+v", resolution.Repositories)
	}
}

func TestResolve_IncludeListExcludesUnlisted(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "gizmos"), "https://github.com/acme/gizmos.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Include:         []string{"acme/widgets"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	statuses := map[string]Status{}
	for _, r := range resolution.Repositories {
		statuses[r.Identity.String()] = r.Status
	}
	if statuses["acme/widgets"] != StatusReviewable {
		t.Errorf("expected acme/widgets to be reviewable, got %s", statuses["acme/widgets"])
	}
	if statuses["acme/gizmos"] != StatusExcluded {
		t.Errorf("expected acme/gizmos to be excluded, got %s", statuses["acme/gizmos"])
	}
}

func TestResolve_ExcludeWinsOverInclude(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Include:         []string{"acme/widgets"},
		Exclude:         []string{"acme/widgets"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusExcluded {
		t.Fatalf("expected exclude to take precedence over include, got %+v", resolution.Repositories)
	}
}

func TestResolve_MissingRootProducesRootWarningNotRepositoryStatus(t *testing.T) {
	root := t.TempDir()
	missingRoot := filepath.Join(root, "does-not-exist")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{missingRoot}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 0 {
		t.Fatalf("expected no repository entries, got %+v", resolution.Repositories)
	}
	if len(resolution.RootWarnings) != 1 {
		t.Fatalf("expected 1 root warning, got %+v", resolution.RootWarnings)
	}
}

func TestResolve_NonGitDirectoryIsIgnoredNotReported(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 0 {
		t.Fatalf("expected no repository entries for an unconfigured plain directory, got %+v", resolution.Repositories)
	}
}

func TestResolve_ResultsAreSortedByIdentity(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "zeta"), "https://github.com/acme/zeta.git")
	initGitRepoWithRemote(t, filepath.Join(root, "alpha"), "https://github.com/acme/alpha.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(resolution.Repositories))
	}
	if resolution.Repositories[0].Identity.String() != "acme/alpha" || resolution.Repositories[1].Identity.String() != "acme/zeta" {
		t.Fatalf("expected sorted order, got %+v", resolution.Repositories)
	}
}
