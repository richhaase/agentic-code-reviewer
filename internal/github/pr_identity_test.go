package github

import "testing"

func TestParsePullRequestKey(t *testing.T) {
	key, err := parsePullRequestKey([]byte(`{"number":195,"url":"https://github.com/richhaase/agentic-code-reviewer/pull/195"}`))
	if err != nil {
		t.Fatal(err)
	}
	if key.Host != "github.com" || key.Owner != "richhaase" || key.Repository != "agentic-code-reviewer" || key.Number != 195 {
		t.Fatalf("key = %#v", key)
	}
}

func TestParsePullRequestKeyRejectsMismatchedNumber(t *testing.T) {
	if _, err := parsePullRequestKey([]byte(`{"number":195,"url":"https://github.com/richhaase/agentic-code-reviewer/pull/194"}`)); err == nil {
		t.Fatal("mismatched pull request identity was accepted")
	}
}

func TestParsePullRequestKeyRejectsUnexpectedURL(t *testing.T) {
	if _, err := parsePullRequestKey([]byte(`{"number":195,"url":"https://github.com/richhaase/agentic-code-reviewer/issues/195"}`)); err == nil {
		t.Fatal("unexpected pull request URL was accepted")
	}
}
