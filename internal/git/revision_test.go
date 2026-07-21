package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
