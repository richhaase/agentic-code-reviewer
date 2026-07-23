package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDir_UsesEnvOverride(t *testing.T) {
	t.Setenv(ConfigDirEnvVar, "/custom/config/dir")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/custom/config/dir" {
		t.Fatalf("expected env override, got %q", dir)
	}
}

func TestConfigDir_DefaultsUnderOSConfigDir(t *testing.T) {
	t.Setenv(ConfigDirEnvVar, "")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(dir) != "acr" {
		t.Fatalf("expected default config dir to end in acr, got %q", dir)
	}
}

func TestInit_CreatesFileWithSafeDefaults(t *testing.T) {
	dir := t.TempDir()

	path, err := Init(dir, "octocat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != filepath.Join(dir, ConfigFileName) {
		t.Fatalf("unexpected path: %q", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read created file: %v", err)
	}
	text := string(content)

	for _, want := range []string{
		"schema_version: 1",
		`expected_user: "octocat"`,
		"posting:\n  enabled: false",
		"own_pr_policy: disabled",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected starter config to contain %q, got:\n%s", want, text)
		}
	}
}

func TestInit_FailsIfExists(t *testing.T) {
	dir := t.TempDir()

	if _, err := Init(dir, "octocat"); err != nil {
		t.Fatalf("unexpected error on first init: %v", err)
	}
	if _, err := Init(dir, "octocat"); err == nil {
		t.Fatal("expected error when workspace.yaml already exists")
	}
}

func TestLoad_MissingFileMentionsInit(t *testing.T) {
	dir := t.TempDir()

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing workspace configuration")
	}
	if !strings.Contains(err.Error(), "acr desk config init") {
		t.Errorf("expected error to reference init command, got: %v", err)
	}
}

func TestLoad_InvalidYAMLIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte("not: [valid"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestInitThenLoad_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "octocat"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Identity.ExpectedUser != "octocat" {
		t.Errorf("expected identity.expected_user to round-trip, got %q", cfg.Identity.ExpectedUser)
	}
	if cfg.Posting.Enabled {
		t.Error("expected posting to be disabled by default")
	}
	if cfg.Behavior.OwnPRPolicy != OwnPRPolicyDisabled {
		t.Errorf("expected own_pr_policy to default to disabled, got %q", cfg.Behavior.OwnPRPolicy)
	}
	if len(cfg.Validate()) != 0 {
		t.Errorf("expected freshly initialized config to be valid, got problems: %v", cfg.Validate())
	}
}
