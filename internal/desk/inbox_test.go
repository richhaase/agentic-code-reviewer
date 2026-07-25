package desk

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
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

type fixtureDiscovery struct {
	search      map[github.SearchKind][]domain.PullRequestKey
	searchErr   error
	enrich      map[domain.PullRequestKey]domain.PullRequestSnapshot
	err         map[domain.PullRequestKey]error
	calls       []github.SearchQuery
	enrichCalls []domain.PullRequestKey
}

func (f *fixtureDiscovery) Search(ctx context.Context, query github.SearchQuery) ([]domain.PullRequestKey, error) {
	f.calls = append(f.calls, query)
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.search[query.Kind], nil
}

func (f *fixtureDiscovery) Enrich(ctx context.Context, key domain.PullRequestKey) (domain.PullRequestSnapshot, error) {
	f.enrichCalls = append(f.enrichCalls, key)
	if err, ok := f.err[key]; ok {
		return domain.PullRequestSnapshot{}, err
	}
	if snapshot, ok := f.enrich[key]; ok {
		return snapshot, nil
	}
	return domain.PullRequestSnapshot{}, errors.New("no fixture for this key")
}

func baseWorkspaceConfig(t *testing.T, reviewableRoot string) workspace.Config {
	t.Helper()
	return workspace.Config{
		Identity: workspace.IdentityConfig{ExpectedUser: "me"},
		Scope: workspace.ScopeConfig{
			Organizations:   []string{"acme"},
			RepositoryRoots: []string{reviewableRoot},
		},
		Behavior: workspace.BehaviorConfig{
			SettleTime:  config.Duration(10 * time.Minute),
			OwnPRPolicy: workspace.OwnPRPolicyDisabled,
		},
	}
}

func fixtureSnapshot(key domain.PullRequestKey, author, head string, now time.Time) domain.PullRequestSnapshot {
	return domain.PullRequestSnapshot{
		PullRequest:  key,
		URL:          "https://github.com/acme/widgets/pull/" + string(rune('0'+key.Number)),
		Title:        "a pull request",
		Author:       author,
		State:        domain.PullRequestStateOpen,
		HeadObjectID: head,
		BaseObjectID: "base-sha",
		UpdatedAt:    now.Add(-time.Hour),
		CapturedAt:   now,
	}
}

func TestRefresh_FirstReviewNeedsReview(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 1}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	if inbox.Items[0].DeskState != domain.DeskStateNeedsReview {
		t.Errorf("expected needs_review, got %q", inbox.Items[0].DeskState)
	}

	stored, err := store.NewFilesystemSnapshotStore(dataDir).LoadSnapshot(store.ToPullRequestKeySchema(key))
	if err != nil {
		t.Fatalf("expected the fresh snapshot to be persisted: %v", err)
	}
	if stored.HeadObjectID != "head-1" {
		t.Errorf("unexpected persisted snapshot: %+v", stored)
	}
}

func TestRefresh_OwnPRDefaultsToWaitingOnOthers(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 2}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindAuthored: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "me", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	item := inbox.Items[0]
	if !item.OwnPR {
		t.Error("expected OwnPR to be true")
	}
	if item.DeskState != domain.DeskStateWaitingOnOthers {
		t.Errorf("expected waiting_on_others for an own PR with review disabled, got %q", item.DeskState)
	}
}

func TestRefresh_RepositoryUnavailableRemainsVisible(t *testing.T) {
	cfg := baseWorkspaceConfig(t, t.TempDir())
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 3}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	item := inbox.Items[0]
	if item.DeskState != domain.DeskStateRepositoryUnavailable {
		t.Errorf("expected repository_unavailable, got %q", item.DeskState)
	}
	if item.Title == "" || item.URL == "" {
		t.Errorf("expected an unavailable repository's PR to still show its title/url, got %+v", item)
	}
}

func TestRefresh_TransientFailureFallsBackToStaleStoredSnapshot(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 4}
	schemaKey := store.ToPullRequestKeySchema(key)
	previous := fixtureSnapshot(key, "someone-else", "head-1", now.Add(-time.Hour))
	if err := store.NewFilesystemSnapshotStore(dataDir).SaveSnapshot(store.ToPRSnapshotSchema(previous)); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		err: map[domain.PullRequestKey]error{
			key: transientTestError{},
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	item := inbox.Items[0]
	if !item.SnapshotStale {
		t.Error("expected the fallback snapshot to be marked stale")
	}
	if item.HeadObjectID != "head-1" {
		t.Errorf("expected the previous snapshot's data to be retained, got %+v", item)
	}

	reloaded, err := store.NewFilesystemSnapshotStore(dataDir).LoadSnapshot(schemaKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reloaded.CapturedAt.Equal(previous.CapturedAt) {
		t.Error("expected the stale fallback to not overwrite the stored snapshot")
	}
}

type transientTestError struct{}

func (transientTestError) Error() string { return "transient test failure" }
func (transientTestError) Unwrap() error { return github.ErrTransient }

func TestRefresh_ContinuingResponsibilitySurvivesDroppedSearchResult(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 5}
	previous := fixtureSnapshot(key, "someone-else", "head-1", now.Add(-time.Hour))
	if err := store.NewFilesystemSnapshotStore(dataDir).SaveSnapshot(store.ToPRSnapshotSchema(previous)); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-2", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected the previously tracked PR to still be refreshed even though search dropped it, got %+v", inbox.Items)
	}
	if inbox.Items[0].HeadObjectID != "head-2" {
		t.Errorf("expected a fresh enrichment, got %+v", inbox.Items[0])
	}
}

func TestRefresh_ReleasedPRIsExcluded(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 6}
	schemaKey := store.ToPullRequestKeySchema(key)
	if _, err := store.NewFilesystemEventStore(dataDir).AppendEvent(store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "event-1",
		PullRequest:   schemaKey,
		Type:          store.EventTypeUserReleased,
		OccurredAt:    now.Add(-time.Minute),
		Actor:         "me",
	}); err != nil {
		t.Fatalf("append release event: %v", err)
	}
	if err := store.NewFilesystemSnapshotStore(dataDir).SaveSnapshot(store.ToPRSnapshotSchema(fixtureSnapshot(key, "someone-else", "head-1", now.Add(-time.Hour)))); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 0 {
		t.Fatalf("expected a released PR to be excluded from the inbox, got %+v", inbox.Items)
	}
}

func TestLoadStored_RendersWithoutDiscoveryAndReportsAge(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	dataDir := t.TempDir()

	captured := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 7}
	if err := store.NewFilesystemSnapshotStore(dataDir).SaveSnapshot(store.ToPRSnapshotSchema(fixtureSnapshot(key, "someone-else", "head-1", captured))); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	now := captured.Add(37 * time.Minute)
	inbox, err := LoadStored(dataDir, cfg, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inbox.FromLiveLock {
		t.Error("expected FromLiveLock to be true for the read-only path")
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	if age := inbox.Items[0].SnapshotAge(now); age != 37*time.Minute {
		t.Errorf("expected a 37m snapshot age, got %v", age)
	}
}

func TestRefresh_FailedFindingsReadyAndResolvedStates(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		number     int
		status     domain.ReviewStatus
		conclusion domain.ReviewConclusion
		findings   []domain.ReviewFinding
		resolved   bool
		want       domain.DeskState
	}{
		{"failed", 10, domain.ReviewStatusFailed, domain.ReviewConclusionNone, nil, false, domain.DeskStateFailed},
		{"findings ready", 11, domain.ReviewStatusCompleted, domain.ReviewConclusionFindings, []domain.ReviewFinding{{ID: "f1", Kind: domain.ReviewFindingActionable}}, false, domain.DeskStateFindingsReady},
		{"resolved", 12, domain.ReviewStatusCompleted, domain.ReviewConclusionFindings, []domain.ReviewFinding{{ID: "f1", Kind: domain.ReviewFindingActionable}}, true, domain.DeskStateResolved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: tt.number}
			schemaKey := store.ToPullRequestKeySchema(key)
			pr := key

			reviewConfig, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
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
				t.Fatalf("NewReviewConfiguration: %v", err)
			}

			run := domain.ReviewRun{
				ID:      "run-1",
				Trigger: domain.ReviewTriggerDesk,
				Target: domain.ReviewTarget{
					RepositoryRoot: "/repo",
					WorktreeRoot:   "/repo",
					Revision: domain.RevisionEvidence{
						RequestedBaseRef: "main",
						ResolvedBaseRef:  "main",
						HeadObjectID:     "head-1",
						BaseObjectID:     "base-sha",
					},
					PullRequest: &pr,
				},
				Engine:                   domain.ReviewEngine{Name: "acr", Version: "test"},
				StartedAt:                now.Add(-2 * time.Hour),
				CompletedAt:              now.Add(-2 * time.Hour),
				Configuration:            reviewConfig,
				ConfigurationSource:      domain.ConfigurationSourceIdentity{Kind: "defaults"},
				ConfigurationFingerprint: reviewConfig.Fingerprint(),
				Status:                   tt.status,
				Conclusion:               tt.conclusion,
				Findings:                 tt.findings,
			}
			if tt.status == domain.ReviewStatusFailed {
				run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: "all reviewers failed"}
			}
			schema, err := store.ToReviewRunSchema(run, store.RenderedOutcomeV1{})
			if err != nil {
				t.Fatalf("ToReviewRunSchema: %v", err)
			}
			if _, err := store.NewFilesystemRunStore(dataDir).SaveRun(schema); err != nil {
				t.Fatalf("save run: %v", err)
			}

			if tt.resolved {
				if _, err := store.NewFilesystemEventStore(dataDir).AppendEvent(store.ReviewEventV1{
					SchemaVersion: store.CurrentSchemaVersion,
					ID:            "event-resolved",
					PullRequest:   schemaKey,
					Type:          store.EventTypeUserResolved,
					OccurredAt:    now.Add(-time.Minute),
					HeadObjectID:  "head-1",
					BaseObjectID:  "base-sha",
					Actor:         "me",
				}); err != nil {
					t.Fatalf("append resolved event: %v", err)
				}
			}

			discovery := &fixtureDiscovery{
				search: map[github.SearchKind][]domain.PullRequestKey{
					github.SearchKindReviewRequested: {key},
				},
				enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
					key: fixtureSnapshot(key, "someone-else", "head-1", now),
				},
			}

			inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(inbox.Items) != 1 {
				t.Fatalf("expected 1 item, got %+v", inbox.Items)
			}
			if inbox.Items[0].DeskState != tt.want {
				t.Errorf("expected %q, got %q", tt.want, inbox.Items[0].DeskState)
			}
		})
	}
}

func TestDiscoverCandidateKeys_QualifiesBareTeamWithEveryConfiguredOrganization(t *testing.T) {
	cfg := workspace.Config{
		Identity: workspace.IdentityConfig{ExpectedUser: "me"},
		Scope: workspace.ScopeConfig{
			Organizations: []string{"acme", "widgets-inc"},
			Teams:         []string{"reviewers", "other-org/qualified-team"},
		},
	}
	discovery := &fixtureDiscovery{search: map[github.SearchKind][]domain.PullRequestKey{}}

	discoverCandidateKeys(context.Background(), discovery, cfg)

	var teamQueries []github.SearchQuery
	for _, call := range discovery.calls {
		if call.Kind == github.SearchKindTeamReviewRequested {
			teamQueries = append(teamQueries, call)
		}
	}
	if len(teamQueries) != 3 {
		t.Fatalf("expected 3 team queries (bare team x 2 orgs + 1 pre-qualified), got %+v", teamQueries)
	}
	for _, q := range teamQueries {
		if err := q.Validate(); err != nil {
			t.Errorf("expected every issued team query to be valid, got %v for %+v", err, q)
		}
	}
}

func TestRefresh_TransientSearchFailureIsSurfacedAsWarning(t *testing.T) {
	cfg := baseWorkspaceConfig(t, t.TempDir())
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	discovery := &fixtureDiscovery{
		search:    map[github.SearchKind][]domain.PullRequestKey{},
		searchErr: transientTestError{},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Warnings) == 0 {
		t.Fatal("expected a transient search failure to be surfaced as a warning rather than silently producing an empty, seemingly-clean inbox")
	}
}

func TestRefresh_RepositoryIdentityMatchIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/Acme/Widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "Acme", Repository: "Widgets", Number: 20}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", inbox.Items)
	}
	if inbox.Items[0].DeskState == domain.DeskStateRepositoryUnavailable {
		t.Errorf("expected a differently-cased but locally available repository to not report repository_unavailable, got %+v", inbox.Items[0])
	}
}

func TestRefresh_ScopeExcludedRepositoryIsOmittedEntirely(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	cfg.Scope.Exclude = []string{"acme/widgets"}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 21}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 0 {
		t.Fatalf("expected a scope-excluded repository's PR to be omitted entirely rather than shown as repository_unavailable, got %+v", inbox.Items)
	}
}

func TestRefresh_ScopeExcludedPRSkippedBeforeEnrichment(t *testing.T) {
	cfg := baseWorkspaceConfig(t, t.TempDir())
	cfg.Scope.Exclude = []string{"acme/widgets"}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 22}
	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 0 {
		t.Fatalf("expected a scope-excluded PR with no local repo to be omitted, got %+v", inbox.Items)
	}
	if len(inbox.Warnings) != 0 {
		t.Errorf("expected no warnings for a scope-excluded PR, got %+v", inbox.Warnings)
	}
	if len(discovery.enrichCalls) != 0 {
		t.Errorf("expected a scope-excluded PR to be skipped before enrichment, got enrich calls %+v", discovery.enrichCalls)
	}
}

func TestRefresh_ReleasedPRSkippedBeforeEnrichment(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 23}
	schemaKey := store.ToPullRequestKeySchema(key)
	if _, err := store.NewFilesystemEventStore(dataDir).AppendEvent(store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "event-1",
		PullRequest:   schemaKey,
		Type:          store.EventTypeUserReleased,
		OccurredAt:    now.Add(-time.Minute),
		Actor:         "me",
	}); err != nil {
		t.Fatalf("append release event: %v", err)
	}

	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 0 {
		t.Fatalf("expected a released PR to be omitted, got %+v", inbox.Items)
	}
	if len(discovery.enrichCalls) != 0 {
		t.Errorf("expected a released PR to be skipped before enrichment, got enrich calls %+v", discovery.enrichCalls)
	}
}

func TestRefresh_SurfacesRepositoryRootScanWarnings(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	cfg := baseWorkspaceConfig(t, missingRoot)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	discovery := &fixtureDiscovery{search: map[github.SearchKind][]domain.PullRequestKey{}}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Warnings) == 0 {
		t.Fatal("expected a warning about the missing repository root")
	}
}

func TestLoadStored_RespectsScopeExclusion(t *testing.T) {
	cfg := baseWorkspaceConfig(t, t.TempDir())
	cfg.Scope.Exclude = []string{"acme/widgets"}
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 24}
	captured := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	if err := store.NewFilesystemSnapshotStore(dataDir).SaveSnapshot(store.ToPRSnapshotSchema(fixtureSnapshot(key, "someone-else", "head-1", captured))); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	inbox, err := LoadStored(dataDir, cfg, captured.Add(time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Items) != 0 {
		t.Fatalf("expected a scope-excluded stored PR to be omitted from the locked/read-only path, got %+v", inbox.Items)
	}
}

func TestRefresh_CorruptStoredHistorySurfacesWarning(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithRemote(t, filepath.Join(root, "widgets"), "https://github.com/acme/widgets.git")
	cfg := baseWorkspaceConfig(t, root)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	dataDir := t.TempDir()

	key := domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 25}
	runsPath := filepath.Join(dataDir, "prs", "github.com", "acme", "widgets", "25", "runs")
	if err := os.MkdirAll(runsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	corruptRun := filepath.Join(runsPath, "20260725T110000.000000000Z-run-bad.json")
	if err := os.WriteFile(corruptRun, []byte(`{"schema_version": 1, "id": "run-bad", "truncated`), 0o644); err != nil {
		t.Fatalf("seed corrupt run: %v", err)
	}

	discovery := &fixtureDiscovery{
		search: map[github.SearchKind][]domain.PullRequestKey{
			github.SearchKindReviewRequested: {key},
		},
		enrich: map[domain.PullRequestKey]domain.PullRequestSnapshot{
			key: fixtureSnapshot(key, "someone-else", "head-1", now),
		},
	}

	inbox, err := Refresh(context.Background(), cfg, dataDir, discovery, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inbox.Warnings) == 0 {
		t.Fatal("expected a warning about the corrupt stored run rather than silently classifying from incomplete history")
	}
}

func TestDiscoverCandidateKeys_BareTeamWithNoOrganizationsWarns(t *testing.T) {
	cfg := workspace.Config{
		Identity: workspace.IdentityConfig{ExpectedUser: "me"},
		Scope: workspace.ScopeConfig{
			Teams: []string{"reviewers"},
		},
	}
	discovery := &fixtureDiscovery{search: map[github.SearchKind][]domain.PullRequestKey{}}

	_, warnings := discoverCandidateKeys(context.Background(), discovery, cfg)

	if len(warnings) == 0 {
		t.Fatal("expected a warning that a bare team could not be searched without a configured organization")
	}
	for _, call := range discovery.calls {
		if call.Kind == github.SearchKindTeamReviewRequested {
			t.Errorf("expected no team search to be attempted for an unqualifiable bare team, got %+v", call)
		}
	}
}

func TestDiscoverCandidateKeys_UnscopedDirectUserSearchWhenNoOrganizationsConfigured(t *testing.T) {
	cfg := workspace.Config{
		Identity: workspace.IdentityConfig{ExpectedUser: "me"},
		Scope:    workspace.ScopeConfig{Include: []string{"acme/widgets"}},
	}
	discovery := &fixtureDiscovery{search: map[github.SearchKind][]domain.PullRequestKey{}}

	discoverCandidateKeys(context.Background(), discovery, cfg)

	var reviewRequested, authored bool
	for _, call := range discovery.calls {
		if call.Organization != "" {
			continue
		}
		switch call.Kind {
		case github.SearchKindReviewRequested:
			reviewRequested = true
		case github.SearchKindAuthored:
			authored = true
		}
	}
	if !reviewRequested || !authored {
		t.Fatalf("expected unscoped review-requested and authored searches when no organizations are configured, got calls %+v", discovery.calls)
	}
}

func TestAppendUniqueKey_DeduplicatesCaseInsensitively(t *testing.T) {
	keys := []domain.PullRequestKey{
		{Host: "github.com", Owner: "Acme", Repository: "Widgets", Number: 1},
	}
	keys = appendUniqueKey(keys, domain.PullRequestKey{Host: "GitHub.com", Owner: "acme", Repository: "widgets", Number: 1})

	if len(keys) != 1 {
		t.Fatalf("expected a case-variant of an existing key to be deduplicated, got %+v", keys)
	}
}

func TestAppendUniqueKey_DistinctRepositoriesAreNotMerged(t *testing.T) {
	keys := []domain.PullRequestKey{
		{Host: "github.com", Owner: "acme", Repository: "widgets", Number: 1},
	}
	keys = appendUniqueKey(keys, domain.PullRequestKey{Host: "github.com", Owner: "acme", Repository: "gadgets", Number: 1})

	if len(keys) != 2 {
		t.Fatalf("expected genuinely distinct repositories to both be kept, got %+v", keys)
	}
}
