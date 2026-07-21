package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommitRejectsUnsafeRefs(t *testing.T) {
	repositoryRoot := setupTestRepo(t)
	for _, ref := range []string{"", "   ", "--help"} {
		if _, err := ResolveCommit(context.Background(), repositoryRoot, ref); err == nil {
			t.Errorf("ResolveCommit(%q) succeeded", ref)
		}
	}
}

func TestResolveCommitIgnoresSuccessfulStderr(t *testing.T) {
	repositoryRoot := setupTestRepo(t)
	for _, args := range [][]string{{"branch", "ambiguous"}, {"tag", "ambiguous"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repositoryRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	revision, err := ResolveCommit(context.Background(), repositoryRoot, "ambiguous")
	if err != nil {
		t.Fatal(err)
	}
	if !isObjectID(revision) {
		t.Fatalf("ResolveCommit() = %q", revision)
	}
}

func TestReadFileAtCommitUsesImmutableObjectID(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(repositoryRoot, "test.txt")
	if err := os.WriteFile(filePath, []byte("changed content"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "test content" {
		t.Fatalf("ReadFileAtCommit() = %q", content)
	}
}

func TestReadFileAtCommitRejectsMutableRefAndEscapingPath(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	if _, err := ReadFileAtCommit(ctx, repositoryRoot, "HEAD", "test.txt"); err == nil {
		t.Fatal("ReadFileAtCommit accepted a mutable ref")
	}

	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "../test.txt"); err == nil {
		t.Fatal("ReadFileAtCommit accepted an escaping path")
	}
	if _, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "missing.txt"); !errors.Is(err, ErrPathNotFoundAtRevision) {
		t.Fatalf("ReadFileAtCommit missing error = %v", err)
	}
}

func TestReadFileAtCommitDoesNotTreatRepositoryFailureAsMissingPath(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	_, err = ReadFileAtCommit(ctx, filepath.Join(repositoryRoot, "missing-repository"), commit, "test.txt")
	if err == nil {
		t.Fatal("ReadFileAtCommit succeeded")
	}
	if errors.Is(err, ErrPathNotFoundAtRevision) {
		t.Fatalf("ReadFileAtCommit error = %v", err)
	}
}

func TestReadFileAtCommitFollowsSymlinkWithinRevision(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	guidancePath := filepath.Join(repositoryRoot, "guidance.md")
	linkPath := filepath.Join(repositoryRoot, "review-guidance.md")
	if err := os.WriteFile(guidancePath, []byte("trusted guidance"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("guidance.md", linkPath); err != nil {
		t.Fatal(err)
	}
	commitRevisionFiles(t, repositoryRoot)

	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	content, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "review-guidance.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "trusted guidance" {
		t.Fatalf("ReadFileAtCommit() = %q", content)
	}
}

func TestReadFileAtCommitRejectsEscapingAndCyclicSymlinks(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	if err := os.Symlink("../outside.md", filepath.Join(repositoryRoot, "escaping.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cycle-b.md", filepath.Join(repositoryRoot, "cycle-a.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cycle-a.md", filepath.Join(repositoryRoot, "cycle-b.md")); err != nil {
		t.Fatal(err)
	}
	commitRevisionFiles(t, repositoryRoot)

	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "escaping.md"); err == nil || !strings.Contains(err.Error(), "escapes the repository") {
		t.Fatalf("escaping symlink error = %v", err)
	}
	if _, err := ReadFileAtCommit(ctx, repositoryRoot, commit, "cycle-a.md"); err == nil || !strings.Contains(err.Error(), "symlink cycle") {
		t.Fatalf("cyclic symlink error = %v", err)
	}
}

func TestReadFileAtCommitRejectsCrossPlatformAbsolutePaths(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	commit, err := ResolveCommit(ctx, repositoryRoot, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, repositoryPath := range []string{"/tmp/guidance.md", `C:\guidance.md`, `d:/guidance.md`} {
		if _, err := ReadFileAtCommit(ctx, repositoryRoot, commit, repositoryPath); err == nil || !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("ReadFileAtCommit(%q) error = %v", repositoryPath, err)
		}
	}
}

func commitRevisionFiles(t *testing.T, repositoryRoot string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "revision files"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repositoryRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

func TestRemoteDefaultBranch(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := setupTestRepo(t)
	bareRoot := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.Command("git", "clone", "--bare", repositoryRoot, bareRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare failed: %v\n%s", err, out)
	}

	branch, err := RemoteDefaultBranch(ctx, repositoryRoot, bareRoot)
	if err != nil {
		t.Fatal(err)
	}
	if branch == "" {
		t.Fatal("RemoteDefaultBranch returned an empty branch")
	}
}
