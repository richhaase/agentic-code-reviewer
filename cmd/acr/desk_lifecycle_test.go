package main

import (
	"context"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func setupDeskLifecycleTest(t *testing.T, prNumber int) deskActTestEnv {
	t.Helper()

	fixture := newDispatchGitFixture(t, prNumber)

	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)
	writeWorkspaceConfigFull(t, configDir, "me", []string{fixture.repoRoot}, false, "disabled")

	key := dispatchPullRequestKey(fixture.host, prNumber)
	now := time.Now()
	discovery := &dispatchFixtureDiscovery{
		searchResult: []domain.PullRequestKey{key.ToDomain()},
		enrichResponses: []domain.PullRequestSnapshot{
			dispatchSnapshotWithAuthor(key, "someone-else", fixture.prHeadSHA, fixture.baseSHA, now),
		},
	}

	return deskActTestEnv{
		fixture:   fixture,
		dataDir:   dataDir,
		configDir: configDir,
		key:       key,
		discovery: discovery,
	}
}

func loadStoredItem(t *testing.T, env deskActTestEnv) (desk.Item, bool) {
	t.Helper()
	cfg, err := workspace.Load(env.configDir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := desk.LoadStored(env.dataDir, *cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return findDeskItem(inbox, env.key)
}

func TestRunDeskLifecycleAction_ResolveMarksItemResolved(t *testing.T) {
	env := setupDeskLifecycleTest(t, 31)
	withFakeGH(t, fakeGHResponses{user: "me"})

	if err := runDeskLifecycleAction(context.Background(), env.key, store.EventTypeUserResolved, env.discovery); err != nil {
		t.Fatalf("runDeskLifecycleAction failed: %v", err)
	}

	item, ok := loadStoredItem(t, env)
	if !ok || item.DeskState != domain.DeskStateResolved {
		t.Fatalf("expected the item to read as resolved, got %+v (found=%v)", item, ok)
	}
}

func TestRunDeskLifecycleAction_ReleaseUntracksItem(t *testing.T) {
	env := setupDeskLifecycleTest(t, 32)
	withFakeGH(t, fakeGHResponses{user: "me"})

	if err := runDeskLifecycleAction(context.Background(), env.key, store.EventTypeUserReleased, env.discovery); err != nil {
		t.Fatalf("runDeskLifecycleAction failed: %v", err)
	}

	if _, ok := loadStoredItem(t, env); ok {
		t.Fatal("expected a released PR to no longer be tracked")
	}
}

func TestRunDeskLifecycleAction_ResumeAfterReleaseRetracksItem(t *testing.T) {
	env := setupDeskLifecycleTest(t, 33)
	withFakeGH(t, fakeGHResponses{user: "me"})

	if err := runDeskLifecycleAction(context.Background(), env.key, store.EventTypeUserReleased, env.discovery); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if _, ok := loadStoredItem(t, env); ok {
		t.Fatal("expected the PR to be untracked after release")
	}

	if err := runDeskLifecycleAction(context.Background(), env.key, store.EventTypeUserResumed, env.discovery); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	item, ok := loadStoredItem(t, env)
	if !ok {
		t.Fatal("expected the PR to be tracked again after resume")
	}
	if item.DeskState == domain.DeskStateResolved {
		t.Fatalf("did not expect a resumed PR to read as resolved, got %+v", item)
	}
}

func TestRunDeskLifecycleAction_SnoozeSuppressesRereviewInFavorOfStale(t *testing.T) {
	env := setupDeskLifecycleTest(t, 34)
	withFakeGH(t, fakeGHResponses{user: "me"})

	run := fakeReviewRun(t, domain.ReviewTarget{})
	domainKey := env.key.ToDomain()
	run.Target = domain.ReviewTarget{
		RepositoryRoot: env.fixture.repoRoot,
		WorktreeRoot:   env.fixture.repoRoot,
		Revision: domain.RevisionEvidence{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "main",
			HeadObjectID:     env.fixture.baseSHA,
			BaseObjectID:     env.fixture.baseSHA,
		},
		PullRequest: &domainKey,
	}
	persistRun(t, env.dataDir, run)

	if err := runDeskLifecycleAction(context.Background(), env.key, store.EventTypeUserSnoozed, env.discovery); err != nil {
		t.Fatalf("runDeskLifecycleAction failed: %v", err)
	}

	item, ok := loadStoredItem(t, env)
	if !ok {
		t.Fatal("expected the PR to still be tracked after snoozing")
	}
	if item.DeskState != domain.DeskStateStale {
		t.Fatalf("expected a snoozed PR with a new head to read as stale (not needs_rereview), got %+v", item)
	}
}
