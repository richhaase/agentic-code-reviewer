package github

import (
	"errors"
	"os/exec"
	"testing"
)

func TestParseCIChecks_AllPassed(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "pass"},
		{"name": "test", "bucket": "pass"}
	]`

	status := ParseCIChecks([]byte(json))

	if !status.AllPassed {
		t.Error("expected AllPassed to be true")
	}
	if len(status.Pending) != 0 {
		t.Errorf("expected no pending checks, got %v", status.Pending)
	}
	if len(status.Failed) != 0 {
		t.Errorf("expected no failed checks, got %v", status.Failed)
	}
	if status.Error != "" {
		t.Errorf("expected no error, got %q", status.Error)
	}
}

func TestParseCIChecks_WithPending(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "deploy", "bucket": "pending"},
		{"name": "e2e", "bucket": "pending"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with pending checks")
	}
	if len(status.Pending) != 2 {
		t.Errorf("expected 2 pending checks, got %d", len(status.Pending))
	}
	// Verify the actual check names are captured
	found := map[string]bool{}
	for _, name := range status.Pending {
		found[name] = true
	}
	if !found["deploy"] || !found["e2e"] {
		t.Errorf("pending checks should contain 'deploy' and 'e2e', got %v", status.Pending)
	}
}

func TestParseCIChecks_WithFailures(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "fail"},
		{"name": "security", "bucket": "fail"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with failures")
	}
	if len(status.Failed) != 2 {
		t.Errorf("expected 2 failed checks, got %d", len(status.Failed))
	}
	found := map[string]bool{}
	for _, name := range status.Failed {
		found[name] = true
	}
	if !found["lint"] || !found["security"] {
		t.Errorf("failed checks should contain 'lint' and 'security', got %v", status.Failed)
	}
}

func TestParseCIChecks_SkippingTreatedAsPass(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "optional-check", "bucket": "skipping"}
	]`

	status := ParseCIChecks([]byte(json))

	if !status.AllPassed {
		t.Error("expected AllPassed to be true with skipping checks")
	}
	if len(status.Failed) != 0 {
		t.Errorf("skipping should not be in failed, got %v", status.Failed)
	}
}

func TestParseCIChecks_CancelTreatedAsFailure(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "slow-test", "bucket": "cancel"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with canceled check")
	}
	if len(status.Failed) != 1 || status.Failed[0] != "slow-test" {
		t.Errorf("canceled check should be in failed, got %v", status.Failed)
	}
}

func TestParseCIChecks_CaseInsensitiveBucket(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "PASS"},
		{"name": "lint", "bucket": "Pass"},
		{"name": "test", "bucket": "PENDING"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with pending (uppercase)")
	}
	if len(status.Pending) != 1 || status.Pending[0] != "test" {
		t.Errorf("expected 'test' in pending, got %v", status.Pending)
	}
}

func TestParseCIChecks_EmptyChecks(t *testing.T) {
	json := `[]`

	status := ParseCIChecks([]byte(json))

	// No CI checks configured - should allow approval
	if !status.AllPassed {
		t.Error("expected AllPassed to be true when no checks exist")
	}
}

func TestParseCIChecks_InvalidJSON(t *testing.T) {
	status := ParseCIChecks([]byte(`not valid json`))

	if status.Error == "" {
		t.Error("expected error for invalid JSON")
	}
	if status.AllPassed {
		t.Error("AllPassed should be false on parse error")
	}
}

func TestParseCIChecks_MixedStatuses(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "fail"},
		{"name": "deploy", "bucket": "pending"},
		{"name": "optional", "bucket": "skipping"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with mixed statuses")
	}
	if len(status.Pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(status.Pending))
	}
	if len(status.Failed) != 1 {
		t.Errorf("expected 1 failed, got %d", len(status.Failed))
	}
}

func TestParseCIChecks_UnknownBucketTreatedAsFailure(t *testing.T) {
	json := `[
		{"name": "custom-check", "bucket": "unknown_status"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with unknown bucket")
	}
	if len(status.Failed) != 1 {
		t.Errorf("unknown bucket should be treated as failure, got %v failed", status.Failed)
	}
}

// Tests for self-review functions

func TestCheckSelfReview_MatchingSameCase(t *testing.T) {
	result := checkSelfReview("octocat", "octocat")
	if !result {
		t.Error("expected true for matching usernames with same case")
	}
}

func TestCheckSelfReview_MatchingDifferentCase(t *testing.T) {
	result := checkSelfReview("OctoCat", "octocat")
	if !result {
		t.Error("expected true for matching usernames with different case")
	}
}

func TestCheckSelfReview_MatchingAllCaps(t *testing.T) {
	result := checkSelfReview("OCTOCAT", "octocat")
	if !result {
		t.Error("expected true for all caps vs lowercase")
	}
}

func TestCheckSelfReview_DifferentUsers(t *testing.T) {
	result := checkSelfReview("octocat", "hubot")
	if result {
		t.Error("expected false for different usernames")
	}
}

func TestCheckSelfReview_EmptyCurrentUser(t *testing.T) {
	// Fail closed: assume self-review when current user lookup fails
	result := checkSelfReview("", "octocat")
	if !result {
		t.Error("expected true when current user is empty (fail closed)")
	}
}

func TestCheckSelfReview_EmptyPRAuthor(t *testing.T) {
	// Fail closed: assume self-review when PR author lookup fails
	result := checkSelfReview("octocat", "")
	if !result {
		t.Error("expected true when PR author is empty (fail closed)")
	}
}

func TestCheckSelfReview_BothEmpty(t *testing.T) {
	// Fail closed: assume self-review when both lookups fail
	result := checkSelfReview("", "")
	if !result {
		t.Error("expected true when both are empty (fail closed)")
	}
}

func TestCheckSelfReview_WhitespaceOnly(t *testing.T) {
	// Whitespace-only strings are not empty but should not match valid usernames
	result := checkSelfReview("  ", "octocat")
	if result {
		t.Error("expected false when current user is whitespace (not a match)")
	}

	result = checkSelfReview("octocat", "  ")
	if result {
		t.Error("expected false when PR author is whitespace (not a match)")
	}
}

func TestCheckSelfReview_PartialMatch(t *testing.T) {
	result := checkSelfReview("octo", "octocat")
	if result {
		t.Error("expected false for partial username match")
	}
}

func TestCheckSelfReview_UsernameWithNumbers(t *testing.T) {
	result := checkSelfReview("user123", "USER123")
	if !result {
		t.Error("expected true for matching usernames with numbers (case insensitive)")
	}
}

func TestCheckSelfReview_UsernameWithHyphens(t *testing.T) {
	result := checkSelfReview("octo-cat", "OCTO-CAT")
	if !result {
		t.Error("expected true for matching usernames with hyphens (case insensitive)")
	}
}

// Tests for classifyGHError

func TestClassifyGHError_NoPRFound(t *testing.T) {
	// Create an ExitError with stderr indicating no PR found
	exitErr := &exec.ExitError{
		Stderr: []byte(`no pull requests found for branch "feature-branch"`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrNoPRFound) {
		t.Errorf("expected ErrNoPRFound, got %v", err)
	}
}

func TestClassifyGHError_AuthFailed401(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`HTTP 401: Bad credentials (https://api.github.com/graphql)`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_AuthFailedLogin(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`To get started with GitHub CLI, please run:  gh auth login`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_AuthFailedCredentials(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`error: authentication required, check your credentials`),
	}

	err := classifyGHError(exitErr)

	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestClassifyGHError_OtherError(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte(`repository not found`),
	}

	err := classifyGHError(exitErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected generic error, got %v", err)
	}
	if err == nil {
		t.Error("expected an error, got nil")
	}
}

func TestClassifyGHError_EmptyStderr(t *testing.T) {
	exitErr := &exec.ExitError{
		Stderr: []byte{},
	}

	err := classifyGHError(exitErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected generic error for empty stderr, got %v", err)
	}
}

func TestClassifyGHError_NonExitError(t *testing.T) {
	// Test with a non-ExitError
	plainErr := errors.New("some other error")

	err := classifyGHError(plainErr)

	if errors.Is(err, ErrNoPRFound) || errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected wrapped error for non-ExitError, got %v", err)
	}
}
