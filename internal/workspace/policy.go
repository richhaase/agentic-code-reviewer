package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/github"
)

var ErrPostingDisabled = errors.New("posting is disabled in workspace configuration")

func (c Config) Validate() []string {
	var problems []string

	if c.SchemaVersion != CurrentSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema_version: unsupported version %d (this build supports version %d)", c.SchemaVersion, CurrentSchemaVersion))
	}

	if strings.TrimSpace(c.Identity.ExpectedUser) == "" {
		problems = append(problems, "identity.expected_user is required")
	}

	switch c.Behavior.OwnPRPolicy {
	case OwnPRPolicyDisabled, OwnPRPolicyCommentOnly:
	default:
		problems = append(problems, fmt.Sprintf("behavior.own_pr_policy: invalid value %q (must be %q or %q)", c.Behavior.OwnPRPolicy, OwnPRPolicyDisabled, OwnPRPolicyCommentOnly))
	}

	if c.Behavior.Concurrency < 0 {
		problems = append(problems, "behavior.concurrency must not be negative")
	}
	if c.Behavior.PollInterval.AsDuration() < 0 {
		problems = append(problems, "behavior.poll_interval must not be negative")
	}
	if c.Behavior.SettleTime.AsDuration() < 0 {
		problems = append(problems, "behavior.settle_time must not be negative")
	}

	problems = append(problems, validateNonEmptyEntries("scope.organizations", c.Scope.Organizations)...)
	problems = append(problems, validateNonEmptyEntries("scope.teams", c.Scope.Teams)...)
	problems = append(problems, validateNonEmptyEntries("scope.repository_roots", c.Scope.RepositoryRoots)...)
	problems = append(problems, validateNonEmptyEntries("scope.include", c.Scope.Include)...)
	problems = append(problems, validateNonEmptyEntries("scope.exclude", c.Scope.Exclude)...)

	for key, value := range c.Scope.PathOverrides {
		if strings.TrimSpace(key) == "" {
			problems = append(problems, "scope.path_overrides must not contain an empty owner/repo key")
		}
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Sprintf("scope.path_overrides[%q] must not be empty", key))
		}
	}

	return problems
}

func validateNonEmptyEntries(field string, entries []string) []string {
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			return []string{fmt.Sprintf("%s must not contain empty entries", field)}
		}
	}
	return nil
}

func (c Config) RequirePosting() error {
	if !c.Posting.Enabled {
		return ErrPostingDisabled
	}
	return nil
}

func CheckIdentity(ctx context.Context, c Config) error {
	return matchIdentity(github.GetCurrentUser(ctx), c.Identity.ExpectedUser)
}

func matchIdentity(actual, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return fmt.Errorf("workspace identity check failed: identity.expected_user is not configured")
	}

	if actual == "" {
		return fmt.Errorf("workspace identity check failed: unable to determine the authenticated GitHub user (is gh installed and authenticated?)")
	}

	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("workspace identity check failed: authenticated GitHub user %q does not match configured identity.expected_user %q", actual, expected)
	}

	return nil
}
