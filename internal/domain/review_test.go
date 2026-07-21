package domain

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validReviewConfigurationValues() ReviewConfigurationValues {
	return ReviewConfigurationValues{
		Reviewers:         3,
		Concurrency:       2,
		Timeout:           time.Minute,
		Retries:           1,
		ReviewerAgents:    []string{"codex", "claude"},
		ReviewerModel:     "review-model",
		SummarizerAgent:   "codex",
		SummarizerModel:   "summary-model",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		Guidance:          "focus on correctness",
		FPFilterEnabled:   true,
		FPThreshold:       75,
		PRFeedbackEnabled: true,
		ExcludePatterns:   []string{"generated/"},
	}
}

func TestPullRequestKeyValidate(t *testing.T) {
	valid := PullRequestKey{Host: "github.com", Owner: "owner", Repository: "repo", Number: 42}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	if valid.String() != "github.com/owner/repo#42" {
		t.Fatalf("unexpected key string %q", valid.String())
	}

	invalid := PullRequestKey{}
	err := invalid.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, part := range []string{"host", "owner", "repository", "number"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("validation error %q does not mention %q", err, part)
		}
	}
}

func TestReviewTargetAllowsLocalReviewWithoutPullRequest(t *testing.T) {
	root := t.TempDir()
	target := ReviewTarget{
		RepositoryRoot: root,
		WorktreeRoot:   filepath.Join(root, "worktree"),
		Revision: RevisionEvidence{
			RequestedBaseRef: "main",
			ResolvedBaseRef:  "origin/main",
		},
	}

	if err := target.Validate(); err != nil {
		t.Fatalf("local target rejected: %v", err)
	}
}

func TestReviewConfigurationIsImmutableAndFingerprintIsDeterministic(t *testing.T) {
	values := validReviewConfigurationValues()
	first, err := NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create first configuration: %v", err)
	}
	second, err := NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create second configuration: %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("fingerprints differ: %q != %q", first.Fingerprint(), second.Fingerprint())
	}

	values.ReviewerAgents[0] = "changed"
	values.ExcludePatterns[0] = "changed"
	got := first.Values()
	if got.ReviewerAgents[0] != "codex" {
		t.Fatalf("reviewer agents changed through input alias: %v", got.ReviewerAgents)
	}
	if got.ExcludePatterns[0] != "generated/" {
		t.Fatalf("exclude patterns changed through input alias: %v", got.ExcludePatterns)
	}

	got.ReviewerAgents[0] = "changed-again"
	if first.Values().ReviewerAgents[0] != "codex" {
		t.Fatal("configuration values exposed mutable reviewer agents")
	}
}

func TestReviewConfigurationFingerprintChangesWithSemanticInput(t *testing.T) {
	firstValues := validReviewConfigurationValues()
	secondValues := validReviewConfigurationValues()
	secondValues.FPThreshold = 90

	first, err := NewReviewConfiguration(firstValues)
	if err != nil {
		t.Fatalf("create first configuration: %v", err)
	}
	second, err := NewReviewConfiguration(secondValues)
	if err != nil {
		t.Fatalf("create second configuration: %v", err)
	}
	if first.Fingerprint() == second.Fingerprint() {
		t.Fatal("semantic configuration change did not alter fingerprint")
	}
}

func TestReviewConfigurationRejectsInvalidInputs(t *testing.T) {
	values := validReviewConfigurationValues()
	values.Reviewers = 0
	values.ExcludePatterns = []string{"["}

	_, err := NewReviewConfiguration(values)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "reviewers") || !strings.Contains(err.Error(), "exclude") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestReviewConfigurationRequiresResolvedConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{name: "zero", concurrency: 0},
		{name: "greater than reviewers", concurrency: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := validReviewConfigurationValues()
			values.Concurrency = test.concurrency
			_, err := NewReviewConfiguration(values)
			if err == nil || !strings.Contains(err.Error(), "concurrency") {
				t.Fatalf("expected concurrency validation error, got %v", err)
			}
		})
	}
}

func TestConfigurationSourceIdentityValidation(t *testing.T) {
	valid := []ConfigurationSourceIdentity{
		{Kind: "test", Locator: "fixture", ConfigPresent: true, ConfigDigest: "digest"},
		{Kind: "repository-revision", Locator: "/repo", Ref: "main", Revision: "object"},
		{Kind: "disabled", Locator: "--no-config"},
		{Kind: "defaults", Locator: "configuration absent"},
	}
	for _, source := range valid {
		if err := source.Validate(); err != nil {
			t.Errorf("valid source %#v rejected: %v", source, err)
		}
	}

	invalid := []ConfigurationSourceIdentity{
		{},
		{Kind: "test", ConfigPresent: true},
		{Kind: "test", ConfigDigest: "digest"},
	}
	for _, source := range invalid {
		if err := source.Validate(); err == nil {
			t.Errorf("invalid source %#v accepted", source)
		}
	}
}

func TestReviewRunBuildsPresentationNeutralFinalGroups(t *testing.T) {
	run := ReviewRun{
		Findings: []ReviewFinding{{
			ID:   "finding-001",
			Kind: ReviewFindingActionable,
			Group: FindingGroup{
				Title: "Bug",
			},
		}},
		Info: []ReviewFinding{{
			ID:   "info-001",
			Kind: ReviewFindingInformational,
			Group: FindingGroup{
				Title: "Note",
			},
		}},
	}

	grouped := run.FinalGroupedFindings()
	if len(grouped.Findings) != 1 || grouped.Findings[0].Title != "Bug" {
		t.Fatalf("unexpected actionable groups: %#v", grouped.Findings)
	}
	if len(grouped.Info) != 1 || grouped.Info[0].Title != "Note" {
		t.Fatalf("unexpected informational groups: %#v", grouped.Info)
	}
}
