package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepositoryRevisionSourceIgnoresTargetConfiguration(t *testing.T) {
	trustedConfig := "reviewers: 7\nguidance_file: guidance/trusted.md\nfilters:\n  exclude_patterns:\n    - trusted-pattern\n"
	tests := []struct {
		name         string
		targetConfig *string
		targetFiles  map[string]string
	}{
		{
			name:         "conflicting",
			targetConfig: stringPointer("reviewers: 1\nguidance_file: guidance/target.md\n"),
			targetFiles:  map[string]string{"guidance/target.md": "target guidance"},
		},
		{
			name:         "broken",
			targetConfig: stringPointer("reviewers: [broken\n"),
		},
		{
			name: "missing",
		},
		{
			name:         "redirecting",
			targetConfig: stringPointer("reviewers: 1\nguidance_file: ../target-guidance.md\n"),
			targetFiles:  map[string]string{"target-guidance.md": "target guidance"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repositoryRoot := newConfigSourceRepository(t, trustedConfig, map[string]string{
				"guidance/trusted.md": "trusted guidance",
			})
			runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
			if tt.targetConfig == nil {
				if err := os.Remove(filepath.Join(repositoryRoot, ConfigFileName)); err != nil {
					t.Fatal(err)
				}
			} else {
				writeConfigSourceFile(t, repositoryRoot, ConfigFileName, *tt.targetConfig)
			}
			for path, content := range tt.targetFiles {
				writeConfigSourceFile(t, repositoryRoot, path, content)
			}

			source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
			if err != nil {
				t.Fatal(err)
			}
			result, err := source.LoadWithWarnings(ctx)
			if err != nil {
				t.Fatalf("LoadWithWarnings() error = %v", err)
			}

			resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
			if resolved.Reviewers != 7 {
				t.Fatalf("Reviewers = %d", resolved.Reviewers)
			}
			if patterns := Merge(result.Config, nil); len(patterns) != 1 || patterns[0] != "trusted-pattern" {
				t.Fatalf("patterns = %v", patterns)
			}

			guidance, err := ResolveGuidanceFromLoadResult(ctx, result, EnvState{}, FlagState{}, ResolvedConfig{})
			if err != nil {
				t.Fatalf("ResolveGuidanceFromLoadResult() error = %v", err)
			}
			if guidance != "trusted guidance" {
				t.Fatalf("guidance = %q", guidance)
			}
			if result.Source.Ref != "main" || result.Source.Revision != source.Revision || !result.Source.ConfigPresent || result.Source.ConfigDigest == "" {
				t.Fatalf("source identity = %+v", result.Source)
			}
		})
	}
}

func TestRepositoryRevisionSourceUsesDefaultsWhenTrustedConfigIsAbsent(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newConfigSourceRepository(t, "", nil)
	runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeConfigSourceFile(t, repositoryRoot, ConfigFileName, "reviewers: 1\n")

	source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != Defaults.Reviewers {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	if result.Source.ConfigPresent {
		t.Fatalf("source identity = %+v", result.Source)
	}
}

func TestRepositoryRevisionSourceFailsClosedForInvalidTrustedConfig(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newConfigSourceRepository(t, "reviewers: [broken\n", nil)
	runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeConfigSourceFile(t, repositoryRoot, ConfigFileName, "reviewers: 1\n")

	source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.LoadWithWarnings(ctx); err == nil {
		t.Fatal("LoadWithWarnings succeeded")
	}
}

func TestRepositoryRevisionSourceFailsClosedForMissingTrustedGuidance(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newConfigSourceRepository(t, "guidance_file: guidance/review.md\n", nil)
	runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeConfigSourceFile(t, repositoryRoot, "guidance/review.md", "target guidance")

	source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveGuidanceFromLoadResult(ctx, result, EnvState{}, FlagState{}, ResolvedConfig{}); err == nil {
		t.Fatal("ResolveGuidanceFromLoadResult succeeded")
	}
}

func TestRepositoryRevisionSourceResolvesTrustedSymlinksWithinRevision(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newConfigSourceRepository(t, "", map[string]string{
		"config/trusted.yaml":     "reviewers: 9\nguidance_file: guidance/review.md\n",
		"guidance/trusted-review": "trusted guidance",
	})
	if err := os.Symlink("config/trusted.yaml", filepath.Join(repositoryRoot, ConfigFileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("trusted-review", filepath.Join(repositoryRoot, "guidance", "review.md")); err != nil {
		t.Fatal(err)
	}
	runConfigGit(t, repositoryRoot, "add", ".")
	runConfigGit(t, repositoryRoot, "commit", "-m", "trusted symlinks")

	source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, "main")
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != 9 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	guidance, err := ResolveGuidanceFromLoadResult(ctx, result, EnvState{}, FlagState{}, ResolvedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if guidance != "trusted guidance" {
		t.Fatalf("guidance = %q", guidance)
	}
}

func TestResolveTrustedSourceUsesExplicitRepositoryOutsideCurrentDirectory(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := newConfigSourceRepository(t, "reviewers: 11\n", nil)
	runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeConfigSourceFile(t, repositoryRoot, ConfigFileName, "reviewers: 1\n")

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDirectory)

	source, err := ResolveTrustedSource(ctx, TrustedSourceRequest{
		RepositoryRoot: repositoryRoot,
		Branch:         "main",
		Policy:         CanonicalNamedBranch,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != 11 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	if result.Source.Locator != repositoryRoot {
		t.Fatalf("source = %+v", result.Source)
	}
}

func TestResolveTrustedSourceUsesFullyQualifiedRemoteTrackingRef(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	seedRoot := filepath.Join(root, "seed")
	remoteRoot := filepath.Join(root, "origin.git")
	repositoryRoot := filepath.Join(root, "working")
	if err := os.MkdirAll(seedRoot, 0755); err != nil {
		t.Fatal(err)
	}
	runConfigGit(t, seedRoot, "init", "-b", "main")
	runConfigGit(t, seedRoot, "config", "user.email", "test@example.com")
	runConfigGit(t, seedRoot, "config", "user.name", "Test User")
	writeConfigSourceFile(t, seedRoot, ConfigFileName, "reviewers: 12\n")
	runConfigGit(t, seedRoot, "add", ".")
	runConfigGit(t, seedRoot, "commit", "-m", "trusted")
	runConfigGit(t, root, "clone", "--bare", seedRoot, remoteRoot)
	runConfigGit(t, root, "clone", remoteRoot, repositoryRoot)
	runConfigGit(t, repositoryRoot, "config", "user.email", "test@example.com")
	runConfigGit(t, repositoryRoot, "config", "user.name", "Test User")
	runConfigGit(t, repositoryRoot, "checkout", "-b", "incoming")
	writeConfigSourceFile(t, repositoryRoot, ConfigFileName, "reviewers: 1\n")
	runConfigGit(t, repositoryRoot, "add", ".")
	runConfigGit(t, repositoryRoot, "commit", "-m", "incoming")
	runConfigGit(t, repositoryRoot, "tag", "origin/main")

	source, err := ResolveTrustedSource(ctx, TrustedSourceRequest{
		RepositoryRoot: repositoryRoot,
		Remote:         "origin",
		Branch:         "main",
		Policy:         CanonicalNamedBranch,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != 12 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	if result.Source.Ref != "refs/remotes/origin/main" {
		t.Fatalf("source ref = %q", result.Source.Ref)
	}
}

func TestResolveTrustedSourceRefreshesBranchMatchingRemotePrefix(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	seedRoot := filepath.Join(root, "seed")
	remoteRoot := filepath.Join(root, "release.git")
	repositoryRoot := filepath.Join(root, "working")
	if err := os.MkdirAll(seedRoot, 0755); err != nil {
		t.Fatal(err)
	}
	runConfigGit(t, seedRoot, "init", "-b", "main")
	runConfigGit(t, seedRoot, "config", "user.email", "test@example.com")
	runConfigGit(t, seedRoot, "config", "user.name", "Test User")
	writeConfigSourceFile(t, seedRoot, ConfigFileName, "reviewers: 12\n")
	runConfigGit(t, seedRoot, "add", ".")
	runConfigGit(t, seedRoot, "commit", "-m", "initial")
	runConfigGit(t, seedRoot, "checkout", "-b", "release/2.x")
	runConfigGit(t, root, "clone", "--bare", seedRoot, remoteRoot)
	runConfigGit(t, root, "clone", remoteRoot, repositoryRoot)
	runConfigGit(t, repositoryRoot, "remote", "rename", "origin", "release")
	runConfigGit(t, seedRoot, "remote", "add", "origin", remoteRoot)
	writeConfigSourceFile(t, seedRoot, ConfigFileName, "reviewers: 13\n")
	runConfigGit(t, seedRoot, "add", ConfigFileName)
	runConfigGit(t, seedRoot, "commit", "-m", "updated")
	runConfigGit(t, seedRoot, "push", "origin", "release/2.x")

	source, err := ResolveTrustedSource(ctx, TrustedSourceRequest{
		RepositoryRoot: repositoryRoot,
		Remote:         "release",
		Branch:         "release/2.x",
		Policy:         CanonicalNamedBranch,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != 13 {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
	if result.Source.Ref != "refs/remotes/release/release/2.x" {
		t.Fatalf("source ref = %q", result.Source.Ref)
	}
}

func TestResolveTrustedSourceDisabledIdentity(t *testing.T) {
	ctx := context.Background()
	source, err := ResolveTrustedSource(ctx, TrustedSourceRequest{
		RepositoryRoot: "/does/not/exist",
		Disabled:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.LoadWithWarnings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source.Kind != SourceKindDisabled || result.Source.Locator != "--no-config" {
		t.Fatalf("source = %+v", result.Source)
	}
	resolved := Resolve(result.Config, EnvState{}, FlagState{}, ResolvedConfig{})
	if resolved.Reviewers != Defaults.Reviewers {
		t.Fatalf("Reviewers = %d", resolved.Reviewers)
	}
}

func newConfigSourceRepository(t *testing.T, trustedConfig string, trustedFiles map[string]string) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	runConfigGit(t, repositoryRoot, "init", "-b", "main")
	runConfigGit(t, repositoryRoot, "config", "user.email", "test@example.com")
	runConfigGit(t, repositoryRoot, "config", "user.name", "Test User")
	writeConfigSourceFile(t, repositoryRoot, "tracked.txt", "tracked")
	if trustedConfig != "" {
		writeConfigSourceFile(t, repositoryRoot, ConfigFileName, trustedConfig)
	}
	for path, content := range trustedFiles {
		writeConfigSourceFile(t, repositoryRoot, path, content)
	}
	runConfigGit(t, repositoryRoot, "add", ".")
	runConfigGit(t, repositoryRoot, "commit", "-m", "trusted")
	return repositoryRoot
}

func writeConfigSourceFile(t *testing.T, repositoryRoot, relativePath, content string) {
	t.Helper()
	filePath := filepath.Join(repositoryRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runConfigGit(t *testing.T, repositoryRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repositoryRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func stringPointer(value string) *string {
	return &value
}
