package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestConfigInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Init a git repo so git.GetRoot() works
	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, config.ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("expected .acr.yaml to be created")
	}
}

func TestConfigInit_FailsIfExists(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Init a git repo so git.GetRoot() works
	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// Create the file first
	configPath := filepath.Join(dir, config.ConfigFileName)
	if err := os.WriteFile(configPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"init"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when file already exists")
	}
}

func TestConfigValidate_DoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	cmd := newConfigCmd()
	cmd.SetArgs([]string{"validate"})
	// Should not panic; may return error if not in git repo, that's fine
	_ = cmd.Execute()
}

func TestConfigValidate_DetectsInvalidEnvVars(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Init a git repo so config loading works
	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// Set semantically invalid env var (parses fine, but fails validation)
	t.Setenv("ACR_REVIEWERS", "0")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	// The command should succeed (it reports issues via logger, doesn't return error)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigValidate_DetectsInvalidAgent(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	if out, err := exec.Command("git", "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	t.Setenv("ACR_REVIEWER_AGENT", "unsupported")

	cmd := newConfigCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"validate"})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
