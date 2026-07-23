package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func newPolicyResolveRepository(t *testing.T, trustedAdjudicationConfig string) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	runPolicyResolveGit(t, repositoryRoot, "init", "-b", "main")
	runPolicyResolveGit(t, repositoryRoot, "config", "user.email", "test@example.com")
	runPolicyResolveGit(t, repositoryRoot, "config", "user.name", "Test User")
	writePolicyResolveFile(t, repositoryRoot, config.ConfigFileName, trustedAdjudicationConfig)
	runPolicyResolveGit(t, repositoryRoot, "add", ".")
	runPolicyResolveGit(t, repositoryRoot, "commit", "-m", "trusted")
	return repositoryRoot
}

func writePolicyResolveFile(t *testing.T, repositoryRoot, relativePath, content string) {
	t.Helper()
	filePath := filepath.Join(repositoryRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPolicyResolveGit(t *testing.T, repositoryRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repositoryRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func policyResolveGitOutput(t *testing.T, repositoryRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repositoryRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const trustedAdjudicationConfig = `adjudication:
  max_iterations: 5
  max_cost_usd: 10
  stop_on_clean_run: true
  stop_on_no_new_findings: true
  evaluation_guidance: trusted guidance
`

func TestResolveAdjudicationPolicy_IgnoresConflictingWorktreeConfig(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newPolicyResolveRepository(t, trustedAdjudicationConfig)

	runPolicyResolveGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writePolicyResolveFile(t, repositoryRoot, config.ConfigFileName, `adjudication:
  max_iterations: 999
  max_cost_usd: 999
  stop_on_clean_run: false
  stop_on_no_new_findings: false
  evaluation_guidance: attacker-supplied guidance
`)
	runPolicyResolveGit(t, repositoryRoot, "add", ".")
	runPolicyResolveGit(t, repositoryRoot, "commit", "-m", "reviewed pr head")
	reviewedHead := policyResolveGitOutput(t, repositoryRoot, "rev-parse", "HEAD")

	source, err := config.NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
	if err != nil {
		t.Fatalf("NewRepositoryRevisionSource: %v", err)
	}

	target := ReviewTargetV1{
		WorktreeRoot: repositoryRoot,
		Revision:     RevisionEvidenceV1{HeadObjectID: reviewedHead},
	}

	policy, _, err := ResolveAdjudicationPolicy(ctx, source, target)
	if err != nil {
		t.Fatalf("ResolveAdjudicationPolicy: %v", err)
	}

	if policy.Budget.MaxIterations != 5 || policy.Budget.MaxCostUSD != 10 {
		t.Fatalf("expected trusted budget policy, got %+v; the reviewed head's worktree config must never supply this data", policy.Budget)
	}
	if !policy.Stop.StopOnCleanRun || !policy.Stop.StopOnNoNewFindings {
		t.Fatalf("expected trusted stop policy, got %+v", policy.Stop)
	}
	if policy.EvaluationGuidance != "trusted guidance" {
		t.Fatalf("expected trusted evaluation guidance, got %q", policy.EvaluationGuidance)
	}
}

func TestResolveAdjudicationPolicy_RejectsSourcePinnedToReviewedHead(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newPolicyResolveRepository(t, trustedAdjudicationConfig)

	runPolicyResolveGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writePolicyResolveFile(t, repositoryRoot, config.ConfigFileName, `adjudication:
  max_iterations: 999
  evaluation_guidance: attacker-supplied guidance via own head
`)
	runPolicyResolveGit(t, repositoryRoot, "add", ".")
	runPolicyResolveGit(t, repositoryRoot, "commit", "-m", "reviewed pr head")
	reviewedHead := policyResolveGitOutput(t, repositoryRoot, "rev-parse", "HEAD")

	source, err := config.NewRepositoryRevisionSource(ctx, repositoryRoot, "incoming")
	if err != nil {
		t.Fatalf("NewRepositoryRevisionSource: %v", err)
	}

	target := ReviewTargetV1{
		WorktreeRoot: repositoryRoot,
		Revision:     RevisionEvidenceV1{HeadObjectID: reviewedHead},
	}

	if _, _, err := ResolveAdjudicationPolicy(ctx, source, target); err == nil {
		t.Fatal("expected an error resolving adjudication policy from a source pinned to the reviewed pull request's own head")
	}
}

type filesystemPolicySource struct {
	config *config.Config
}

func (s filesystemPolicySource) LoadWithWarnings(_ context.Context) (*config.LoadResult, error) {
	return &config.LoadResult{
		Config: s.config,
		Source: config.SourceIdentity{
			Kind:    config.SourceKindFilesystem,
			Locator: "/reviewed/pr/worktree/.acr.yaml",
		},
	}, nil
}

func TestResolveAdjudicationPolicy_RejectsFilesystemSourcedConfiguration(t *testing.T) {
	maxIterations := 999
	guidance := "attacker-supplied guidance via raw worktree read"
	source := filesystemPolicySource{
		config: &config.Config{
			Adjudication: config.AdjudicationConfig{
				MaxIterations:      &maxIterations,
				EvaluationGuidance: &guidance,
			},
		},
	}

	target := ReviewTargetV1{Revision: RevisionEvidenceV1{HeadObjectID: "some-head"}}

	if _, _, err := ResolveAdjudicationPolicy(context.Background(), source, target); err == nil {
		t.Fatal("expected an error resolving adjudication policy from a filesystem-kind source; a reviewed PR's own worktree must never supply this data")
	}
}
