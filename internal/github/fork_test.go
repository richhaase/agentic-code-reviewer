package github

import (
	"testing"
)

func TestParseForkNotation_Valid(t *testing.T) {
	username, branch, ok := ParseForkNotation("yunidbauza:feat/enable-pr-number-review")
	if !ok {
		t.Error("expected ok=true for valid fork notation")
	}
	if username != "yunidbauza" {
		t.Errorf("expected username 'yunidbauza', got %q", username)
	}
	if branch != "feat/enable-pr-number-review" {
		t.Errorf("expected branch 'feat/enable-pr-number-review', got %q", branch)
	}
}

func TestParseForkNotation_NotForkNotation(t *testing.T) {
	_, _, ok := ParseForkNotation("main")
	if ok {
		t.Error("expected ok=false for non-fork notation")
	}
}

func TestParseForkNotation_EmptyUsername(t *testing.T) {
	_, _, ok := ParseForkNotation(":branch")
	if ok {
		t.Error("expected ok=false for empty username")
	}
}

func TestParseForkNotation_EmptyBranch(t *testing.T) {
	_, _, ok := ParseForkNotation("user:")
	if ok {
		t.Error("expected ok=false for empty branch")
	}
}

func TestParseForkNotation_MultipleColons(t *testing.T) {
	username, branch, ok := ParseForkNotation("user:feat/with:colon")
	if !ok {
		t.Error("expected ok=true for branch with colon")
	}
	if username != "user" {
		t.Errorf("expected username 'user', got %q", username)
	}
	if branch != "feat/with:colon" {
		t.Errorf("expected branch 'feat/with:colon', got %q", branch)
	}
}

func TestBuildForkRef(t *testing.T) {
	// Test the ForkRef construction logic (not the gh CLI call)
	ref := buildForkRef("yunidbauza", "feat/branch", "agentic-code-reviewer", 83)

	if ref.Username != "yunidbauza" {
		t.Errorf("expected username 'yunidbauza', got %q", ref.Username)
	}
	if ref.Branch != "feat/branch" {
		t.Errorf("expected branch 'feat/branch', got %q", ref.Branch)
	}
	if ref.RepoURL != "https://github.com/yunidbauza/agentic-code-reviewer.git" {
		t.Errorf("expected RepoURL with .git suffix, got %q", ref.RepoURL)
	}
	if ref.RemoteName != "fork-yunidbauza" {
		t.Errorf("expected RemoteName 'fork-yunidbauza', got %q", ref.RemoteName)
	}
	if ref.PRNumber != 83 {
		t.Errorf("expected PRNumber 83, got %d", ref.PRNumber)
	}
}
