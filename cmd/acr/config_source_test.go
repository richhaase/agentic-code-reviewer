package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestResolveTrustedReviewConfigSourceUsesSoleConfiguredRemoteDefaultBranch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	originRoot := filepath.Join(root, "origin.git")
	seedRoot := filepath.Join(root, "seed")
	workingRoot := filepath.Join(root, "working")
	if err := os.MkdirAll(seedRoot, 0755); err != nil {
		t.Fatal(err)
	}
	runConfigSourceGit(t, seedRoot, "init", "-b", "main")
	runConfigSourceGit(t, seedRoot, "config", "user.email", "test@example.com")
	runConfigSourceGit(t, seedRoot, "config", "user.name", "Test User")
	writeReviewConfigSourceFile(t, seedRoot, config.ConfigFileName, "reviewers: 8\nguidance_file: guidance/review.md\n")
	writeReviewConfigSourceFile(t, seedRoot, "guidance/review.md", "trusted guidance")
	runConfigSourceGit(t, seedRoot, "add", ".")
	runConfigSourceGit(t, seedRoot, "commit", "-m", "trusted")
	runConfigSourceGit(t, root, "clone", "--bare", seedRoot, originRoot)
	runConfigSourceGit(t, root, "clone", originRoot, workingRoot)
	runConfigSourceGit(t, workingRoot, "remote", "rename", "origin", "upstream")
	runConfigSourceGit(t, workingRoot, "checkout", "-b", "incoming")
	writeReviewConfigSourceFile(t, workingRoot, config.ConfigFileName, "reviewers: [broken\n")
	writeReviewConfigSourceFile(t, workingRoot, "guidance/review.md", "target guidance")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingRoot); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	source, err := resolveTrustedReviewConfigSource(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := config.Resolve(result.Config, config.EnvState{}, config.FlagState{}, config.ResolvedConfig{})
	if resolved.Reviewers != 8 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	guidance, err := config.ResolveGuidanceFromLoadResult(ctx, result, config.EnvState{}, config.FlagState{}, config.ResolvedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if guidance != "trusted guidance" {
		t.Fatalf("guidance = %q", guidance)
	}
	if result.Source.Ref != "upstream/main" {
		t.Fatalf("source ref = %q", result.Source.Ref)
	}
}

func TestResolveTrustedReviewConfigSourceUsesLocalMainWithoutRemote(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	runConfigSourceGit(t, repositoryRoot, "init", "-b", "main")
	runConfigSourceGit(t, repositoryRoot, "config", "user.email", "test@example.com")
	runConfigSourceGit(t, repositoryRoot, "config", "user.name", "Test User")
	writeReviewConfigSourceFile(t, repositoryRoot, config.ConfigFileName, "reviewers: 9\n")
	runConfigSourceGit(t, repositoryRoot, "add", ".")
	runConfigSourceGit(t, repositoryRoot, "commit", "-m", "trusted")
	runConfigSourceGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeReviewConfigSourceFile(t, repositoryRoot, config.ConfigFileName, "reviewers: 1\n")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repositoryRoot); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	source, err := resolveTrustedReviewConfigSource(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := config.Resolve(result.Config, config.EnvState{}, config.FlagState{}, config.ResolvedConfig{})
	if resolved.Reviewers != 9 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
}

func TestResolveTrustedReviewConfigSourceUsesDefaultsWithoutCanonicalBranch(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	runConfigSourceGit(t, repositoryRoot, "init", "-b", "feature")
	runConfigSourceGit(t, repositoryRoot, "config", "user.email", "test@example.com")
	runConfigSourceGit(t, repositoryRoot, "config", "user.name", "Test User")
	writeReviewConfigSourceFile(t, repositoryRoot, config.ConfigFileName, "reviewers: 1\n")
	runConfigSourceGit(t, repositoryRoot, "add", ".")
	runConfigSourceGit(t, repositoryRoot, "commit", "-m", "target")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repositoryRoot); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	source, err := resolveTrustedReviewConfigSource(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := config.Resolve(result.Config, config.EnvState{}, config.FlagState{}, config.ResolvedConfig{})
	if resolved.Reviewers != config.Defaults.Reviewers {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	if result.Source.Kind != config.SourceKindDefaults {
		t.Fatalf("source = %+v", result.Source)
	}
}

func TestResolveTrustedReviewConfigSourceAllowsExplicitExclusion(t *testing.T) {
	ctx := context.Background()
	source, err := resolveTrustedReviewConfigSource(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.Kind != config.SourceKindDisabled {
		t.Fatalf("source = %+v", result.Source)
	}
}

func runConfigSourceGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeReviewConfigSourceFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	filePath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
