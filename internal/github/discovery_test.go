package github

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestSearch_ReviewRequested_BuildsExpectedArgsAndParsesResults(t *testing.T) {
	response := `[
		{"number":42,"url":"https://github.com/richhaase/agentic-code-reviewer/pull/42","repository":{"nameWithOwner":"richhaase/agentic-code-reviewer"}},
		{"number":7,"url":"https://github.com/richhaase/other-repo/pull/7","repository":{"nameWithOwner":"richhaase/other-repo"}}
	]`
	scriptDir := setupMockGH(t, response)

	keys, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind:         SearchKindReviewRequested,
		Organization: "richhaase",
		Login:        "richhaase",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %+v", len(keys), keys)
	}
	want := domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 42}
	if keys[0] != want {
		t.Errorf("expected %+v, got %+v", want, keys[0])
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 {
		t.Fatalf("expected 1 gh invocation, got %v", args)
	}
	if got := args[0]; !strings.Contains(got, "search prs") || !strings.Contains(got, "--review-requested richhaase") || !strings.Contains(got, "--owner richhaase") {
		t.Errorf("unexpected args: %q", got)
	}
}

func TestSearch_Authored_UsesAuthorFlag(t *testing.T) {
	scriptDir := setupMockGH(t, `[]`)

	_, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind:         SearchKindAuthored,
		Organization: "richhaase",
		Login:        "richhaase",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "--author richhaase") {
		t.Errorf("expected --author flag, got %v", args)
	}
}

func TestSearch_TeamReviewRequested_QualifiesTeamWithOrganization(t *testing.T) {
	scriptDir := setupMockGH(t, `[]`)

	_, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind:         SearchKindTeamReviewRequested,
		Organization: "richhaase",
		Team:         "reviewers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "--review-requested richhaase/reviewers") {
		t.Errorf("expected qualified team review-requested flag, got %v", args)
	}
}

func TestSearch_RejectsInvalidQuery(t *testing.T) {
	setupMockGH(t, `[]`)

	if _, err := NewDiscovery().Search(context.Background(), SearchQuery{Kind: SearchKindReviewRequested}); err == nil {
		t.Fatal("expected validation error for missing login")
	}
}

func TestSearch_RejectsBareTeamSlugWithoutOrganization(t *testing.T) {
	setupMockGH(t, `[]`)

	_, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind: SearchKindTeamReviewRequested,
		Team: "reviewers",
	})
	if err == nil {
		t.Fatal("expected validation error for ambiguous bare team slug")
	}
}

func TestSearch_AcceptsFullyQualifiedTeamWithoutOrganization(t *testing.T) {
	scriptDir := setupMockGH(t, `[]`)

	_, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind: SearchKindTeamReviewRequested,
		Team: "richhaase/reviewers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "--review-requested richhaase/reviewers") {
		t.Errorf("expected qualified team review-requested flag, got %v", args)
	}
}

func TestSearch_RequestsMaxResultLimitToAvoidTruncation(t *testing.T) {
	scriptDir := setupMockGH(t, `[]`)

	_, err := NewDiscovery().Search(context.Background(), SearchQuery{
		Kind:         SearchKindReviewRequested,
		Organization: "richhaase",
		Login:        "richhaase",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "--limit 1000") {
		t.Errorf("expected an explicit high --limit to avoid the gh default of 30, got %v", args)
	}
}

func TestEnrich_BuildsExpectedArgsAndParsesFullResponse(t *testing.T) {
	response := `{
		"number": 202,
		"url": "https://github.com/richhaase/agentic-code-reviewer/pull/202",
		"title": "Implement GitHub candidate discovery and PR enrichment",
		"author": {"login": "richhaase"},
		"state": "OPEN",
		"isDraft": false,
		"headRefOid": "headsha123",
		"baseRefOid": "basesha456",
		"reviewDecision": "REVIEW_REQUIRED",
		"reviewRequests": [
			{"__typename":"User","login":"reviewer1"},
			{"__typename":"Team","slug":"reviewers","name":"Reviewers"}
		],
		"latestReviews": [
			{"author":{"login":"reviewer2"},"state":"COMMENTED","submittedAt":"2026-07-24T10:00:00Z"}
		],
		"statusCheckRollup": [
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS","name":"Test"}
		],
		"mergeStateStatus": "CLEAN",
		"updatedAt": "2026-07-24T09:00:00Z"
	}`
	scriptDir := setupMockGH(t, response)

	key := domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 202}
	snapshot, err := NewDiscovery().Enrich(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snapshot.HeadObjectID != "headsha123" || snapshot.BaseObjectID != "basesha456" {
		t.Errorf("unexpected object ids: %+v", snapshot)
	}
	if snapshot.State != domain.PullRequestStateOpen {
		t.Errorf("expected open state, got %q", snapshot.State)
	}
	if snapshot.CheckRollupState != "SUCCESS" {
		t.Errorf("expected SUCCESS rollup, got %q", snapshot.CheckRollupState)
	}
	if len(snapshot.ReviewRequests) != 2 {
		t.Fatalf("expected 2 review requests, got %+v", snapshot.ReviewRequests)
	}
	if snapshot.ReviewRequests[0].Kind != domain.ReviewRequestKindUser || snapshot.ReviewRequests[0].Login != "reviewer1" {
		t.Errorf("unexpected user review request: %+v", snapshot.ReviewRequests[0])
	}
	if snapshot.ReviewRequests[1].Kind != domain.ReviewRequestKindTeam || snapshot.ReviewRequests[1].Login != "richhaase/reviewers" {
		t.Errorf("unexpected team review request: %+v", snapshot.ReviewRequests[1])
	}
	if len(snapshot.LatestReviews) != 1 || snapshot.LatestReviews[0].Author != "reviewer2" {
		t.Errorf("unexpected latest reviews: %+v", snapshot.LatestReviews)
	}
	if snapshot.CapturedAt.IsZero() {
		t.Error("expected captured_at to be set")
	}
	if err := snapshot.Validate(); err != nil {
		t.Errorf("expected valid snapshot, got %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "pr view 202") || !strings.Contains(args[0], "-R github.com/richhaase/agentic-code-reviewer") {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestEnrich_IncludesHostInRepoSelectorForEnterpriseHost(t *testing.T) {
	scriptDir := setupMockGH(t, `{
		"number": 20, "url": "https://ghe.example.com/o/r/pull/20", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [], "statusCheckRollup": [],
		"mergeStateStatus": "CLEAN", "updatedAt": "2026-07-24T09:00:00Z"
	}`)

	_, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "ghe.example.com", Owner: "o", Repository: "r", Number: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || !strings.Contains(args[0], "-R ghe.example.com/o/r") {
		t.Errorf("expected host-qualified repo selector, got %v", args)
	}
}

func TestEnrich_TeamReviewRequestQualifiedWithRepositoryOwner(t *testing.T) {
	response := `{
		"number": 21, "url": "https://github.com/richhaase/agentic-code-reviewer/pull/21", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [{"__typename":"Team","slug":"reviewers","name":"Reviewers"}],
		"latestReviews": [], "statusCheckRollup": [],
		"mergeStateStatus": "CLEAN", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	setupMockGH(t, response)

	key := domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 21}
	snapshot, err := NewDiscovery().Enrich(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshot.ReviewRequests) != 1 || snapshot.ReviewRequests[0].Login != "richhaase/reviewers" {
		t.Fatalf("expected org-qualified team login, got %+v", snapshot.ReviewRequests)
	}
}

func TestEnrich_StaleChecksReportFailure(t *testing.T) {
	response := `{
		"number": 22, "url": "https://github.com/o/r/pull/22", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [],
		"statusCheckRollup": [
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"STALE"}
		],
		"mergeStateStatus": "UNKNOWN", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	setupMockGH(t, response)

	snapshot, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 22})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.CheckRollupState != "FAILURE" {
		t.Errorf("expected STALE check conclusion to report FAILURE, got %q", snapshot.CheckRollupState)
	}
}

func TestEnrich_DraftPullRequest(t *testing.T) {
	response := `{
		"number": 5, "url": "https://github.com/o/r/pull/5", "title": "wip",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": true,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [], "statusCheckRollup": [],
		"mergeStateStatus": "DRAFT", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	setupMockGH(t, response)

	snapshot, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !snapshot.Draft {
		t.Error("expected draft to be true")
	}
}

func TestEnrich_ClosedAndMergedPullRequests(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want domain.PullRequestState
	}{
		{"CLOSED", domain.PullRequestStateClosed},
		{"MERGED", domain.PullRequestStateMerged},
	} {
		response := `{
			"number": 6, "url": "https://github.com/o/r/pull/6", "title": "done",
			"author": {"login": "a"}, "state": "` + tc.raw + `", "isDraft": false,
			"headRefOid": "h", "baseRefOid": "b",
			"reviewRequests": [], "latestReviews": [], "statusCheckRollup": [],
			"mergeStateStatus": "UNKNOWN", "updatedAt": "2026-07-24T09:00:00Z"
		}`
		setupMockGH(t, response)

		snapshot, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 6})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if snapshot.State != tc.want {
			t.Errorf("state %q: expected %q, got %q", tc.raw, tc.want, snapshot.State)
		}
	}
}

func TestEnrich_FailingChecksReportFailure(t *testing.T) {
	response := `{
		"number": 8, "url": "https://github.com/o/r/pull/8", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [],
		"statusCheckRollup": [
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}
		],
		"mergeStateStatus": "DIRTY", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	setupMockGH(t, response)

	snapshot, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 8})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.CheckRollupState != "FAILURE" {
		t.Errorf("expected FAILURE, got %q", snapshot.CheckRollupState)
	}
}

func TestEnrich_PendingChecksReportPending(t *testing.T) {
	response := `{
		"number": 9, "url": "https://github.com/o/r/pull/9", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "h", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [],
		"statusCheckRollup": [
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}
		],
		"mergeStateStatus": "UNKNOWN", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	setupMockGH(t, response)

	snapshot, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 9})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.CheckRollupState != "PENDING" {
		t.Errorf("expected PENDING, got %q", snapshot.CheckRollupState)
	}
}

func TestEnrich_HeadChangeReflectedInSnapshot(t *testing.T) {
	first := `{
		"number": 10, "url": "https://github.com/o/r/pull/10", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "sha-one", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [], "statusCheckRollup": [],
		"mergeStateStatus": "CLEAN", "updatedAt": "2026-07-24T09:00:00Z"
	}`
	scriptDir := setupMockGH(t, first)
	key := domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 10}

	snapshot, err := NewDiscovery().Enrich(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.HeadObjectID != "sha-one" {
		t.Fatalf("expected sha-one, got %q", snapshot.HeadObjectID)
	}

	second := `{
		"number": 10, "url": "https://github.com/o/r/pull/10", "title": "t",
		"author": {"login": "a"}, "state": "OPEN", "isDraft": false,
		"headRefOid": "sha-two", "baseRefOid": "b",
		"reviewRequests": [], "latestReviews": [], "statusCheckRollup": [],
		"mergeStateStatus": "CLEAN", "updatedAt": "2026-07-24T09:05:00Z"
	}`
	if err := writeMockGHResponse(scriptDir, second); err != nil {
		t.Fatal(err)
	}

	snapshot, err = NewDiscovery().Enrich(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.HeadObjectID != "sha-two" {
		t.Fatalf("expected sha-two after head change, got %q", snapshot.HeadObjectID)
	}
}

func TestEnrich_TransientGitHubFailure(t *testing.T) {
	setupMockGHFailure(t, `HTTP 503: Service Unavailable (https://api.github.com/graphql)`)

	_, err := NewDiscovery().Enrich(context.Background(), domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 11})
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("expected ErrTransient, got %v", err)
	}
}

func TestObserveSnapshot_RetainsPreviousSnapshotAsStaleOnTransientFailure(t *testing.T) {
	key := domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 12}
	previous := domain.PullRequestSnapshot{
		PullRequest:  key,
		URL:          "https://github.com/o/r/pull/12",
		State:        domain.PullRequestStateOpen,
		HeadObjectID: "sha",
		BaseObjectID: "base",
		CapturedAt:   time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC),
	}

	discovery := stubDiscovery{enrichErr: transientErr("rate limit")}
	got, err := ObserveSnapshot(context.Background(), discovery, key, &previous)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Stale {
		t.Error("expected retained snapshot to be marked stale")
	}
	if got.HeadObjectID != "sha" {
		t.Errorf("expected retained snapshot data, got %+v", got)
	}
}

func TestObserveSnapshot_PropagatesTransientFailureWithNoPrevious(t *testing.T) {
	key := domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 13}
	discovery := stubDiscovery{enrichErr: transientErr("rate limit")}

	_, err := ObserveSnapshot(context.Background(), discovery, key, nil)
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("expected ErrTransient to propagate, got %v", err)
	}
}

func TestObserveSnapshot_NonTransientFailurePropagatesEvenWithPrevious(t *testing.T) {
	key := domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 14}
	previous := domain.PullRequestSnapshot{PullRequest: key, HeadObjectID: "sha", CapturedAt: time.Now()}
	discovery := stubDiscovery{enrichErr: errors.New("repository not found")}

	_, err := ObserveSnapshot(context.Background(), discovery, key, &previous)
	if err == nil || errors.Is(err, ErrTransient) {
		t.Fatalf("expected non-transient error to propagate as-is, got %v", err)
	}
}

func TestObserveSnapshot_ReturnsFreshSnapshotOnSuccess(t *testing.T) {
	key := domain.PullRequestKey{Host: "github.com", Owner: "o", Repository: "r", Number: 15}
	fresh := domain.PullRequestSnapshot{PullRequest: key, HeadObjectID: "fresh-sha", CapturedAt: time.Now()}
	discovery := stubDiscovery{enrichResult: fresh}

	got, err := ObserveSnapshot(context.Background(), discovery, key, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Stale {
		t.Error("expected fresh snapshot to not be marked stale")
	}
	if got.HeadObjectID != "fresh-sha" {
		t.Errorf("expected fresh snapshot data, got %+v", got)
	}
}

func TestReduceCheckRollup_EmptyIsNone(t *testing.T) {
	if got := reduceCheckRollup(nil); got != "NONE" {
		t.Errorf("expected NONE for empty checks, got %q", got)
	}
}

func TestReduceCheckRollup_StatusContextStates(t *testing.T) {
	tests := []struct {
		name  string
		check enrichCheck
		want  string
	}{
		{"status context failure", enrichCheck{Typename: "StatusContext", State: "FAILURE"}, "FAILURE"},
		{"status context error", enrichCheck{Typename: "StatusContext", State: "ERROR"}, "FAILURE"},
		{"status context pending", enrichCheck{Typename: "StatusContext", State: "PENDING"}, "PENDING"},
		{"status context expected", enrichCheck{Typename: "StatusContext", State: "EXPECTED"}, "PENDING"},
		{"status context success", enrichCheck{Typename: "StatusContext", State: "SUCCESS"}, "SUCCESS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reduceCheckRollup([]enrichCheck{tt.check}); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestReduceCheckRollup_ExpectedRequiredCheckDoesNotReportSuccess(t *testing.T) {
	checks := []enrichCheck{
		{Typename: "CheckRun", Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Typename: "StatusContext", State: "EXPECTED"},
	}
	if got := reduceCheckRollup(checks); got != "PENDING" {
		t.Errorf("expected an EXPECTED required status to keep the rollup PENDING, got %q", got)
	}
}

func TestClassifyDiscoveryError_TransientMarkers(t *testing.T) {
	markers := []string{
		"API rate limit exceeded",
		"context deadline exceeded (Client.Timeout exceeded while awaiting headers)",
		"connection reset by peer",
		"connection refused",
		"HTTP 502: Bad Gateway",
		"HTTP 503: Service Unavailable",
		"HTTP 504: Gateway Timeout",
		"could not resolve host: api.github.com",
	}
	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			exitErr := &exec.ExitError{Stderr: []byte(marker)}
			err := classifyDiscoveryError(exitErr)
			if !errors.Is(err, ErrTransient) {
				t.Errorf("expected ErrTransient for %q, got %v", marker, err)
			}
		})
	}
}

func TestClassifyDiscoveryError_NonTransientErrorsAreNotTransient(t *testing.T) {
	exitErr := &exec.ExitError{Stderr: []byte("repository not found")}
	err := classifyDiscoveryError(exitErr)
	if errors.Is(err, ErrTransient) {
		t.Errorf("expected non-transient error, got %v", err)
	}
}

type stubDiscovery struct {
	enrichResult domain.PullRequestSnapshot
	enrichErr    error
}

func (s stubDiscovery) Search(ctx context.Context, query SearchQuery) ([]domain.PullRequestKey, error) {
	return nil, nil
}

func (s stubDiscovery) Enrich(ctx context.Context, key domain.PullRequestKey) (domain.PullRequestSnapshot, error) {
	return s.enrichResult, s.enrichErr
}

func transientErr(stderr string) error {
	return classifyDiscoveryError(&exec.ExitError{Stderr: []byte(stderr)})
}
