package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func TestDeskConfigInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)

	cmd := newDeskConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, workspace.ConfigFileName)); err != nil {
		t.Fatalf("expected workspace.yaml to be created: %v", err)
	}
}

func TestDeskConfigInit_FailsIfExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)

	if _, err := workspace.Init(dir, "octocat"); err != nil {
		t.Fatal(err)
	}

	cmd := newDeskConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when workspace.yaml already exists")
	}
}

func TestDeskConfigShow_MissingConfigIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)

	cmd := newDeskConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when workspace configuration does not exist")
	}
}

func TestDeskConfigShow_PrintsResolvedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)
	if _, err := workspace.Init(dir, "octocat"); err != nil {
		t.Fatal(err)
	}

	cmd := newDeskConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"show"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeskConfigValidate_DetectsInvalidOwnPRPolicy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)
	if err := os.WriteFile(filepath.Join(dir, workspace.ConfigFileName), []byte(
		"schema_version: 1\nidentity:\n  expected_user: octocat\nbehavior:\n  own_pr_policy: approve\n"),
		0644); err != nil {
		t.Fatal(err)
	}

	cmd := newDeskConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid own_pr_policy")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Errorf("expected error message to mention errors, got: %v", err)
	}
}

func TestDeskConfigValidate_DetectsAmbiguousRepositoryClones(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, configDir)

	reposRoot := t.TempDir()
	for _, name := range []string{"widgets-a", "widgets-b"} {
		dir := filepath.Join(reposRoot, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init failed: %v: %s", err, out)
		}
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
			t.Fatalf("git remote add failed: %v: %s", err, out)
		}
	}

	configContent := "schema_version: 1\nidentity:\n  expected_user: octocat\nbehavior:\n  own_pr_policy: disabled\nscope:\n  repository_roots:\n    - " + reposRoot + "\n"
	if err := os.WriteFile(filepath.Join(configDir, workspace.ConfigFileName), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newDeskConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for ambiguous repository clones")
	}
}

func TestDeskConfigValidate_MissingConfigIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(workspace.ConfigDirEnvVar, dir)

	cmd := newDeskConfigCmd()
	cmd.SetArgs([]string{"validate"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when workspace configuration does not exist")
	}
}
