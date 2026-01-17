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

func TestResolvePrompt(t *testing.T) {
	// Create temp files for prompt file tests
	dir := t.TempDir()
	flagPromptFile := filepath.Join(dir, "flag_prompt.txt")
	envPromptFile := filepath.Join(dir, "env_prompt.txt")
	configPromptFile := filepath.Join(dir, "config_prompt.txt")

	if err := os.WriteFile(flagPromptFile, []byte("prompt from flag file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPromptFile, []byte("prompt from env file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPromptFile, []byte("prompt from config file"), 0644); err != nil {
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
			name: "flag prompt has highest priority",
			cfg: &Config{
				ReviewPrompt:     strPtr("config prompt"),
				ReviewPromptFile: strPtr(configPromptFile),
			},
			envState: EnvState{
				ReviewPromptSet:     true,
				ReviewPrompt:        "env prompt",
				ReviewPromptFileSet: true,
				ReviewPromptFile:    envPromptFile,
			},
			flagState: FlagState{
				ReviewPromptSet:     true,
				ReviewPromptFileSet: true,
			},
			flagValues: ResolvedConfig{
				ReviewPrompt:     "flag prompt",
				ReviewPromptFile: flagPromptFile,
			},
			want: "flag prompt",
		},
		{
			name: "flag file has second priority",
			cfg: &Config{
				ReviewPrompt:     strPtr("config prompt"),
				ReviewPromptFile: strPtr(configPromptFile),
			},
			envState: EnvState{
				ReviewPromptSet:     true,
				ReviewPrompt:        "env prompt",
				ReviewPromptFileSet: true,
				ReviewPromptFile:    envPromptFile,
			},
			flagState: FlagState{
				ReviewPromptFileSet: true,
			},
			flagValues: ResolvedConfig{
				ReviewPromptFile: flagPromptFile,
			},
			want: "prompt from flag file",
		},
		{
			name: "env prompt has third priority",
			cfg: &Config{
				ReviewPrompt:     strPtr("config prompt"),
				ReviewPromptFile: strPtr(configPromptFile),
			},
			envState: EnvState{
				ReviewPromptSet:     true,
				ReviewPrompt:        "env prompt",
				ReviewPromptFileSet: true,
				ReviewPromptFile:    envPromptFile,
			},
			want: "env prompt",
		},
		{
			name: "env file has fourth priority",
			cfg: &Config{
				ReviewPrompt:     strPtr("config prompt"),
				ReviewPromptFile: strPtr(configPromptFile),
			},
			envState: EnvState{
				ReviewPromptFileSet: true,
				ReviewPromptFile:    envPromptFile,
			},
			want: "prompt from env file",
		},
		{
			name: "config prompt has fifth priority",
			cfg: &Config{
				ReviewPrompt:     strPtr("config prompt"),
				ReviewPromptFile: strPtr(configPromptFile),
			},
			want: "config prompt",
		},
		{
			name: "config file has sixth priority",
			cfg: &Config{
				ReviewPromptFile: strPtr(configPromptFile),
			},
			want: "prompt from config file",
		},
		{
			name: "default prompt when nothing is set",
			want: "You are a code reviewer",
		},
		{
			name: "empty strings are ignored",
			cfg: &Config{
				ReviewPrompt:     strPtr(""),
				ReviewPromptFile: strPtr(""),
			},
			envState: EnvState{
				ReviewPromptSet:     true,
				ReviewPrompt:        "",
				ReviewPromptFileSet: true,
				ReviewPromptFile:    "",
			},
			flagState: FlagState{
				ReviewPromptSet:     true,
				ReviewPromptFileSet: true,
			},
			flagValues: ResolvedConfig{
				ReviewPrompt:     "",
				ReviewPromptFile: "",
			},
			want: "You are a code reviewer",
		},
		{
			name: "error reading flag prompt file",
			flagState: FlagState{
				ReviewPromptFileSet: true,
			},
			flagValues: ResolvedConfig{
				ReviewPromptFile: "/nonexistent/prompt.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading env prompt file",
			envState: EnvState{
				ReviewPromptFileSet: true,
				ReviewPromptFile:    "/nonexistent/prompt.txt",
			},
			wantErr: true,
		},
		{
			name: "error reading config prompt file",
			cfg: &Config{
				ReviewPromptFile: strPtr("/nonexistent/prompt.txt"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePrompt(tt.cfg, tt.envState, tt.flagState, tt.flagValues)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolvePrompt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// For default prompt, just check it starts with expected text
				if tt.want == "You are a code reviewer" {
					if !strings.HasPrefix(got, tt.want) {
						t.Errorf("ResolvePrompt() = %q, want prompt starting with %q", got, tt.want)
					}
				} else if got != tt.want {
					t.Errorf("ResolvePrompt() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// Tests for agent config

func TestLoadFromPathWithWarnings_AgentConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".acr.yaml")

	content := `agent: claude
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

	if cfg.Agent == nil || *cfg.Agent != "claude" {
		t.Errorf("expected agent=claude, got %v", cfg.Agent)
	}
}

func TestValidate_Agent(t *testing.T) {
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
			cfg := Config{Agent: strPtr(tt.agent)}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_Agent_FlagOverridesAll(t *testing.T) {
	cfg := &Config{Agent: strPtr("gemini")}
	envState := EnvState{Agent: "claude", AgentSet: true}
	flagState := FlagState{AgentSet: true}
	flagValues := ResolvedConfig{Agent: "codex"}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Agent != "codex" {
		t.Errorf("expected flag value 'codex', got %q", result.Agent)
	}
}

func TestResolve_Agent_EnvOverridesConfig(t *testing.T) {
	cfg := &Config{Agent: strPtr("gemini")}
	envState := EnvState{Agent: "claude", AgentSet: true}
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Agent != "claude" {
		t.Errorf("expected env value 'claude', got %q", result.Agent)
	}
}

func TestResolve_Agent_ConfigOverridesDefault(t *testing.T) {
	cfg := &Config{Agent: strPtr("gemini")}
	envState := EnvState{}   // no env vars set
	flagState := FlagState{} // no flags set
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Agent != "gemini" {
		t.Errorf("expected config value 'gemini', got %q", result.Agent)
	}
}

func TestResolve_Agent_DefaultsToCodex(t *testing.T) {
	cfg := &Config{} // empty config
	envState := EnvState{}
	flagState := FlagState{}
	flagValues := ResolvedConfig{}

	result := Resolve(cfg, envState, flagState, flagValues)

	if result.Agent != "codex" {
		t.Errorf("expected default agent 'codex', got %q", result.Agent)
	}
}

func TestLoadEnvState_Agent(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_AGENT", original)
		} else {
			os.Unsetenv("ACR_AGENT")
		}
	}()

	os.Setenv("ACR_AGENT", "claude")
	state := LoadEnvState()

	if !state.AgentSet {
		t.Error("expected AgentSet to be true")
	}
	if state.Agent != "claude" {
		t.Errorf("expected agent='claude', got %q", state.Agent)
	}
}

func TestLoadEnvState_Agent_NotSet(t *testing.T) {
	// Save and restore original env
	original := os.Getenv("ACR_AGENT")
	defer func() {
		if original != "" {
			os.Setenv("ACR_AGENT", original)
		} else {
			os.Unsetenv("ACR_AGENT")
		}
	}()

	os.Unsetenv("ACR_AGENT")
	state := LoadEnvState()

	if state.AgentSet {
		t.Error("expected AgentSet to be false")
	}
	if state.Agent != "" {
		t.Errorf("expected empty agent, got %q", state.Agent)
	}
}

func TestResolvePrompt_Precedence(t *testing.T) {
	// Test that verifies the exact precedence order
	dir := t.TempDir()
	promptFile := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	// All sources set, flag prompt should win
	cfg := &Config{
		ReviewPrompt:     strPtr("config prompt"),
		ReviewPromptFile: strPtr(promptFile),
	}
	envState := EnvState{
		ReviewPromptSet:     true,
		ReviewPrompt:        "env prompt",
		ReviewPromptFileSet: true,
		ReviewPromptFile:    promptFile,
	}
	flagState := FlagState{
		ReviewPromptSet:     true,
		ReviewPromptFileSet: false,
	}
	flagValues := ResolvedConfig{
		ReviewPrompt:     "flag prompt",
		ReviewPromptFile: "",
	}

	got, err := ResolvePrompt(cfg, envState, flagState, flagValues)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "flag prompt" {
		t.Errorf("expected 'flag prompt', got %q", got)
	}
}
