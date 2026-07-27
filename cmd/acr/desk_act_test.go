package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func fakeFindingsReviewRun(t *testing.T, target domain.ReviewTarget) *domain.ReviewRun {
	t.Helper()
	run := fakeReviewRun(t, target)
	run.Conclusion = domain.ReviewConclusionFindings
	run.Findings = []domain.ReviewFinding{
		{
			ID:   "finding-1",
			Kind: domain.ReviewFindingActionable,
			Group: domain.FindingGroup{
				Title:         "Example finding",
				Summary:       "Something worth fixing.",
				Messages:      []string{"Something worth fixing."},
				ReviewerCount: 1,
				Sources:       []int{0},
			},
		},
	}
	run.AggregatedFindings = []domain.AggregatedFinding{
		{Text: "Something worth fixing.", Reviewers: []int{0}},
	}
	run.Stats = domain.ReviewStats{TotalReviewers: 1, SuccessfulReviewers: 1}
	return run
}

func persistRun(t *testing.T, dataDir string, run *domain.ReviewRun) {
	t.Helper()
	schema, err := store.ToReviewRunSchema(*run, store.RenderedOutcomeV1{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewFilesystemRunStore(dataDir).SaveRun(schema); err != nil {
		t.Fatal(err)
	}
}

type deskActTestEnv struct {
	fixture       dispatchGitFixture
	dataDir       string
	configDir     string
	key           store.PullRequestKeyV1
	discovery     *dispatchFixtureDiscovery
	postReviewLog string
}

func setupDeskActTest(t *testing.T, prNumber int, run *domain.ReviewRun, author string, ownPRPolicy string, postingEnabled bool) deskActTestEnv {
	t.Helper()

	fixture := newDispatchGitFixture(t, prNumber)

	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)
	writeWorkspaceConfigFull(t, configDir, "me", []string{fixture.repoRoot}, postingEnabled, ownPRPolicy)

	key := dispatchPullRequestKey(fixture.host, prNumber)
	domainKey := key.ToDomain()
	run.Target = domain.ReviewTarget{
		RepositoryRoot: fixture.repoRoot,
		WorktreeRoot:   fixture.repoRoot,
		Revision: domain.RevisionEvidence{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "main",
			HeadObjectID:     fixture.prHeadSHA,
			BaseObjectID:     fixture.baseSHA,
		},
		PullRequest: &domainKey,
	}
	persistRun(t, dataDir, run)

	now := time.Now()
	snapshot := dispatchSnapshotWithAuthor(key, author, fixture.prHeadSHA, fixture.baseSHA, now)
	discovery := &dispatchFixtureDiscovery{
		searchResult: []domain.PullRequestKey{key.ToDomain()},
		enrichResponses: []domain.PullRequestSnapshot{
			snapshot,
			dispatchSnapshotWithAuthor(key, author, fixture.prHeadSHA, fixture.baseSHA, now.Add(time.Minute)),
		},
	}

	postReviewLog := filepath.Join(t.TempDir(), "pr-review.log")

	return deskActTestEnv{
		fixture:       fixture,
		dataDir:       dataDir,
		configDir:     configDir,
		key:           key,
		discovery:     discovery,
		postReviewLog: postReviewLog,
	}
}

func readPostReviewLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

func loadEvents(t *testing.T, dataDir string, key store.PullRequestKeyV1) []store.ReviewEventV1 {
	t.Helper()
	events, corrupt, err := store.NewFilesystemEventStore(dataDir).ListEvents(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("unexpected corrupt events: %v", corrupt)
	}
	return events
}

func TestRunDeskAct_PostsApprovalForCleanReview(t *testing.T) {
	env := setupDeskActTest(t, 21, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--approve") {
		t.Fatalf("expected an --approve invocation, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeActionApprovalPosted {
		t.Fatalf("expected exactly one action_approval_posted event, got %+v", events)
	}

	cfg, err := workspace.Load(env.configDir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := desk.LoadStored(env.dataDir, *cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item, ok := findDeskItem(inbox, env.key)
	if !ok || item.DeskState != domain.DeskStateResolved {
		t.Fatalf("expected the item to read as resolved after approval, got %+v (found=%v)", item, ok)
	}
}

func TestRunDeskAct_DoesNotRetryAfterAmbiguousSubmissionFailure(t *testing.T) {
	env := setupDeskActTest(t, 21, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:              "me",
		repoURL:           env.fixture.remoteURL,
		repoSSHURL:        env.fixture.remoteURL,
		baseRefName:       "main",
		watchHeadSHA:      env.fixture.prHeadSHA,
		watchBaseSHA:      env.fixture.baseSHA,
		prAuthor:          "someone-else",
		ciBucket:          "pass",
		postReviewLog:     env.postReviewLog,
		postReviewFailFor: 1,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected runDeskAct to fail when the first gh pr review invocation fails")
	}

	attempts := readPostReviewLog(t, env.postReviewLog+".count")
	if strings.TrimSpace(attempts) != "1" {
		t.Fatalf("desk act must not retry a non-idempotent submission after an ambiguous failure; expected exactly 1 gh invocation, got count file %q", attempts)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if posted != "" {
		t.Fatalf("no review should have been recorded as posted, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no action event to be recorded for a failed post, got %+v", events)
	}
}

func TestRunDeskAct_PostsRequestChangesForFindingsReview(t *testing.T) {
	env := setupDeskActTest(t, 22, fakeFindingsReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActRequestChanges, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--request-changes") {
		t.Fatalf("expected a --request-changes invocation, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeActionRequestChangesPosted {
		t.Fatalf("expected exactly one action_request_changes_posted event, got %+v", events)
	}
}

func TestRunDeskAct_RequestChangesSurvivesFailedLiveAuthorLookup(t *testing.T) {
	env := setupDeskActTest(t, 221, fakeFindingsReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActRequestChanges, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--request-changes") {
		t.Fatalf("expected the confirmed --request-changes action to survive a failed live PR-author lookup, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeActionRequestChangesPosted {
		t.Fatalf("expected exactly one action_request_changes_posted event, got %+v", events)
	}
}

func TestRunDeskAct_PostsCommentForFindingsReview(t *testing.T) {
	env := setupDeskActTest(t, 23, fakeFindingsReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActComment, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--comment") {
		t.Fatalf("expected a --comment invocation, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeActionCommentPosted {
		t.Fatalf("expected exactly one action_comment_posted event, got %+v", events)
	}
}

func TestRunDeskAct_RejectsWhenPostingDisabled(t *testing.T) {
	env := setupDeskActTest(t, 24, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", false)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when posting is disabled")
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation, got %q", posted)
	}
	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no events to be recorded, got %+v", events)
	}
}

func TestRunDeskAct_FailsClosedOnIdentityMismatch(t *testing.T) {
	env := setupDeskActTest(t, 25, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "someone-entirely-different",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error on identity mismatch")
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation, got %q", posted)
	}
}

func TestRunDeskAct_SelfAuthoredPRForcesComment(t *testing.T) {
	env := setupDeskActTest(t, 26, fakeReviewRun(t, domain.ReviewTarget{}), "me", "comment-only", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "me",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--comment") || strings.Contains(posted, "--approve") {
		t.Fatalf("expected a self-authored PR to be downgraded to --comment, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeActionCommentPosted {
		t.Fatalf("expected exactly one action_comment_posted event, got %+v", events)
	}
}

func TestRunDeskAct_SelfReviewClassificationUsesDeskStateNotLiveLookup(t *testing.T) {
	env := setupDeskActTest(t, 261, fakeReviewRun(t, domain.ReviewTarget{}), "me", "comment-only", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-entirely-different",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--comment") || strings.Contains(posted, "--approve") {
		t.Fatalf("expected the self-authored PR to be downgraded to --comment based on desk classification even though a live PR-author lookup would disagree, got %q", posted)
	}
}

func TestRunDeskAct_StaleHeadPreventsPostingAndRecordsAuditEvent(t *testing.T) {
	env := setupDeskActTest(t, 27, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.staleSHA, env.fixture.baseSHA, now.Add(time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.staleSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the head moved since the review")
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation for a stale head, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeReviewStale {
		t.Fatalf("expected exactly one review_stale audit event, got %+v", events)
	}
	if events[0].PriorHeadObjectID != env.fixture.prHeadSHA {
		t.Fatalf("expected prior_head_object_id = %q, got %q", env.fixture.prHeadSHA, events[0].PriorHeadObjectID)
	}
}

func TestRunDeskAct_UnverifiableRevisionDoesNotRecordFalseStaleEvent(t *testing.T) {
	env := setupDeskActTest(t, 271, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now.Add(time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchFail:     true,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the revision could not be verified before posting")
	}
	if !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("expected the error to explain the revision was actually unchanged, got %q", err.Error())
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation when the revision could not be verified, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no false review_stale audit event when the revision was actually unchanged, got %+v", events)
	}
}

func TestRunDeskAct_EmptyLiveBaseIsUnverifiableNotStale(t *testing.T) {
	env := setupDeskActTest(t, 275, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, "", now.Add(time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the live base object id could not be verified")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Fatalf("expected an unverifiable-revision error, not a false moved/stale report, got %q", err.Error())
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation when the base could not be verified, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no false review_stale audit event when the base could not be verified (only unverifiable), got %+v", events)
	}
}

func TestRunDeskAct_RecordStaleResultTreatsEmptyBaseAsUnverifiableNotMoved(t *testing.T) {
	env := setupDeskActTest(t, 276, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, "", now.Add(time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.staleSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the stale re-check's live base object id could not be verified")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Fatalf("expected an unverifiable-revision error, not a false moved/stale report, got %q", err.Error())
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no false review_stale audit event when the re-check base could not be verified, got %+v", events)
	}
}

func TestRunDeskAct_RevisionMovedDuringPreSubmitCheckRoutesToStaleOutcome(t *testing.T) {
	env := setupDeskActTest(t, 272, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now.Add(time.Minute)),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.staleSHA, env.fixture.baseSHA, now.Add(2*time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the revision moved during the pre-submit eligibility recheck")
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation when the pre-submit recheck finds a moved revision, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeReviewStale {
		t.Fatalf("expected exactly one review_stale audit event routed through the stale outcome, got %+v", events)
	}
}

func TestRunDeskAct_DeclinedConfirmationDoesNotPost(t *testing.T) {
	env := setupDeskActTest(t, 28, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, false, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the action is not confirmed")
	}

	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation without confirmation, got %q", posted)
	}
	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no events to be recorded, got %+v", events)
	}
}

func TestRunDeskAct_CIDowngradedApprovalStaysActionable(t *testing.T) {
	env := setupDeskActTest(t, 29, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "fail",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err != nil {
		t.Fatalf("runDeskAct failed: %v", err)
	}

	posted := readPostReviewLog(t, env.postReviewLog)
	if !strings.Contains(posted, "--comment") {
		t.Fatalf("expected a CI-downgraded approval to post --comment, got %q", posted)
	}

	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 0 {
		t.Fatalf("expected no action-posted event for a CI-downgraded approval (item must stay actionable), got %+v", events)
	}

	cfg, err := workspace.Load(env.configDir)
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := desk.LoadStored(env.dataDir, *cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item, ok := findDeskItem(inbox, env.key)
	if !ok || item.DeskState != domain.DeskStateDecisionReady {
		t.Fatalf("expected the item to remain decision_ready after a CI-downgraded approval, got %+v (found=%v)", item, ok)
	}
}

func TestRunDeskAct_RejectsNoChangesRun(t *testing.T) {
	run := fakeReviewRun(t, domain.ReviewTarget{})
	run.Conclusion = domain.ReviewConclusionNoChanges
	env := setupDeskActTest(t, 30, run, "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the stored run found no changes")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation for a no-changes run, got %q", posted)
	}
}

func TestRunDeskAct_BaseMismatchPreventsPosting(t *testing.T) {
	env := setupDeskActTest(t, 31, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	now := time.Now()
	env.discovery.enrichResponses = []domain.PullRequestSnapshot{
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, env.fixture.baseSHA, now),
		dispatchSnapshotWithAuthor(env.key, "someone-else", env.fixture.prHeadSHA, "a-different-base-sha", now.Add(time.Minute)),
	}
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the base moved since the review even though the head did not")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation for a base mismatch, got %q", posted)
	}
	events := loadEvents(t, env.dataDir, env.key)
	if len(events) != 1 || events[0].Type != store.EventTypeReviewStale {
		t.Fatalf("expected exactly one review_stale audit event for the base mismatch, got %+v", events)
	}
	if events[0].BaseObjectID != "a-different-base-sha" {
		t.Fatalf("expected the stale event to record the live base that actually moved, got base=%q", events[0].BaseObjectID)
	}
}

func TestRunDeskAct_RejectsWhenRunHistoryHasCorruptRecords(t *testing.T) {
	env := setupDeskActTest(t, 32, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	runsDir := filepath.Join(env.dataDir, "prs", env.key.Host, env.key.Owner, env.key.Repository, "32", "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(runsDir, "20260101T000000.000000000Z-corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when stored run history has an unreadable record")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation with corrupt run history present, got %q", posted)
	}
}

func TestRunDeskAct_RejectsWhenEventHistoryHasCorruptRecords(t *testing.T) {
	env := setupDeskActTest(t, 36, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		prAuthor:      "someone-else",
		postReviewLog: env.postReviewLog,
	})

	eventsDir := filepath.Join(env.dataDir, "prs", env.key.Host, env.key.Owner, env.key.Repository, "36", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(eventsDir, "20260101T000000.000000000Z-corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when stored event history has an unreadable record")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation with corrupt event history present, got %q", posted)
	}
}

func TestRunDeskAct_FinalCheckCatchesBaseMoveMissedByPreflight(t *testing.T) {
	oldDelay := submissionRetryDelay
	submissionRetryDelay = 0
	defer func() { submissionRetryDelay = oldDelay }()

	env := setupDeskActTest(t, 33, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  "a-base-that-moved-after-the-preflight-check",
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the final live check finds a base mismatch the preflight discovery check missed")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation, got %q", posted)
	}
}

func TestRunDeskAct_FinalCheckBlocksPostingWhenPRClosedDuringSubmissionWindow(t *testing.T) {
	oldDelay := submissionRetryDelay
	submissionRetryDelay = 0
	defer func() { submissionRetryDelay = oldDelay }()

	env := setupDeskActTest(t, 371, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  env.fixture.prHeadSHA,
		watchBaseSHA:  env.fixture.baseSHA,
		watchState:    "CLOSED",
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the pull request closed before the final submission check")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation when the pull request closed during the submission window, got %q", posted)
	}
}

func TestRunDeskAct_FinalCheckFailsClosedWhenLiveLookupIsUnavailable(t *testing.T) {
	oldDelay := submissionRetryDelay
	submissionRetryDelay = 0
	defer func() { submissionRetryDelay = oldDelay }()

	env := setupDeskActTest(t, 34, fakeReviewRun(t, domain.ReviewTarget{}), "someone-else", "disabled", true)
	withFakeGH(t, fakeGHResponses{
		user:          "me",
		repoURL:       env.fixture.remoteURL,
		repoSSHURL:    env.fixture.remoteURL,
		baseRefName:   "main",
		watchHeadSHA:  "",
		watchBaseSHA:  "",
		prAuthor:      "someone-else",
		ciBucket:      "pass",
		postReviewLog: env.postReviewLog,
	})

	err := runDeskAct(context.Background(), env.key, deskActApprove, true, env.discovery)
	if err == nil {
		t.Fatal("expected an error when the final live revision lookup returns no data")
	}
	if posted := readPostReviewLog(t, env.postReviewLog); posted != "" {
		t.Fatalf("expected no GitHub mutation, got %q", posted)
	}
}
