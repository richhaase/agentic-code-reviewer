package github

import (
	"context"
	"strings"
	"testing"
)

func TestParseDiscussionIncludesTextAndDetectsBodyEdits(t *testing.T) {
	first, err := parseDiscussion([]byte(`{"id":7,"body":"first","user":{"login":"octocat"},"updated_at":"2026-01-01T00:00:00Z"}`), "issue_comment")
	if err != nil {
		t.Fatalf("parseDiscussion() error = %v", err)
	}
	edited, err := parseDiscussion([]byte(`{"id":7,"body":"second","user":{"login":"octocat"},"updated_at":"2026-01-01T00:00:00Z"}`), "issue_comment")
	if err != nil {
		t.Fatalf("parseDiscussion() error = %v", err)
	}
	if len(first) != 1 || first[0].Author != "octocat" || first[0].Body != "first" {
		t.Fatalf("discussion = %#v", first)
	}
	if first[0].Revision == edited[0].Revision {
		t.Fatal("body edit must change the discussion revision even when timestamps match")
	}
}

func TestParseDiscussionPreservesInlineLocation(t *testing.T) {
	items, err := parseDiscussion([]byte(`{"id":7,"body":"this can panic","user":{"login":"octocat"},"path":"internal/watch/watch.go","line":42,"diff_hunk":"@@ -40,3 +40,3 @@"}`), "review_comment")
	if err != nil {
		t.Fatalf("parseDiscussion() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("discussion = %#v", items)
	}
	if items[0].Path != "internal/watch/watch.go" || items[0].Line != 42 || items[0].DiffHunk != "@@ -40,3 +40,3 @@" {
		t.Fatalf("discussion = %#v", items[0])
	}
}

func TestParseDiscussionSkipsEmptyText(t *testing.T) {
	items, err := parseDiscussion([]byte(`{"id":7,"body":"  ","user":{"login":"octocat"}}`), "review")
	if err != nil {
		t.Fatalf("parseDiscussion() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("discussion = %#v", items)
	}
}

func TestParseDiscussionSkipsPendingReviewDrafts(t *testing.T) {
	items, err := parseDiscussion([]byte(`{"id":7,"body":"draft","state":"PENDING","user":{"login":"octocat"}}`), "review")
	if err != nil {
		t.Fatalf("parseDiscussion() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("discussion = %#v", items)
	}
}

func TestGetPRDiscussionScopesAllSourcesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, `{"id":7,"body":"text","user":{"login":"octocat"},"updated_at":"2026-01-01T00:00:00Z"}`)

	items, err := GetPRDiscussion(context.Background(), repoRoot, "ghe.example.com", "9")
	if err != nil {
		t.Fatalf("GetPRDiscussion() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("discussion = %#v", items)
	}
	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 3 {
		t.Fatalf("gh calls = %d, want 3", len(cwds))
	}
	for _, cwd := range cwds {
		if cwd != resolvedDir(t, repoRoot) {
			t.Fatalf("gh cwd = %q, want %q", cwd, resolvedDir(t, repoRoot))
		}
	}
	args := readCapturedArgs(t, scriptDir)
	for _, invocation := range args {
		if !strings.Contains(invocation, "api --hostname ghe.example.com --paginate") {
			t.Fatalf("gh args = %v", args)
		}
	}
}

func TestSubmitPRReviewWithIDReturnsRecordedObject(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGHRoutedByArgs(t, map[string]string{
		"repo view":      `https://ghe.example.com/octocat/example`,
		"api --hostname": `{"id":123}`,
	})

	id, err := SubmitPRReviewWithID(context.Background(), repoRoot, "9", "body", false)
	if err != nil {
		t.Fatalf("SubmitPRReviewWithID() error = %v", err)
	}
	if id != (DiscussionID{Kind: "review", ID: 123}) {
		t.Fatalf("id = %#v", id)
	}
	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 2 || cwds[0] != resolvedDir(t, repoRoot) || cwds[1] != resolvedDir(t, repoRoot) {
		t.Fatalf("gh cwd = %v", cwds)
	}
	args := readCapturedArgs(t, scriptDir)
	if len(args) != 2 || !strings.Contains(args[1], "api --hostname ghe.example.com --method POST repos/{owner}/{repo}/pulls/9/reviews --input -") {
		t.Fatalf("gh args = %v", args)
	}
}

func TestSubmitPRReviewKeepsLegacyCLIPath(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, "")

	if err := SubmitPRReview(context.Background(), repoRoot, "9", "body", false); err != nil {
		t.Fatalf("SubmitPRReview() error = %v", err)
	}
	args := readCapturedArgs(t, scriptDir)
	if len(args) != 1 || args[0] != "pr review 9 --comment --body-file -" {
		t.Fatalf("gh args = %v", args)
	}
}
