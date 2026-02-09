package main

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/github"
)

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
