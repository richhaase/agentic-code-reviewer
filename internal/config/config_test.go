package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDir_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "test pattern"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Filters.ExcludePatterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(cfg.Filters.ExcludePatterns))
	}
	if cfg.Filters.ExcludePatterns[0] != "test pattern" {
		t.Errorf("expected 'test pattern', got %q", cfg.Filters.ExcludePatterns[0])
	}
}

func TestLoadFromDir_NoConfig(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(cfg.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", cfg.Filters.ExcludePatterns)
	}
}

func TestLoadFromPath_FileNotFound(t *testing.T) {
	cfg, err := LoadFromPath("/nonexistent/path/.acr.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", cfg.Filters.ExcludePatterns)
	}
}

func TestLoadFromPath_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "Next\\.js forbids"
    - "deprecated API"
    - "consider using"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Next\\.js forbids", "deprecated API", "consider using"}
	if len(cfg.Filters.ExcludePatterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d", len(expected), len(cfg.Filters.ExcludePatterns))
	}
	for i, pattern := range expected {
		if cfg.Filters.ExcludePatterns[i] != pattern {
			t.Errorf("pattern %d: expected %q, got %q", i, pattern, cfg.Filters.ExcludePatterns[i])
		}
	}
}

func TestLoadFromPath_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", cfg.Filters.ExcludePatterns)
	}
}

func TestLoadFromPath_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "valid"
    invalid yaml here
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPath(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFromPath_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "valid pattern"
    - "[invalid regex"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPath(configPath)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestLoadFromPath_EmptyPatterns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns: []
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", cfg.Filters.ExcludePatterns)
	}
}

func TestMerge_NilConfig(t *testing.T) {
	cliPatterns := []string{"cli-pattern"}
	result := Merge(nil, cliPatterns)

	if len(result) != 1 || result[0] != "cli-pattern" {
		t.Errorf("expected cli patterns only, got: %v", result)
	}
}

func TestMerge_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	cliPatterns := []string{"cli-pattern"}
	result := Merge(cfg, cliPatterns)

	if len(result) != 1 || result[0] != "cli-pattern" {
		t.Errorf("expected cli patterns only, got: %v", result)
	}
}

func TestMerge_ConfigOnly(t *testing.T) {
	cfg := &Config{
		Filters: FilterConfig{
			ExcludePatterns: []string{"config-pattern-1", "config-pattern-2"},
		},
	}
	result := Merge(cfg, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(result))
	}
	if result[0] != "config-pattern-1" || result[1] != "config-pattern-2" {
		t.Errorf("unexpected patterns: %v", result)
	}
}

func TestMerge_BothConfigAndCLI(t *testing.T) {
	cfg := &Config{
		Filters: FilterConfig{
			ExcludePatterns: []string{"config-pattern"},
		},
	}
	cliPatterns := []string{"cli-pattern"}
	result := Merge(cfg, cliPatterns)

	if len(result) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(result))
	}
	// Config patterns come first, then CLI patterns
	if result[0] != "config-pattern" {
		t.Errorf("expected config pattern first, got: %s", result[0])
	}
	if result[1] != "cli-pattern" {
		t.Errorf("expected cli pattern second, got: %s", result[1])
	}
}

func TestMerge_BothEmpty(t *testing.T) {
	cfg := &Config{}
	result := Merge(cfg, nil)

	if len(result) != 0 {
		t.Errorf("expected empty result, got: %v", result)
	}
}
