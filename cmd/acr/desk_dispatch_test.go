package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func TestCheckDispatchEligible(t *testing.T) {
	cases := []struct {
		name    string
		item    desk.Item
		wantErr bool
	}{
		{"resolved", desk.Item{DeskState: domain.DeskStateResolved}, true},
		{"repository unavailable", desk.Item{DeskState: domain.DeskStateRepositoryUnavailable}, true},
		{"waiting on others", desk.Item{DeskState: domain.DeskStateWaitingOnOthers, Reason: "own pull request"}, true},
		{"running", desk.Item{DeskState: domain.DeskStateRunning, RepositoryPath: "/repo"}, true},
		{"queued", desk.Item{DeskState: domain.DeskStateQueued, RepositoryPath: "/repo"}, true},
		{"needs review is eligible", desk.Item{DeskState: domain.DeskStateNeedsReview, RepositoryPath: "/repo"}, false},
		{"findings ready is eligible", desk.Item{DeskState: domain.DeskStateFindingsReady, RepositoryPath: "/repo"}, false},
		{"no local repository path", desk.Item{DeskState: domain.DeskStateNeedsReview, RepositoryPath: ""}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDispatchEligible(tc.item)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkDispatchEligible() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFindDeskItem(t *testing.T) {
	key := store.PullRequestKeyV1{Host: "fakehub.test", Owner: "acme", Repository: "widgets", Number: 7}
	inbox := desk.Inbox{Items: []desk.Item{{Key: key, Title: "widgets PR"}}}

	if _, ok := findDeskItem(inbox, key); !ok {
		t.Fatal("expected to find the item by key")
	}

	other := key
	other.Number = 8
	if _, ok := findDeskItem(inbox, other); ok {
		t.Fatal("expected not to find a non-matching key")
	}
}

func TestRunDeskDispatch_RejectsUnknownPullRequest(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)
	writeWorkspaceConfig(t, configDir, "me", nil)
	withFakeGH(t, fakeGHResponses{user: "me"})

	key := store.PullRequestKeyV1{Host: "fakehub.test", Owner: "acme", Repository: "widgets", Number: 7}
	discovery := &dispatchFixtureDiscovery{}

	err := runDeskDispatch(context.Background(), key, discovery, unusedDispatchService(t))
	if err == nil {
		t.Fatal("expected an error for a PR not in the current desk view")
	}
}

type dispatchFixtureDiscovery struct {
	searchResult    []domain.PullRequestKey
	enrichResponses []domain.PullRequestSnapshot
	enrichCalls     int
}

func (f *dispatchFixtureDiscovery) Search(ctx context.Context, query github.SearchQuery) ([]domain.PullRequestKey, error) {
	if query.Kind != github.SearchKindReviewRequested {
		return nil, nil
	}
	return f.searchResult, nil
}

func (f *dispatchFixtureDiscovery) Enrich(ctx context.Context, key domain.PullRequestKey) (domain.PullRequestSnapshot, error) {
	if len(f.enrichResponses) == 0 {
		return domain.PullRequestSnapshot{}, errors.New("no fixture enrich response configured")
	}
	idx := f.enrichCalls
	if idx >= len(f.enrichResponses) {
		idx = len(f.enrichResponses) - 1
	}
	f.enrichCalls++
	return f.enrichResponses[idx], nil
}

func unusedDispatchService(t *testing.T) dispatchServiceFactory {
	t.Helper()
	return func(ReviewOpts, *terminal.Logger) (semanticReviewService, error) {
		t.Fatal("review service should not have been invoked")
		return nil, nil
	}
}

func writeWorkspaceConfig(t *testing.T, configDir, expectedUser string, repositoryRoots []string) {
	t.Helper()
	rootsYAML := "[]"
	if len(repositoryRoots) > 0 {
		quoted := make([]string, len(repositoryRoots))
		for i, root := range repositoryRoots {
			quoted[i] = fmt.Sprintf("%q", root)
		}
		rootsYAML = "[" + joinStrings(quoted, ", ") + "]"
	}
	yamlContent := fmt.Sprintf(`schema_version: 1
identity:
  expected_user: %q
scope:
  organizations: []
  teams: []
  repository_roots: %s
  include: []
  exclude: []
  path_overrides: {}
behavior:
  poll_interval: 1m
  settle_time: 10m
  concurrency: 0
  auto_review: false
  re_review: false
  own_pr_policy: disabled
posting:
  enabled: false
notifications:
  enabled: false
`, expectedUser, rootsYAML)

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, workspace.ConfigFileName), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}

type fakeGHResponses struct {
	user        string
	repoURL     string
	repoSSHURL  string
	baseRefName string
}

func withFakeGH(t *testing.T, responses fakeGHResponses) {
	t.Helper()
	binDir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
  "api user")
    echo %q
    ;;
  "repo view")
    echo '{"url":%q,"sshUrl":%q}'
    ;;
  "pr view")
    echo '{"baseRefName":%q}'
    ;;
  *)
    echo "fake gh: unhandled invocation: $*" >&2
    exit 1
    ;;
esac
`, responses.user, responses.repoURL, responses.repoSSHURL, responses.baseRefName)

	path := filepath.Join(binDir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)
}

type dispatchGitFixture struct {
	repoRoot  string
	host      string
	remoteURL string
	baseSHA   string
	prHeadSHA string
	staleSHA  string
}

const gitDaemonPort = "9418"

func newDispatchGitFixture(t *testing.T, prNumber int) dispatchGitFixture {
	t.Helper()

	baseDir := t.TempDir()
	bareDir := filepath.Join(baseDir, "acme", "widgets.git")
	if err := os.MkdirAll(filepath.Dir(bareDir), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, "", "init", "--bare", "-q", bareDir)
	runGit(t, bareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	startGitDaemon(t, baseDir)

	host := "127.0.0.1"
	remoteURL := fmt.Sprintf("git://127.0.0.1:%s/acme/widgets.git", gitDaemonPort)

	repoRoot := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q", "-b", "main")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	runGit(t, repoRoot, "remote", "add", "origin", remoteURL)

	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\n")
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-q", "-m", "initial commit")
	runGit(t, repoRoot, "push", "-q", bareDir, "main:refs/heads/main")
	baseSHA := runGitOutput(t, repoRoot, "rev-parse", "HEAD")

	runGit(t, repoRoot, "checkout", "-q", "-b", "pr-branch")
	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\nchanged\n")
	runGit(t, repoRoot, "commit", "-q", "-am", "pr change")
	prHeadSHA := runGitOutput(t, repoRoot, "rev-parse", "HEAD")
	runGit(t, repoRoot, "push", "-q", bareDir, fmt.Sprintf("pr-branch:refs/pull/%d/head", prNumber))

	writeFile(t, filepath.Join(repoRoot, "README.md"), "hello\nchanged\nagain\n")
	runGit(t, repoRoot, "commit", "-q", "-am", "post-review push")
	staleSHA := runGitOutput(t, repoRoot, "rev-parse", "HEAD")
	runGit(t, repoRoot, "push", "-q", "-f", bareDir, fmt.Sprintf("pr-branch:refs/pull/%d/head", prNumber))

	runGit(t, repoRoot, "checkout", "-q", "main")

	return dispatchGitFixture{
		repoRoot:  repoRoot,
		host:      host,
		remoteURL: remoteURL,
		baseSHA:   baseSHA,
		prHeadSHA: prHeadSHA,
		staleSHA:  staleSHA,
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startGitDaemon(t *testing.T, baseDir string) {
	t.Helper()

	stderr := &syncBuffer{}
	cmd := exec.Command("git", "daemon",
		"--reuseaddr",
		"--export-all",
		"--base-path="+baseDir,
		"--port="+gitDaemonPort,
		"--listen=127.0.0.1",
	)
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start git daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", "127.0.0.1:"+gitDaemonPort)
		if err == nil {
			_ = conn.Close()
			return
		}
		if strings.Contains(stderr.String(), "Address already in use") {
			t.Skipf("port %s unavailable for git daemon: %s", gitDaemonPort, stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("git daemon did not become ready: %s", stderr.String())
	t.Fatal("git daemon did not become ready")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd = exec.Command("git", append([]string{"-C", dir}, args...)...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return trimNewline(string(out))
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dispatchPullRequestKey(host string, prNumber int) store.PullRequestKeyV1 {
	return store.PullRequestKeyV1{Host: host, Owner: "acme", Repository: "widgets", Number: prNumber}
}

func dispatchSnapshot(key store.PullRequestKeyV1, head, base string, now time.Time) domain.PullRequestSnapshot {
	return domain.PullRequestSnapshot{
		PullRequest:  key.ToDomain(),
		URL:          fmt.Sprintf("https://%s/acme/widgets/pull/%d", key.Host, key.Number),
		Title:        "widgets PR",
		Author:       "someone-else",
		State:        domain.PullRequestStateOpen,
		HeadObjectID: head,
		BaseObjectID: base,
		UpdatedAt:    now.Add(-time.Hour),
		CapturedAt:   now,
	}
}

func fakeReviewRun(t *testing.T, target domain.ReviewTarget) *domain.ReviewRun {
	t.Helper()
	configuration, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         1,
		Concurrency:       1,
		Timeout:           time.Minute,
		ReviewerAgents:    []string{"codex"},
		SummarizerAgent:   "codex",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		FPThreshold:       75,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &domain.ReviewRun{
		ID:                       "run-1",
		Target:                   target,
		Trigger:                  domain.ReviewTriggerDesk,
		Engine:                   domain.ReviewEngine{Name: "acr", Version: "test"},
		Configuration:            configuration,
		ConfigurationFingerprint: configuration.Fingerprint(),
		ConfigurationSource:      domain.ConfigurationSourceIdentity{Kind: "defaults", Locator: "test"},
		StartedAt:                time.Now(),
		CompletedAt:              time.Now(),
		Status:                   domain.ReviewStatusCompleted,
		Conclusion:               domain.ReviewConclusionClean,
	}
}

func fakeDispatchService(run *domain.ReviewRun) dispatchServiceFactory {
	return func(ReviewOpts, *terminal.Logger) (semanticReviewService, error) {
		return fakeSemanticReviewService{run: run}, nil
	}
}

type fakeSemanticReviewService struct {
	run *domain.ReviewRun
}

func (s fakeSemanticReviewService) Run(ctx context.Context, request review.Request) (*domain.ReviewRun, error) {
	run := *s.run
	run.Target.RepositoryRoot = request.Target.RepositoryRoot
	run.Target.WorktreeRoot = request.Target.WorktreeRoot
	run.Target.PullRequest = request.Target.PullRequest
	run.Target.Revision.RequestedBaseRef = request.Target.Revision.RequestedBaseRef
	run.Target.Revision.ResolvedBaseRef = request.Target.Revision.ResolvedBaseRef
	return &run, nil
}

func TestRunDeskDispatch_PersistsRunAndReportsDecisionReady(t *testing.T) {
	fixture := newDispatchGitFixture(t, 7)

	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)
	writeWorkspaceConfig(t, configDir, "me", []string{fixture.repoRoot})
	withFakeGH(t, fakeGHResponses{
		user:        "me",
		repoURL:     fixture.remoteURL,
		repoSSHURL:  fixture.remoteURL,
		baseRefName: "main",
	})

	key := dispatchPullRequestKey(fixture.host, 7)
	now := time.Now()
	discovery := &dispatchFixtureDiscovery{
		searchResult: []domain.PullRequestKey{key.ToDomain()},
		enrichResponses: []domain.PullRequestSnapshot{
			dispatchSnapshot(key, fixture.prHeadSHA, fixture.baseSHA, now),
			dispatchSnapshot(key, fixture.prHeadSHA, fixture.baseSHA, now.Add(time.Minute)),
		},
	}

	target := domain.ReviewTarget{
		RepositoryRoot: fixture.repoRoot,
		Revision: domain.RevisionEvidence{
			HeadObjectID: fixture.prHeadSHA,
			BaseObjectID: fixture.baseSHA,
		},
	}

	err := runDeskDispatch(context.Background(), key, discovery, fakeDispatchService(fakeReviewRun(t, target)))
	if err != nil {
		t.Fatalf("runDeskDispatch failed: %v", err)
	}

	runs, corrupt, err := store.NewFilesystemRunStore(dataDir).ListRuns(key)
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("unexpected corrupt runs: %v", corrupt)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one persisted run, got %d", len(runs))
	}
	if runs[0].Target.Revision.HeadObjectID != fixture.prHeadSHA {
		t.Fatalf("persisted run head = %q, want %q", runs[0].Target.Revision.HeadObjectID, fixture.prHeadSHA)
	}

	cfg, err := workspace.Load(configDir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := desk.LoadStored(dataDir, *cfg, time.Now())
	if err != nil {
		t.Fatalf("LoadStored failed: %v", err)
	}
	item, ok := findDeskItem(inbox, key)
	if !ok {
		t.Fatal("expected the dispatched PR to still be tracked")
	}
	if item.DeskState != domain.DeskStateDecisionReady {
		t.Fatalf("desk state = %q, reason = %q, want decision_ready", item.DeskState, item.Reason)
	}
}

func TestRunDeskDispatch_MarksResultStaleWhenHeadMovesAfterReview(t *testing.T) {
	fixture := newDispatchGitFixture(t, 9)

	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)
	writeWorkspaceConfig(t, configDir, "me", []string{fixture.repoRoot})
	withFakeGH(t, fakeGHResponses{
		user:        "me",
		repoURL:     fixture.remoteURL,
		repoSSHURL:  fixture.remoteURL,
		baseRefName: "main",
	})

	key := dispatchPullRequestKey(fixture.host, 9)
	now := time.Now()
	discovery := &dispatchFixtureDiscovery{
		searchResult: []domain.PullRequestKey{key.ToDomain()},
		enrichResponses: []domain.PullRequestSnapshot{
			dispatchSnapshot(key, fixture.prHeadSHA, fixture.baseSHA, now),
			dispatchSnapshot(key, fixture.staleSHA, fixture.baseSHA, now.Add(time.Minute)),
		},
	}

	target := domain.ReviewTarget{
		RepositoryRoot: fixture.repoRoot,
		Revision: domain.RevisionEvidence{
			HeadObjectID: fixture.prHeadSHA,
			BaseObjectID: fixture.baseSHA,
		},
	}

	err := runDeskDispatch(context.Background(), key, discovery, fakeDispatchService(fakeReviewRun(t, target)))
	if err != nil {
		t.Fatalf("runDeskDispatch failed: %v", err)
	}

	cfg, err := workspace.Load(configDir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := desk.LoadStored(dataDir, *cfg, time.Now())
	if err != nil {
		t.Fatalf("LoadStored failed: %v", err)
	}
	item, ok := findDeskItem(inbox, key)
	if !ok {
		t.Fatal("expected the dispatched PR to still be tracked")
	}
	if item.DeskState == domain.DeskStateDecisionReady || item.DeskState == domain.DeskStateFindingsReady {
		t.Fatalf("desk state = %q, want the result to read as stale/needing re-review since the head moved after the review completed", item.DeskState)
	}
}
