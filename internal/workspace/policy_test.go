package workspace

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		Identity:      IdentityConfig{ExpectedUser: "octocat"},
		Behavior:      BehaviorConfig{OwnPRPolicy: OwnPRPolicyDisabled},
	}
}

func TestValidate_AcceptsMinimalValidConfig(t *testing.T) {
	if problems := validConfig().Validate(); len(problems) != 0 {
		t.Fatalf("expected no problems, got: %v", problems)
	}
}

func TestValidate_RequiresExpectedUser(t *testing.T) {
	cfg := validConfig()
	cfg.Identity.ExpectedUser = ""

	if !containsSubstring(cfg.Validate(), "expected_user") {
		t.Fatal("expected a problem for missing identity.expected_user")
	}
}

func TestValidate_RejectsUnsupportedSchemaVersion(t *testing.T) {
	cfg := validConfig()
	cfg.SchemaVersion = 999

	if !containsSubstring(cfg.Validate(), "schema_version") {
		t.Fatal("expected a schema_version problem")
	}
}

func TestValidate_RejectsInvalidOwnPRPolicy(t *testing.T) {
	cfg := validConfig()
	cfg.Behavior.OwnPRPolicy = "approve"

	if !containsSubstring(cfg.Validate(), "own_pr_policy") {
		t.Fatal("expected an own_pr_policy problem")
	}
}

func TestValidate_AcceptsCommentOnlyOwnPRPolicy(t *testing.T) {
	cfg := validConfig()
	cfg.Behavior.OwnPRPolicy = OwnPRPolicyCommentOnly

	if problems := cfg.Validate(); len(problems) != 0 {
		t.Fatalf("expected no problems, got: %v", problems)
	}
}

func TestValidate_RejectsNegativeConcurrency(t *testing.T) {
	cfg := validConfig()
	cfg.Behavior.Concurrency = -1

	if !containsSubstring(cfg.Validate(), "concurrency") {
		t.Fatal("expected a concurrency problem")
	}
}

func TestValidate_RejectsEmptyScopeEntries(t *testing.T) {
	cfg := validConfig()
	cfg.Scope.Organizations = []string{"acme", ""}

	if !containsSubstring(cfg.Validate(), "scope.organizations") {
		t.Fatal("expected a scope.organizations problem")
	}
}

func TestValidate_RejectsEmptyPathOverrideValue(t *testing.T) {
	cfg := validConfig()
	cfg.Scope.PathOverrides = map[string]string{"acme/widgets": ""}

	if !containsSubstring(cfg.Validate(), "path_overrides") {
		t.Fatal("expected a path_overrides problem")
	}
}

func TestRequirePosting_DisabledByDefault(t *testing.T) {
	cfg := validConfig()

	if err := cfg.RequirePosting(); err != ErrPostingDisabled {
		t.Fatalf("expected ErrPostingDisabled, got: %v", err)
	}
}

func TestRequirePosting_AllowsWhenEnabled(t *testing.T) {
	cfg := validConfig()
	cfg.Posting.Enabled = true

	if err := cfg.RequirePosting(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestMatchIdentity_MatchingSameCase(t *testing.T) {
	if err := matchIdentity("octocat", "octocat"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestMatchIdentity_MatchingDifferentCase(t *testing.T) {
	if err := matchIdentity("OctoCat", "octocat"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestMatchIdentity_Mismatch(t *testing.T) {
	if err := matchIdentity("hubot", "octocat"); err == nil {
		t.Fatal("expected error for mismatched identity")
	}
}

func TestMatchIdentity_FailsClosedWhenActualUnknown(t *testing.T) {
	if err := matchIdentity("", "octocat"); err == nil {
		t.Fatal("expected error when the authenticated user cannot be determined")
	}
}

func TestMatchIdentity_FailsClosedWhenExpectedUnconfigured(t *testing.T) {
	if err := matchIdentity("octocat", ""); err == nil {
		t.Fatal("expected error when identity.expected_user is not configured")
	}
}

func containsSubstring(problems []string, substr string) bool {
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}
