package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TestConfigInit_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

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
