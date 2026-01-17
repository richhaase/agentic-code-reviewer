// Package config provides configuration file support for acr.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// ConfigFileName is the name of the config file.
const ConfigFileName = ".acr.yaml"

// Duration is a custom type that handles YAML duration parsing.
// Supports both Go duration format ("5m", "300s") and numeric seconds.
type Duration time.Duration

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw interface{}
	if err := unmarshal(&raw); err != nil {
		return err
	}

	switch v := raw.(type) {
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", v, err)
		}
		*d = Duration(parsed)
	case int:
		*d = Duration(time.Duration(v) * time.Second)
	case float64:
		*d = Duration(time.Duration(v) * time.Second)
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
	return nil
}

// Duration returns the underlying time.Duration.
func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}

// Config represents the acr configuration file.
type Config struct {
	Reviewers        *int         `yaml:"reviewers"`
	Concurrency      *int         `yaml:"concurrency"`
	Base             *string      `yaml:"base"`
	Timeout          *Duration    `yaml:"timeout"`
	Retries          *int         `yaml:"retries"`
	ReviewerAgent    *string      `yaml:"reviewer_agent"`
	SummarizerAgent  *string      `yaml:"summarizer_agent"`
	ReviewPrompt     *string      `yaml:"review_prompt"`
	ReviewPromptFile *string      `yaml:"review_prompt_file"`
	Filters          FilterConfig `yaml:"filters"`
}

// FilterConfig holds filter-related configuration.
type FilterConfig struct {
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

// LoadWithWarnings reads .acr.yaml from the git repository root and returns warnings.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadWithWarnings() (*LoadResult, error) {
	repoRoot, err := git.GetRoot()
	if err != nil {
		// Not in a git repo - return empty config
		return &LoadResult{Config: &Config{}}, nil
	}

	configPath := filepath.Join(repoRoot, ConfigFileName)
	return LoadFromPathWithWarnings(configPath)
}

// LoadFromDirWithWarnings reads .acr.yaml from the specified directory and returns warnings.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromDirWithWarnings(dir string) (*LoadResult, error) {
	configPath := filepath.Join(dir, ConfigFileName)
	return LoadFromPathWithWarnings(configPath)
}

// LoadResult contains the loaded config and any warnings encountered.
type LoadResult struct {
	Config   *Config
	Warnings []string
}

// LoadFromPathWithWarnings reads a config file and returns warnings for unknown keys.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromPathWithWarnings(path string) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &LoadResult{Config: &Config{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Check for unknown keys using strict mode
	warnings := checkUnknownKeys(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ConfigFileName, err)
	}

	// Validate regex patterns
	if err := cfg.validatePatterns(); err != nil {
		return nil, err
	}

	// Validate config values
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFileName, err)
	}

	return &LoadResult{Config: &cfg, Warnings: warnings}, nil
}

// validatePatterns checks that all exclude patterns are valid regex.
func (c *Config) validatePatterns() error {
	for _, pattern := range c.Filters.ExcludePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex pattern %q in %s: %w", pattern, ConfigFileName, err)
		}
	}
	return nil
}

// knownTopLevelKeys are the valid top-level keys in the config file.
var knownTopLevelKeys = []string{"reviewers", "concurrency", "base", "timeout", "retries", "reviewer_agent", "summarizer_agent", "review_prompt", "review_prompt_file", "filters"}

// knownFilterKeys are the valid keys under the "filters" section.
var knownFilterKeys = []string{"exclude_patterns"}

// checkUnknownKeys checks for unknown keys in the YAML data and returns warnings.
func checkUnknownKeys(data []byte) []string {
	var warnings []string

	// Parse into a generic map to inspect keys
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// If we can't parse, let the main parser handle the error
		return nil
	}

	// Check top-level keys
	for key := range raw {
		if !slices.Contains(knownTopLevelKeys, key) {
			warning := fmt.Sprintf("unknown key %q in %s", key, ConfigFileName)
			if suggestion := findSimilar(key, knownTopLevelKeys); suggestion != "" {
				warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			warnings = append(warnings, warning)
		}
	}

	// Check keys under "filters" section
	if filters, ok := raw["filters"].(map[string]any); ok {
		for key := range filters {
			if !slices.Contains(knownFilterKeys, key) {
				warning := fmt.Sprintf("unknown key %q in filters section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownFilterKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	return warnings
}

// findSimilar finds the most similar string from candidates using Levenshtein distance.
// Returns empty string if no candidate is similar enough (threshold: 3 edits).
func findSimilar(input string, candidates []string) string {
	const maxDistance = 3
	bestMatch := ""
	bestDistance := maxDistance + 1

	for _, candidate := range candidates {
		dist := levenshtein(input, candidate)
		if dist < bestDistance {
			bestDistance = dist
			bestMatch = candidate
		}
	}

	if bestDistance <= maxDistance {
		return bestMatch
	}
	return ""
}

// levenshtein calculates the Levenshtein distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	// Create matrix
	matrix := make([][]int, len(ra)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(rb)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(ra)][len(rb)]
}

// Merge combines config file patterns with CLI patterns.
// CLI patterns are appended after config patterns (both are applied).
func Merge(cfg *Config, cliPatterns []string) []string {
	if cfg == nil {
		return cliPatterns
	}
	return append(cfg.Filters.ExcludePatterns, cliPatterns...)
}

// Validate checks that all config values are valid.
func (c *Config) Validate() error {
	if c.Reviewers != nil && *c.Reviewers < 1 {
		return fmt.Errorf("reviewers must be >= 1, got %d", *c.Reviewers)
	}
	if c.Concurrency != nil && *c.Concurrency < 0 {
		return fmt.Errorf("concurrency must be >= 0, got %d", *c.Concurrency)
	}
	if c.Retries != nil && *c.Retries < 0 {
		return fmt.Errorf("retries must be >= 0, got %d", *c.Retries)
	}
	if c.Timeout != nil && *c.Timeout <= 0 {
		return fmt.Errorf("timeout must be > 0, got %s", time.Duration(*c.Timeout))
	}
	if c.ReviewerAgent != nil && !slices.Contains(agent.SupportedAgents, *c.ReviewerAgent) {
		return fmt.Errorf("reviewer_agent must be one of %v, got %q", agent.SupportedAgents, *c.ReviewerAgent)
	}
	if c.SummarizerAgent != nil && !slices.Contains(agent.SupportedAgents, *c.SummarizerAgent) {
		return fmt.Errorf("summarizer_agent must be one of %v, got %q", agent.SupportedAgents, *c.SummarizerAgent)
	}
	return nil
}

// Defaults holds the built-in default values.
var Defaults = ResolvedConfig{
	Reviewers:       5,
	Concurrency:     0, // means "same as reviewers"
	Base:            "main",
	Timeout:         10 * time.Minute,
	Retries:         1,
	ReviewerAgent:   agent.DefaultAgent,
	SummarizerAgent: agent.DefaultSummarizerAgent,
}

// ResolvedConfig holds the final resolved configuration values.
type ResolvedConfig struct {
	Reviewers        int
	Concurrency      int
	Base             string
	Timeout          time.Duration
	Retries          int
	ReviewerAgent    string
	SummarizerAgent  string
	ReviewPrompt     string
	ReviewPromptFile string
}

// FlagState tracks whether a flag was explicitly set.
type FlagState struct {
	ReviewersSet        bool
	ConcurrencySet      bool
	BaseSet             bool
	TimeoutSet          bool
	RetriesSet          bool
	ReviewerAgentSet    bool
	SummarizerAgentSet  bool
	ReviewPromptSet     bool
	ReviewPromptFileSet bool
}

// EnvState captures env var values and whether they were set.
type EnvState struct {
	Reviewers           int
	ReviewersSet        bool
	Concurrency         int
	ConcurrencySet      bool
	Base                string
	BaseSet             bool
	Timeout             time.Duration
	TimeoutSet          bool
	Retries             int
	RetriesSet          bool
	ReviewerAgent       string
	ReviewerAgentSet    bool
	SummarizerAgent     string
	SummarizerAgentSet  bool
	ReviewPrompt        string
	ReviewPromptSet     bool
	ReviewPromptFile    string
	ReviewPromptFileSet bool
}

// LoadEnvState reads environment variables and returns their state.
func LoadEnvState() EnvState {
	var state EnvState

	if v := os.Getenv("ACR_REVIEWERS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Reviewers = i
			state.ReviewersSet = true
		}
	}
	if v := os.Getenv("ACR_CONCURRENCY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Concurrency = i
			state.ConcurrencySet = true
		}
	}
	if v := os.Getenv("ACR_BASE_REF"); v != "" {
		state.Base = v
		state.BaseSet = true
	}
	if v := os.Getenv("ACR_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.Timeout = d
			state.TimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.Timeout = time.Duration(secs) * time.Second
			state.TimeoutSet = true
		}
	}
	if v := os.Getenv("ACR_RETRIES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Retries = i
			state.RetriesSet = true
		}
	}
	if v := os.Getenv("ACR_REVIEWER_AGENT"); v != "" {
		state.ReviewerAgent = v
		state.ReviewerAgentSet = true
	}
	if v := os.Getenv("ACR_SUMMARIZER_AGENT"); v != "" {
		state.SummarizerAgent = v
		state.SummarizerAgentSet = true
	}
	if v := os.Getenv("ACR_REVIEW_PROMPT"); v != "" {
		state.ReviewPrompt = v
		state.ReviewPromptSet = true
	}
	if v := os.Getenv("ACR_REVIEW_PROMPT_FILE"); v != "" {
		state.ReviewPromptFile = v
		state.ReviewPromptFileSet = true
	}

	return state
}

// Resolve merges config file values with env vars and flags.
// Precedence: flags > env vars > config file > defaults
func Resolve(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig) ResolvedConfig {
	result := Defaults

	// Apply config file values (if set)
	if cfg != nil {
		if cfg.Reviewers != nil {
			result.Reviewers = *cfg.Reviewers
		}
		if cfg.Concurrency != nil {
			result.Concurrency = *cfg.Concurrency
		}
		if cfg.Base != nil {
			result.Base = *cfg.Base
		}
		if cfg.Timeout != nil {
			result.Timeout = cfg.Timeout.AsDuration()
		}
		if cfg.Retries != nil {
			result.Retries = *cfg.Retries
		}
		if cfg.ReviewerAgent != nil {
			result.ReviewerAgent = *cfg.ReviewerAgent
		}
		if cfg.SummarizerAgent != nil {
			result.SummarizerAgent = *cfg.SummarizerAgent
		}
		if cfg.ReviewPrompt != nil {
			result.ReviewPrompt = *cfg.ReviewPrompt
		}
		if cfg.ReviewPromptFile != nil {
			result.ReviewPromptFile = *cfg.ReviewPromptFile
		}
	}

	// Apply env var values (if set)
	if envState.ReviewersSet {
		result.Reviewers = envState.Reviewers
	}
	if envState.ConcurrencySet {
		result.Concurrency = envState.Concurrency
	}
	if envState.BaseSet {
		result.Base = envState.Base
	}
	if envState.TimeoutSet {
		result.Timeout = envState.Timeout
	}
	if envState.RetriesSet {
		result.Retries = envState.Retries
	}
	if envState.ReviewerAgentSet {
		result.ReviewerAgent = envState.ReviewerAgent
	}
	if envState.SummarizerAgentSet {
		result.SummarizerAgent = envState.SummarizerAgent
	}
	if envState.ReviewPromptSet {
		result.ReviewPrompt = envState.ReviewPrompt
	}
	if envState.ReviewPromptFileSet {
		result.ReviewPromptFile = envState.ReviewPromptFile
	}

	// Apply flag values (if explicitly set)
	if flagState.ReviewersSet {
		result.Reviewers = flagValues.Reviewers
	}
	if flagState.ConcurrencySet {
		result.Concurrency = flagValues.Concurrency
	}
	if flagState.BaseSet {
		result.Base = flagValues.Base
	}
	if flagState.TimeoutSet {
		result.Timeout = flagValues.Timeout
	}
	if flagState.RetriesSet {
		result.Retries = flagValues.Retries
	}
	if flagState.ReviewerAgentSet {
		result.ReviewerAgent = flagValues.ReviewerAgent
	}
	if flagState.SummarizerAgentSet {
		result.SummarizerAgent = flagValues.SummarizerAgent
	}
	if flagState.ReviewPromptSet {
		result.ReviewPrompt = flagValues.ReviewPrompt
	}
	if flagState.ReviewPromptFileSet {
		result.ReviewPromptFile = flagValues.ReviewPromptFile
	}

	return result
}

// ResolvePrompt resolves the final review prompt with custom precedence logic.
// Unlike other config fields, prompts have a special precedence where prompt-file
// sources are checked separately from prompt string sources.
//
// Precedence (highest to lowest):
// 1. --prompt flag
// 2. --prompt-file flag
// 3. ACR_REVIEW_PROMPT env var
// 4. ACR_REVIEW_PROMPT_FILE env var
// 5. review_prompt config field
// 6. review_prompt_file config field
// 7. DefaultClaudePrompt constant
//
// Returns the resolved prompt and an error if a prompt file cannot be read.
func ResolvePrompt(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig) (string, error) {
	// 1. Check --prompt flag (highest priority)
	if flagState.ReviewPromptSet && flagValues.ReviewPrompt != "" {
		return flagValues.ReviewPrompt, nil
	}

	// 2. Check --prompt-file flag
	if flagState.ReviewPromptFileSet && flagValues.ReviewPromptFile != "" {
		content, err := os.ReadFile(flagValues.ReviewPromptFile)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt file %q: %w", flagValues.ReviewPromptFile, err)
		}
		return string(content), nil
	}

	// 3. Check ACR_REVIEW_PROMPT env var
	if envState.ReviewPromptSet && envState.ReviewPrompt != "" {
		return envState.ReviewPrompt, nil
	}

	// 4. Check ACR_REVIEW_PROMPT_FILE env var
	if envState.ReviewPromptFileSet && envState.ReviewPromptFile != "" {
		content, err := os.ReadFile(envState.ReviewPromptFile)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt file %q: %w", envState.ReviewPromptFile, err)
		}
		return string(content), nil
	}

	// 5. Check review_prompt config field
	if cfg != nil && cfg.ReviewPrompt != nil && *cfg.ReviewPrompt != "" {
		return *cfg.ReviewPrompt, nil
	}

	// 6. Check review_prompt_file config field
	if cfg != nil && cfg.ReviewPromptFile != nil && *cfg.ReviewPromptFile != "" {
		content, err := os.ReadFile(*cfg.ReviewPromptFile)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt file %q: %w", *cfg.ReviewPromptFile, err)
		}
		return string(content), nil
	}

	// 7. No explicit prompt configured - return empty to let agent use its default behavior
	return "", nil
}
