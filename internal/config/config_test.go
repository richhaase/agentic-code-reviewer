package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoadFromDirWithWarnings_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns:
    - "test pattern"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromDirWithWarnings(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Config.Filters.ExcludePatterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(result.Config.Filters.ExcludePatterns))
	}
	if result.Config.Filters.ExcludePatterns[0] != "test pattern" {
		t.Errorf("expected 'test pattern', got %q", result.Config.Filters.ExcludePatterns[0])
	}
}

func TestLoadFromDirWithWarnings_NoConfig(t *testing.T) {
	dir := t.TempDir()

	result, err := LoadFromDirWithWarnings(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_FileNotFound(t *testing.T) {
	result, err := LoadFromPathWithWarnings("/nonexistent/path/.acr.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if result.Config == nil {
		t.Fatal("expected non-nil config")
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for missing file, got: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_ValidYAML(t *testing.T) {
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

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Next\\.js forbids", "deprecated API", "consider using"}
	if len(result.Config.Filters.ExcludePatterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d", len(expected), len(result.Config.Filters.ExcludePatterns))
	}
	for i, pattern := range expected {
		if result.Config.Filters.ExcludePatterns[i] != pattern {
			t.Errorf("pattern %d: expected %q, got %q", i, pattern, result.Config.Filters.ExcludePatterns[i])
		}
	}
}

func TestLoadFromPathWithWarnings_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_InvalidYAML(t *testing.T) {
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

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFromPathWithWarnings_InvalidRegex(t *testing.T) {
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

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestLoadFromPathWithWarnings_EmptyPatterns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_patterns: []
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Config.Filters.ExcludePatterns) != 0 {
		t.Errorf("expected empty patterns, got: %v", result.Config.Filters.ExcludePatterns)
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

// Tests for expanded config schema

func TestLoadFromPathWithWarnings_FullConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 10
concurrency: 5
base: develop
timeout: 10m
retries: 3
filters:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.Reviewers == nil || *cfg.Reviewers != 10 {
		t.Errorf("expected reviewers=10, got %v", cfg.Reviewers)
	}
	if cfg.Concurrency == nil || *cfg.Concurrency != 5 {
		t.Errorf("expected concurrency=5, got %v", cfg.Concurrency)
	}
	if cfg.Base == nil || *cfg.Base != "develop" {
		t.Errorf("expected base=develop, got %v", cfg.Base)
	}
	if cfg.Timeout == nil || cfg.Timeout.AsDuration() != 10*time.Minute {
		t.Errorf("expected timeout=10m, got %v", cfg.Timeout)
	}
	if cfg.Retries == nil || *cfg.Retries != 3 {
		t.Errorf("expected retries=3, got %v", cfg.Retries)
	}
	if len(cfg.Filters.ExcludePatterns) != 1 || cfg.Filters.ExcludePatterns[0] != "test" {
		t.Errorf("expected exclude_patterns=[test], got %v", cfg.Filters.ExcludePatterns)
	}
}

func TestLoadFromPathWithWarnings_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 3
base: feature-branch
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.Reviewers == nil || *cfg.Reviewers != 3 {
		t.Errorf("expected reviewers=3, got %v", cfg.Reviewers)
	}
	if cfg.Concurrency != nil {
		t.Errorf("expected concurrency=nil, got %v", cfg.Concurrency)
	}
	if cfg.Base == nil || *cfg.Base != "feature-branch" {
		t.Errorf("expected base=feature-branch, got %v", cfg.Base)
	}
	if cfg.Timeout != nil {
		t.Errorf("expected timeout=nil, got %v", cfg.Timeout)
	}
	if cfg.Retries != nil {
		t.Errorf("expected retries=nil, got %v", cfg.Retries)
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected time.Duration
		wantErr  bool
	}{
		{"duration string 5m", "timeout: 5m", 5 * time.Minute, false},
		{"duration string 300s", "timeout: 300s", 5 * time.Minute, false},
		{"duration string 1h30m", "timeout: 1h30m", 90 * time.Minute, false},
		{"integer seconds", "timeout: 300", 5 * time.Minute, false},
		{"invalid string", "timeout: invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg struct {
				Timeout *Duration `yaml:"timeout"`
			}
			err := yaml.Unmarshal([]byte(tt.yaml), &cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Timeout == nil {
				t.Fatal("expected timeout to be set")
			}
			if cfg.Timeout.AsDuration() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, cfg.Timeout.AsDuration())
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid config", Config{Reviewers: ptr(5), Retries: ptr(2)}, false},
		{"reviewers zero", Config{Reviewers: ptr(0)}, true},
		{"reviewers negative", Config{Reviewers: ptr(-1)}, true},
		{"concurrency negative", Config{Concurrency: ptr(-1)}, true},
		{"concurrency zero valid", Config{Concurrency: ptr(0)}, false},
		{"retries negative", Config{Retries: ptr(-1)}, true},
		{"retries zero valid", Config{Retries: ptr(0)}, false},
		{"timeout negative", Config{Timeout: durationPtr(-time.Second)}, true},
		{"timeout zero", Config{Timeout: durationPtr(0)}, true},
		{"all nil valid", Config{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_FlagOverridesAll(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{Reviewers: 5, ReviewersSet: true}
	flagState := FlagState{ReviewersSet: true}
	flagValues := ResolvedConfig{Reviewers: 10}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 10 {
		t.Errorf("expected flag value 10, got %d", result.Reviewers)
	}
}

func TestResolve_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{Reviewers: 5, ReviewersSet: true}
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 5 {
		t.Errorf("expected env value 5, got %d", result.Reviewers)
	}
}

func TestResolve_ConfigOverridesDefault(t *testing.T) {
	cfg := &Config{Reviewers: ptr(3)}
	envState := EnvState{}   // no env vars set
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 3 {
		t.Errorf("expected config value 3, got %d", result.Reviewers)
	}
}

func TestResolve_DefaultsUsedWhenNothingSet(t *testing.T) {
	cfg := &Config{} // empty config
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != Defaults.Reviewers {
		t.Errorf("expected default reviewers %d, got %d", Defaults.Reviewers, result.Reviewers)
	}
	if result.Base != Defaults.Base {
		t.Errorf("expected default base %q, got %q", Defaults.Base, result.Base)
	}
	if result.Timeout != Defaults.Timeout {
		t.Errorf("expected default timeout %v, got %v", Defaults.Timeout, result.Timeout)
	}
	if result.Retries != Defaults.Retries {
		t.Errorf("expected default retries %d, got %d", Defaults.Retries, result.Retries)
	}
}

func TestResolve_NilConfig(t *testing.T) {
	result := Resolve(nil, EnvState{}, FlagState{}, ResolvedConfig{})

	if result.Reviewers != Defaults.Reviewers {
		t.Errorf("expected default reviewers %d, got %d", Defaults.Reviewers, result.Reviewers)
	}
}

func TestResolve_MixedSources(t *testing.T) {
	// reviewers from config, base from env, timeout from flag
	cfg := &Config{
		Reviewers: ptr(3),
		Base:      strPtr("config-base"),
		Timeout:   durationPtr(1 * time.Minute),
	}
	envState := EnvState{
		Base:    "env-base",
		BaseSet: true,
	}
	flagState := FlagState{
		TimeoutSet: true,
	}
	flagValues := ResolvedConfig{
		Timeout: 10 * time.Minute,
	}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Reviewers != 3 {
		t.Errorf("expected config reviewers 3, got %d", result.Reviewers)
	}
	if result.Base != "env-base" {
		t.Errorf("expected env base 'env-base', got %q", result.Base)
	}
	if result.Timeout != 10*time.Minute {
		t.Errorf("expected flag timeout 10m, got %v", result.Timeout)
	}
}

func TestLoadFromPathWithWarnings_InvalidReviewers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 0
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for reviewers=0")
	}
}

func TestLoadFromPathWithWarnings_InvalidTimeout(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `timeout: -5m
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestLoadFromPathWithWarnings_PreservesWarningsOnValidationError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	// Config with both an unknown key (produces warning) and invalid value (produces error)
	content := `reviewers: 0
unknown_field: true
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err == nil {
		t.Fatal("expected error for reviewers=0")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on validation error")
	}
	if result.Config == nil {
		t.Fatal("expected non-nil Config in result")
	}
	if result.Config.Reviewers == nil || *result.Config.Reviewers != 0 {
		t.Error("expected parsed Config to contain reviewers=0")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected unknown-key warning to be preserved alongside validation error")
	}
}

// Tests for unknown key warnings

func TestLoadFromPathWithWarnings_UnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 5
unknownkey: value
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	if result.Warnings[0] != `unknown key "unknownkey" in .acr.yaml` {
		t.Errorf("unexpected warning: %s", result.Warnings[0])
	}
	// Config should still be parsed
	if result.Config.Reviewers == nil || *result.Config.Reviewers != 5 {
		t.Errorf("expected reviewers=5, got %v", result.Config.Reviewers)
	}
}

func TestLoadFromPathWithWarnings_UnknownKeyWithSuggestion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filtrs:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	expected := `unknown key "filtrs" in .acr.yaml (did you mean "filters"?)`
	if result.Warnings[0] != expected {
		t.Errorf("expected warning %q, got %q", expected, result.Warnings[0])
	}
}

func TestLoadFromPathWithWarnings_UnknownFilterKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `filters:
  exclude_paterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	expected := `unknown key "exclude_paterns" in filters section of .acr.yaml (did you mean "exclude_patterns"?)`
	if result.Warnings[0] != expected {
		t.Errorf("expected warning %q, got %q", expected, result.Warnings[0])
	}
}

func TestLoadFromPathWithWarnings_MultipleUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewrs: 5
tiemout: 10m
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoWarningsForValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewers: 5
concurrency: 3
base: main
timeout: 5m
retries: 2
guidance_file: guidance.md
filters:
  exclude_patterns:
    - "test"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoWarningsForEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(result.Warnings), result.Warnings)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"filters", "filtrs", 1},
		{"exclude_patterns", "exclude_paterns", 1},
		{"reviewers", "reviewrs", 1},
		{"timeout", "tiemout", 2},
		{"totally_different", "abc", 16},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := levenshtein(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("levenshtein(%q, %q) = %d, expected %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestFindSimilar(t *testing.T) {
	candidates := []string{"reviewers", "concurrency", "base", "timeout", "retries", "filters"}

	tests := []struct {
		input    string
		expected string
	}{
		{"reviewrs", "reviewers"},
		{"filtrs", "filters"},
		{"tiemout", "timeout"},
		{"totally_unrelated_name", ""},
		{"reviewers", "reviewers"}, // exact match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := findSimilar(tt.input, candidates)
			if got != tt.expected {
				t.Errorf("findSimilar(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Helper functions
func ptr(i int) *int { return &i }

func strPtr(s string) *string { return &s }

func durationPtr(d time.Duration) *Duration {
	dur := Duration(d)
	return &dur
}

func TestResolveGuidance(t *testing.T) {
	// Create temp files for guidance file tests
	dir := t.TempDir()
	flagGuidanceFile := filepath.Join(dir, "flag_guidance.txt")
	envGuidanceFile := filepath.Join(dir, "env_guidance.txt")
	configGuidanceFile := filepath.Join(dir, "config_guidance.txt")

	if err := os.WriteFile(flagGuidanceFile, []byte("guidance from flag file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envGuidanceFile, []byte("guidance from env file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configGuidanceFile, []byte("guidance from config file"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		cfg        *Config
		envState   EnvState
		flagState  FlagState
		flagValues ResolvedConfig
		want       string
		wantErr    bool
	}{
		{
			name: "flag guidance text wins over all",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			flagState: FlagState{
				GuidanceSet:     true,
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				Guidance:     "flag guidance",
				GuidanceFile: flagGuidanceFile,
			},
			want: "flag guidance",
		},
		{
			name: "flag guidance-file wins over env/config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			flagState: FlagState{
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				GuidanceFile: flagGuidanceFile,
			},
			want: "guidance from flag file",
		},
		{
			name: "env ACR_GUIDANCE wins over config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "env guidance",
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			want: "env guidance",
		},
		{
			name: "env ACR_GUIDANCE_FILE wins over config",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			envState: EnvState{
				GuidanceFileSet: true,
				GuidanceFile:    envGuidanceFile,
			},
			want: "guidance from env file",
		},
		{
			name: "config guidance_file works",
			cfg: &Config{
				GuidanceFile: strPtr(configGuidanceFile),
			},
			want: "guidance from config file",
		},
		{
			name: "nothing set returns empty",
			want: "",
		},
		{
			name: "empty strings result in empty guidance",
			cfg: &Config{
				GuidanceFile: strPtr(""),
			},
			envState: EnvState{
				GuidanceSet:     true,
				Guidance:        "",
				GuidanceFileSet: true,
				GuidanceFile:    "",
			},
			flagState: FlagState{
				GuidanceSet:     true,
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				Guidance:     "",
				GuidanceFile: "",
			},
			want: "",
		},
		{
			name: "error reading flag guidance file",
			flagState: FlagState{
				GuidanceFileSet: true,
			},
			flagValues: ResolvedConfig{
				GuidanceFile: "/nonexistent/guidance.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading env guidance file",
			envState: EnvState{
				GuidanceFileSet: true,
				GuidanceFile:    "/nonexistent/guidance.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading config guidance file",
			cfg: &Config{
				GuidanceFile: strPtr("/nonexistent/guidance.txt"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveGuidance(tt.cfg, tt.envState, tt.flagState, tt.flagValues, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveGuidance() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got != tt.want {
					t.Errorf("ResolveGuidance() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// Tests for agent config

func TestLoadFromPathWithWarnings_ReviewerAgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
reviewers: 5
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := result.Config

	if cfg.ReviewerAgent == nil || *cfg.ReviewerAgent != "claude" {
		t.Errorf("expected reviewer_agent=claude, got %v", cfg.ReviewerAgent)
	}
}

func TestValidate_ReviewerAgent(t *testing.T) {
	tests := []struct {
		name    string
		agent   string
		wantErr bool
	}{
		{"valid codex", "codex", false},
		{"valid claude", "claude", false},
		{"valid gemini", "gemini", false},
		{"invalid agent", "invalid", true},
		{"empty agent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ReviewerAgent: strPtr(tt.agent)}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_ReviewerAgents_FlagOverridesAll(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{ReviewerAgents: []string{"claude"}, ReviewerAgentsSet: true}
	flagState := FlagState{ReviewerAgentsSet: true}
	flagValues := ResolvedConfig{ReviewerAgents: []string{"codex"}}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "codex" {
		t.Errorf("expected flag value ['codex'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{ReviewerAgents: []string{"claude"}, ReviewerAgentsSet: true}
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "claude" {
		t.Errorf("expected env value ['claude'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_ConfigOverridesDefault(t *testing.T) {
	cfg := &Config{ReviewerAgent: strPtr("gemini")}
	envState := EnvState{}   // no env vars set
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "gemini" {
		t.Errorf("expected config value ['gemini'], got %v", result.ReviewerAgents)
	}
}

func TestResolve_ReviewerAgents_DefaultsToCodex(t *testing.T) {
	cfg := &Config{} // empty config
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if len(result.ReviewerAgents) != 1 || result.ReviewerAgents[0] != "codex" {
		t.Errorf("expected default reviewer_agents ['codex'], got %v", result.ReviewerAgents)
	}
}

func TestLoadEnvState_ReviewerAgents(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_REVIEWER_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_REVIEWER_AGENT", original)
		} else {
			os.Unsetenv("ACR_REVIEWER_AGENT")
		}
	}()

	os.Setenv("ACR_REVIEWER_AGENT", "claude")
	state, warnings := LoadEnvState()

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !state.ReviewerAgentsSet {
		t.Error("expected ReviewerAgentsSet to be true")
	}
	if len(state.ReviewerAgents) != 1 || state.ReviewerAgents[0] != "claude" {
		t.Errorf("expected reviewer_agents=['claude'], got %v", state.ReviewerAgents)
	}
}

func TestLoadEnvState_ReviewerAgents_NotSet(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_REVIEWER_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_REVIEWER_AGENT", original)
		} else {
			os.Unsetenv("ACR_REVIEWER_AGENT")
		}
	}()

	os.Unsetenv("ACR_REVIEWER_AGENT")
	state, warnings := LoadEnvState()

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if state.ReviewerAgentsSet {
		t.Error("expected ReviewerAgentsSet to be false")
	}
	if len(state.ReviewerAgents) != 0 {
		t.Errorf("expected empty reviewer_agents, got %v", state.ReviewerAgents)
	}
}

func TestResolveGuidance_Precedence(t *testing.T) {
	// Test that verifies the exact precedence order
	dir := t.TempDir()
	guidanceFile := filepath.Join(dir, "guidance.txt")
	if err := os.WriteFile(guidanceFile, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	// All sources set, flag guidance text should win
	cfg := &Config{
		GuidanceFile: strPtr(guidanceFile),
	}
	envState := EnvState{
		GuidanceSet:     true,
		Guidance:        "env guidance",
		GuidanceFileSet: true,
		GuidanceFile:    guidanceFile,
	}
	flagState := FlagState{
		GuidanceSet:     true,
		GuidanceFileSet: false,
	}
	flagValues := ResolvedConfig{
		Guidance:     "flag guidance",
		GuidanceFile: "",
	}

	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "flag guidance" {
		t.Errorf("expected 'flag guidance', got %q", got)
	}
}

func TestResolveGuidance_ConfigFileRelativePath(t *testing.T) {
	// Create a temp directory structure:
	// tempdir/
	//   guidance/
	//     review.md
	dir := t.TempDir()
	guidanceDir := filepath.Join(dir, "guidance")
	if err := os.MkdirAll(guidanceDir, 0755); err != nil {
		t.Fatalf("failed to create guidance dir: %v", err)
	}
	guidanceFile := filepath.Join(guidanceDir, "review.md")
	guidanceContent := "custom review guidance from file"
	if err := os.WriteFile(guidanceFile, []byte(guidanceContent), 0644); err != nil {
		t.Fatalf("failed to write guidance file: %v", err)
	}

	// Config with relative path
	relativePath := "guidance/review.md"
	cfg := &Config{
		GuidanceFile: &relativePath,
	}
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	// Resolve with configDir set to temp directory
	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != guidanceContent {
		t.Errorf("ResolveGuidance() = %q, want %q", got, guidanceContent)
	}
}

func TestResolveGuidance_ConfigFileAbsolutePath(t *testing.T) {
	// Create a temp file with guidance content
	dir := t.TempDir()
	guidanceFile := filepath.Join(dir, "guidance.md")
	guidanceContent := "absolute path guidance"
	if err := os.WriteFile(guidanceFile, []byte(guidanceContent), 0644); err != nil {
		t.Fatalf("failed to write guidance file: %v", err)
	}

	// Config with absolute path - should work regardless of configDir
	cfg := &Config{
		GuidanceFile: &guidanceFile,
	}
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	// Resolve with a different configDir - absolute path should still work
	got, err := ResolveGuidance(cfg, envState, flagState, flagValues, "/some/other/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != guidanceContent {
		t.Errorf("ResolveGuidance() = %q, want %q", got, guidanceContent)
	}
}

// Tests for malformed environment variable warnings

// clearACREnv unsets all ACR_* env vars to isolate tests from ambient environment.
// Uses t.Setenv("VAR", "") then os.Unsetenv to get automatic restore on test cleanup.
func clearACREnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ACR_REVIEWERS", "ACR_CONCURRENCY", "ACR_BASE_REF", "ACR_TIMEOUT",
		"ACR_RETRIES", "ACR_FETCH", "ACR_REVIEWER_AGENT", "ACR_SUMMARIZER_AGENT",
		"ACR_GUIDANCE", "ACR_GUIDANCE_FILE", "ACR_FP_FILTER", "ACR_FP_THRESHOLD",
		"ACR_PR_FEEDBACK", "ACR_PR_FEEDBACK_AGENT",
	} {
		t.Setenv(key, os.Getenv(key)) // register for restore
		os.Unsetenv(key)
	}
}

// hasWarningContaining checks if any warning contains the given substring.
func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestLoadEnvState_MalformedReviewers(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_REVIEWERS", "abc")
	state, warnings := LoadEnvState()
	if state.ReviewersSet {
		t.Error("expected ReviewersSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_REVIEWERS") {
		t.Errorf("expected warning about ACR_REVIEWERS, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedConcurrency(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_CONCURRENCY", "xyz")
	state, warnings := LoadEnvState()
	if state.ConcurrencySet {
		t.Error("expected ConcurrencySet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_CONCURRENCY") {
		t.Errorf("expected warning about ACR_CONCURRENCY, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedTimeout(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_TIMEOUT", "notaduration")
	state, warnings := LoadEnvState()
	if state.TimeoutSet {
		t.Error("expected TimeoutSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_TIMEOUT") {
		t.Errorf("expected warning about ACR_TIMEOUT, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedRetries(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_RETRIES", "nope")
	state, warnings := LoadEnvState()
	if state.RetriesSet {
		t.Error("expected RetriesSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_RETRIES") {
		t.Errorf("expected warning about ACR_RETRIES, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFetch(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FETCH", "maybe")
	state, warnings := LoadEnvState()
	if state.FetchSet {
		t.Error("expected FetchSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FETCH") {
		t.Errorf("expected warning about ACR_FETCH, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPFilter(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_FILTER", "maybe")
	state, warnings := LoadEnvState()
	if state.FPFilterSet {
		t.Error("expected FPFilterSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FP_FILTER") {
		t.Errorf("expected warning about ACR_FP_FILTER, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedPRFeedback(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_PR_FEEDBACK", "maybe")
	state, warnings := LoadEnvState()
	if state.PRFeedbackEnabledSet {
		t.Error("expected PRFeedbackEnabledSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_PR_FEEDBACK") {
		t.Errorf("expected warning about ACR_PR_FEEDBACK, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPThreshold_NotInt(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_THRESHOLD", "abc")
	state, warnings := LoadEnvState()
	if state.FPThresholdSet {
		t.Error("expected FPThresholdSet to be false for invalid value")
	}
	if !hasWarningContaining(warnings, "ACR_FP_THRESHOLD") {
		t.Errorf("expected warning about ACR_FP_THRESHOLD, got %v", warnings)
	}
}

func TestLoadEnvState_MalformedFPThreshold_OutOfRange(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_FP_THRESHOLD", "200")
	state, warnings := LoadEnvState()
	if state.FPThresholdSet {
		t.Error("expected FPThresholdSet to be false for out-of-range value")
	}
	if !hasWarningContaining(warnings, "out of range") {
		t.Errorf("expected out-of-range warning, got %v", warnings)
	}
}

func TestLoadEnvState_NoWarningsForValidValues(t *testing.T) {
	clearACREnv(t)
	t.Setenv("ACR_REVIEWERS", "5")
	t.Setenv("ACR_TIMEOUT", "10m")
	t.Setenv("ACR_FETCH", "true")
	_, warnings := LoadEnvState()
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for valid values, got %v", warnings)
	}
}

// Tests for deprecated reviewer_agent config key

func TestLoadFromPathWithWarnings_DeprecatedReviewerAgent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected deprecation warning, got warnings: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_DeprecatedReviewerAgentWithReviewerAgents(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agent: claude
reviewer_agents:
  - codex
  - gemini
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasDeprecated := false
	hasPrecedence := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			hasDeprecated = true
		}
		if strings.Contains(w, "takes precedence") {
			hasPrecedence = true
		}
	}
	if !hasDeprecated {
		t.Errorf("expected deprecation warning, got: %v", result.Warnings)
	}
	if !hasPrecedence {
		t.Errorf("expected precedence warning, got: %v", result.Warnings)
	}
}

func TestLoadFromPathWithWarnings_NoDeprecationWithoutReviewerAgent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `reviewer_agents:
  - codex
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "deprecated") {
			t.Errorf("unexpected deprecation warning: %s", w)
		}
	}
}

func TestResolvedConfig_Validate_Valid(t *testing.T) {
	cfg := Defaults
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected defaults to be valid, got: %v", err)
	}
}

func TestResolvedConfig_Validate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*ResolvedConfig)
		wantMsg string
	}{
		{
			name:    "reviewers too low",
			modify:  func(c *ResolvedConfig) { c.Reviewers = 0 },
			wantMsg: "reviewers must be >= 1",
		},
		{
			name:    "negative concurrency",
			modify:  func(c *ResolvedConfig) { c.Concurrency = -1 },
			wantMsg: "concurrency must be >= 0",
		},
		{
			name:    "negative retries",
			modify:  func(c *ResolvedConfig) { c.Retries = -1 },
			wantMsg: "retries must be >= 0",
		},
		{
			name:    "zero timeout",
			modify:  func(c *ResolvedConfig) { c.Timeout = 0 },
			wantMsg: "timeout must be > 0",
		},
		{
			name:    "empty reviewer agents",
			modify:  func(c *ResolvedConfig) { c.ReviewerAgents = []string{} },
			wantMsg: "reviewer_agents must not be empty",
		},
		{
			name:    "invalid reviewer agent",
			modify:  func(c *ResolvedConfig) { c.ReviewerAgents = []string{"unsupported"} },
			wantMsg: "unsupported agent",
		},
		{
			name:    "invalid summarizer agent",
			modify:  func(c *ResolvedConfig) { c.SummarizerAgent = "unsupported" },
			wantMsg: "summarizer_agent must be one of",
		},
		{
			name:    "fp threshold too low",
			modify:  func(c *ResolvedConfig) { c.FPThreshold = 0 },
			wantMsg: "fp_filter.threshold must be 1-100",
		},
		{
			name:    "fp threshold too high",
			modify:  func(c *ResolvedConfig) { c.FPThreshold = 101 },
			wantMsg: "fp_filter.threshold must be 1-100",
		},
		{
			name:    "invalid pr feedback agent",
			modify:  func(c *ResolvedConfig) { c.PRFeedbackAgent = "bad" },
			wantMsg: "pr_feedback.agent must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults
			tt.modify(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.wantMsg, err)
			}
		})
	}
}

func TestResolvedConfig_Validate_MultipleErrors(t *testing.T) {
	cfg := Defaults
	cfg.Reviewers = 0
	cfg.Retries = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "reviewers") || !strings.Contains(msg, "retries") {
		t.Errorf("expected both reviewers and retries errors, got: %v", err)
	}
}

func TestResolvedConfig_Validate_EmptyPRFeedbackAgent(t *testing.T) {
	cfg := Defaults
	cfg.PRFeedbackAgent = "" // empty means use summarizer agent, should be valid
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected empty pr_feedback.agent to be valid, got: %v", err)
	}
}

func TestResolvedConfig_Validate_EmptyReviewerAgents(t *testing.T) {
	cfg := Defaults
	cfg.ReviewerAgents = []string{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty reviewer_agents, got nil")
	}
	if !strings.Contains(err.Error(), "reviewer_agents must not be empty") {
		t.Errorf("expected 'reviewer_agents must not be empty' in error, got: %v", err)
	}
}
