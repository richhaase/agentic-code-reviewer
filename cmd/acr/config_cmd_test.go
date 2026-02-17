package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

// chdir changes to dir and returns a cleanup function to restore the original directory.
func chdir(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origDir); err != nil {
			t.Errorf("failed to restore working directory to %s: %v", origDir, err)
		}
	})
}

// initGitRepo initializes a git repo in dir.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
}

func TestConfigInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, config.ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected .acr.yaml to be created")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .acr.yaml: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "summarizer_timeout: 5m") {
		t.Fatal("expected starter config to include summarizer_timeout")
	}
	if !strings.Contains(text, "fp_filter_timeout: 5m") {
		t.Fatal("expected starter config to include fp_filter_timeout")
	}
}

func TestConfigInit_FailsIfExists(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Create the file first
	configPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when file already exists")
	}
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected valid config to succeed, got: %v", err)
	}
}

func TestConfigValidate_DetectsInvalidEnvVars(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	// Set semantically invalid env var (parses fine, but fails validation)
	t.Setenv("ACR_REVIEWERS", "0")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_REVIEWERS=0, got nil")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Errorf("expected error message to mention errors, got: %v", err)
	}
}

func TestConfigValidate_DetectsInvalidAgent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_REVIEWER_AGENT", "unsupported")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_REVIEWER_AGENT=unsupported, got nil")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Errorf("expected error message to mention errors, got: %v", err)
	}
}

func TestConfigValidate_DetectsNegativeRetries(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_RETRIES", "-1")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid ACR_RETRIES=-1, got nil")
	}
}

func TestConfigValidate_MalformedEnvVarIsError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_REVIEWERS", "not-a-number")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for malformed ACR_REVIEWERS, got nil")
	}
}

func TestConfigValidate_InvalidGuidanceFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	t.Setenv("ACR_GUIDANCE_FILE", "/nonexistent/guidance.md")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for nonexistent guidance file, got nil")
	}
}

func TestConfigValidate_ValidGuidanceFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	initGitRepo(t, dir)

	guidancePath := filepath.Join(dir, "guidance.md")
	if err := os.WriteFile(guidancePath, []byte("review carefully"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACR_GUIDANCE_FILE", guidancePath)

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("expected valid guidance file to pass, got: %v", err)
	}
}
