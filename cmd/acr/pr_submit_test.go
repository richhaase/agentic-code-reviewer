package main

import (
	"errors"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

var errTransient = errors.New("transient gh failure")

func TestPrContext_Defaults(t *testing.T) {
	pc := prContext{}
	if pc.number != "" {
		t.Errorf("default number = %q, want empty", pc.number)
	}
	if pc.isSelfReview {
		t.Error("default isSelfReview = true, want false")
	}
	if pc.err != nil {
		t.Errorf("default err = %v, want nil", pc.err)
	}
}

func TestPrContext_WithAuthError(t *testing.T) {
	pc := prContext{err: github.ErrAuthFailed}
	if pc.err != github.ErrAuthFailed {
		t.Errorf("err = %v, want ErrAuthFailed", pc.err)
	}
}

func TestPrContext_WithNoPRError(t *testing.T) {
	pc := prContext{err: github.ErrNoPRFound}
	if pc.err != github.ErrNoPRFound {
		t.Errorf("err = %v, want ErrNoPRFound", pc.err)
	}
}

func TestPrependUserNote(t *testing.T) {
	body := "## Review\nSome findings here."

	t.Run("prepends note with separator", func(t *testing.T) {
		got := prependUserNote(body, "1 is low priority, 2 looks good")
		want := "**Reviewer's note:** 1 is low priority, 2 looks good\n\n---\n\n## Review\nSome findings here."
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("empty note still wraps body with prefix", func(t *testing.T) {
		// prependUserNote is only called with non-empty notes,
		// but verify the format is still valid
		got := prependUserNote(body, "")
		if got == body {
			t.Error("expected formatted output even with empty note")
		}
	})
}

func TestLgtmAction_Constants(t *testing.T) {
	if actionApprove == actionComment {
		t.Error("actionApprove should not equal actionComment")
	}
	if actionApprove == actionSkip {
		t.Error("actionApprove should not equal actionSkip")
	}
	if actionComment == actionSkip {
		t.Error("actionComment should not equal actionSkip")
	}
}

func TestRetrySubmission(t *testing.T) {
	oldDelay := submissionRetryDelay
	submissionRetryDelay = 0
	defer func() { submissionRetryDelay = oldDelay }()
	logger := terminal.NewLogger()

	// Watch mode: transient failures are retried until success.
	calls := 0
	err := retrySubmission(func() error {
		calls++
		if calls < 3 {
			return errTransient
		}
		return nil
	}, true, logger)
	if err != nil || calls != 3 {
		t.Errorf("watch mode: err = %v, calls = %d; want success on attempt 3", err, calls)
	}

	// Watch mode: persistent failure surfaces after the attempt budget.
	calls = 0
	err = retrySubmission(func() error { calls++; return errTransient }, true, logger)
	if err == nil || calls != submissionAttempts {
		t.Errorf("watch mode persistent: err = %v, calls = %d; want error after %d attempts", err, calls, submissionAttempts)
	}

	// One-shot mode: the first error returns unchanged, no retries.
	calls = 0
	err = retrySubmission(func() error { calls++; return errTransient }, false, logger)
	if err == nil || calls != 1 {
		t.Errorf("one-shot: err = %v, calls = %d; want single attempt", err, calls)
	}
}
