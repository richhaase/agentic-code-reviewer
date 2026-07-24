package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if got.Identity != (Identity{Host: DefaultHost, Owner: "acme", Name: "widgets"}) {
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

func TestResolve_PathOverrideInvalidWithoutAnyRemote(t *testing.T) {
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
		t.Fatalf("expected invalid status for a checkout without any remote, got %+v", resolution.Repositories)
	}
}

func TestResolve_PathOverrideInvalidWhenOriginDoesNotMatchIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "gizmos")
	initGitRepoWithRemote(t, dir, "https://github.com/acme/gizmos.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/widgets": dir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusInvalid {
		t.Fatalf("expected a typo'd override pointing at a different repo to be invalid, got %+v", resolution.Repositories)
	}
}

func TestResolve_PathOverrideMatchesNonOriginRemote(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fork")
	initGitRepoWithRemote(t, dir, "https://github.com/my-fork/widgets.git")
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "upstream", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/widgets": dir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %+v", resolution.Repositories)
	}
	got := resolution.Repositories[0]
	if got.Status != StatusReviewable {
		t.Fatalf("expected the canonical identity to resolve via the upstream remote, got %s (%s)", got.Status, got.Reason)
	}
	if got.Remote != "upstream" {
		t.Errorf("expected the matching remote to be reported as upstream, got %q", got.Remote)
	}
}

func TestResolve_PathOverrideInvalidWhenMultipleRemotesMatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mirrored")
	initGitRepoWithRemote(t, dir, "https://github.com/acme/widgets.git")
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "upstream", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"acme/widgets": dir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 || resolution.Repositories[0].Status != StatusInvalid {
		t.Fatalf("expected multiple matching remotes to fail closed as invalid, got %+v", resolution.Repositories)
	}
}

func TestResolve_SymlinkedRootDoesNotFalselyReportAmbiguity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	initGitRepoWithRemote(t, realDir, "https://github.com/acme/widgets.git")
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{realDir, linkDir},
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

func TestResolve_DiscoversSoleNonOriginRemote(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "widgets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "upstream", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %+v", resolution.Repositories)
	}
	got := resolution.Repositories[0]
	if got.Status != StatusReviewable {
		t.Fatalf("expected reviewable, got %s (%s)", got.Status, got.Reason)
	}
	if got.Remote != "upstream" {
		t.Errorf("expected the sole remote upstream to be used, got %q", got.Remote)
	}
}

func TestResolve_DoesNotGuessAmongMultipleNonOriginRemotes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "widgets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "upstream", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "backup", "https://github.com/other/project.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v: %s", err, out)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 0 {
		t.Fatalf("expected no repository entries when multiple non-origin remotes exist with nothing to disambiguate them, got %+v", resolution.Repositories)
	}
}

func TestResolve_SurfacesGitInspectionFailureAsRootWarning(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 0 {
		t.Fatalf("expected no reviewable repository for a broken git directory, got %+v", resolution.Repositories)
	}
	if len(resolution.RootWarnings) == 0 {
		t.Fatal("expected a root warning surfacing the broken git directory instead of silently ignoring it")
	}
}

func TestResolve_DiscoversSymlinkedChildRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}

	actualRepo := filepath.Join(t.TempDir(), "actual-widgets")
	initGitRepoWithRemote(t, actualRepo, "https://github.com/acme/widgets.git")

	root := t.TempDir()
	linkPath := filepath.Join(root, "widgets-link")
	if err := os.Symlink(actualRepo, linkPath); err != nil {
		t.Fatal(err)
	}

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository via the symlinked child, got %+v", resolution.Repositories)
	}
	if resolution.Repositories[0].Status != StatusReviewable {
		t.Fatalf("expected reviewable, got %s (%s)", resolution.Repositories[0].Status, resolution.Repositories[0].Reason)
	}
}

func TestResolve_RejectsDuplicateNormalizedPathOverrides(t *testing.T) {
	root := t.TempDir()
	pathA := filepath.Join(root, "a")
	pathB := filepath.Join(root, "b")

	_, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{
			"acme/widgets":            pathA,
			"github.com/acme/widgets": pathB,
		},
	})
	if err == nil {
		t.Fatal("expected an error for two path_overrides keys that normalize to the same identity")
	}
}

func TestIdentity_StringHidesDefaultHostOnly(t *testing.T) {
	githubCom := Identity{Host: DefaultHost, Owner: "acme", Name: "widgets"}
	if githubCom.String() != "acme/widgets" {
		t.Errorf("expected default host to be hidden, got %q", githubCom.String())
	}

	enterprise := Identity{Host: "github.example.com", Owner: "acme", Name: "widgets"}
	if enterprise.String() != "github.example.com/acme/widgets" {
		t.Errorf("expected non-default host to be shown, got %q", enterprise.String())
	}
}

func TestResolve_SameOwnerRepoOnDifferentHostsAreDistinctIdentities(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-github"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-enterprise"), "https://github.example.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{RepositoryRoots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 2 {
		t.Fatalf("expected 2 distinct repositories, got %d: %+v", len(resolution.Repositories), resolution.Repositories)
	}
	for _, r := range resolution.Repositories {
		if r.Status != StatusReviewable {
			t.Errorf("expected %s to be reviewable, got %s (%s)", r.Identity, r.Status, r.Reason)
		}
	}
}

func TestResolve_PathOverrideWithExplicitHost(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "enterprise-widgets"), "https://github.example.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		PathOverrides: map[string]string{"github.example.com/acme/widgets": filepath.Join(root, "enterprise-widgets")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 1 {
		t.Fatalf("expected 1 repository, got %+v", resolution.Repositories)
	}
	got := resolution.Repositories[0]
	if got.Status != StatusReviewable {
		t.Fatalf("expected reviewable, got %s (%s)", got.Status, got.Reason)
	}
	if got.Identity.Host != "github.example.com" {
		t.Errorf("expected host github.example.com, got %q", got.Identity.Host)
	}
}

func TestResolve_ExcludePatternWithTwoSegmentsIsHostAgnostic(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-github"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-enterprise"), "https://github.example.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Exclude:         []string{"acme/widgets"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolution.Repositories) != 2 {
		t.Fatalf("expected 2 repositories, got %+v", resolution.Repositories)
	}
	for _, r := range resolution.Repositories {
		if r.Status != StatusExcluded {
			t.Errorf("expected %s to be excluded by the host-agnostic pattern, got %s", r.Identity, r.Status)
		}
	}
}

func TestResolve_ExcludePatternWithHostOnlyMatchesThatHost(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-github"), "https://github.com/acme/widgets.git")
	initGitRepoWithRemote(t, filepath.Join(root, "widgets-enterprise"), "https://github.example.com/acme/widgets.git")

	resolution, err := Resolve(context.Background(), workspace.ScopeConfig{
		RepositoryRoots: []string{root},
		Exclude:         []string{"github.example.com/acme/widgets"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	statuses := map[string]Status{}
	for _, r := range resolution.Repositories {
		statuses[r.Identity.String()] = r.Status
	}
	if statuses["acme/widgets"] != StatusReviewable {
		t.Errorf("expected the github.com repo to remain reviewable, got %s", statuses["acme/widgets"])
	}
	if statuses["github.example.com/acme/widgets"] != StatusExcluded {
		t.Errorf("expected the enterprise repo to be excluded, got %s", statuses["github.example.com/acme/widgets"])
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
