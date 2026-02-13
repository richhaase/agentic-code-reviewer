// Package config provides configuration file support for acr.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
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

type Config struct {
	Reviewers       *int             `yaml:"reviewers"`
	Concurrency     *int             `yaml:"concurrency"`
	Base            *string          `yaml:"base"`
	Timeout         *Duration        `yaml:"timeout"`
	Retries         *int             `yaml:"retries"`
	Fetch           *bool            `yaml:"fetch"`
	ReviewerAgent   *string          `yaml:"reviewer_agent"`
	ReviewerAgents  []string         `yaml:"reviewer_agents"`
	SummarizerAgent *string          `yaml:"summarizer_agent"`
	GuidanceFile    *string          `yaml:"guidance_file"`
	Filters         FilterConfig     `yaml:"filters"`
	FPFilter        FPFilterConfig   `yaml:"fp_filter"`
	PRFeedback      PRFeedbackConfig `yaml:"pr_feedback"`
}

type FPFilterConfig struct {
	Enabled   *bool `yaml:"enabled"`
	Threshold *int  `yaml:"threshold"`
}

// PRFeedbackConfig holds PR feedback summarization settings.
type PRFeedbackConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Agent   *string `yaml:"agent"`
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
	Config    *Config
	ConfigDir string // Directory containing the config file (for resolving relative paths)
	Warnings  []string
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
		return &LoadResult{Config: &cfg, ConfigDir: filepath.Dir(path), Warnings: warnings},
			fmt.Errorf("%s: %w", ConfigFileName, err)
	}

	if cfg.ReviewerAgent != nil {
		warnings = append(warnings, `"reviewer_agent" is deprecated, use "reviewer_agents" list instead`)
		if len(cfg.ReviewerAgents) > 0 {
			warnings = append(warnings, `both "reviewer_agent" and "reviewer_agents" are set; "reviewer_agents" takes precedence`)
		}
	}

	return &LoadResult{Config: &cfg, ConfigDir: filepath.Dir(path), Warnings: warnings}, nil
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

var knownTopLevelKeys = []string{"reviewers", "concurrency", "base", "timeout", "retries", "fetch", "reviewer_agent", "reviewer_agents", "summarizer_agent", "guidance_file", "filters", "fp_filter", "pr_feedback"}

var knownFPFilterKeys = []string{"enabled", "threshold"}

var knownPRFeedbackKeys = []string{"enabled", "agent"}

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

	if fpFilter, ok := raw["fp_filter"].(map[string]any); ok {
		for key := range fpFilter {
			if !slices.Contains(knownFPFilterKeys, key) {
				warning := fmt.Sprintf("unknown key %q in fp_filter section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownFPFilterKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if prFeedback, ok := raw["pr_feedback"].(map[string]any); ok {
		for key := range prFeedback {
			if !slices.Contains(knownPRFeedbackKeys, key) {
				warning := fmt.Sprintf("unknown key %q in pr_feedback section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownPRFeedbackKeys); suggestion != "" {
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

// parseCommaSeparated splits a comma-separated string into a slice of trimmed strings.
// Returns nil if no non-empty parts are found, so callers can distinguish
// "not set" from "set but empty".
func parseCommaSeparated(input string) []string {
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// Merge combines config file patterns with CLI patterns.
// CLI patterns are appended after config patterns (both are applied).
func Merge(cfg *Config, cliPatterns []string) []string {
	if cfg == nil {
		return cliPatterns
	}
	return append(cfg.Filters.ExcludePatterns, cliPatterns...)
}

// Validate checks that all config file values are semantically valid.
// Delegates to ResolvedConfig.ValidateAll() by resolving config-only values against defaults,
// so validation rules are defined in one place.
func (c *Config) Validate() error {
	resolved := Resolve(c, EnvState{}, FlagState{}, Defaults)
	errs := resolved.ValidateAll()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// ValidateAll checks that all resolved config values are semantically valid.
// Returns individual error strings so callers can count and report them accurately.
func (r *ResolvedConfig) ValidateAll() []string {
	var errs []string
	if r.Reviewers < 1 {
		errs = append(errs, fmt.Sprintf("reviewers must be >= 1, got %d", r.Reviewers))
	}
	if r.Concurrency < 0 {
		errs = append(errs, fmt.Sprintf("concurrency must be >= 0, got %d", r.Concurrency))
	}
	if r.Retries < 0 {
		errs = append(errs, fmt.Sprintf("retries must be >= 0, got %d", r.Retries))
	}
	if r.Timeout <= 0 {
		errs = append(errs, fmt.Sprintf("timeout must be > 0, got %s", r.Timeout))
	}
	if len(r.ReviewerAgents) == 0 {
		errs = append(errs, "reviewer_agents must not be empty")
	}
	for _, a := range r.ReviewerAgents {
		if !slices.Contains(agent.SupportedAgents, a) {
			errs = append(errs, fmt.Sprintf("reviewer_agents contains unsupported agent %q, must be one of %v", a, agent.SupportedAgents))
		}
	}
	if !slices.Contains(agent.SupportedAgents, r.SummarizerAgent) {
		errs = append(errs, fmt.Sprintf("summarizer_agent must be one of %v, got %q", agent.SupportedAgents, r.SummarizerAgent))
	}
	if r.FPThreshold < 1 || r.FPThreshold > 100 {
		errs = append(errs, fmt.Sprintf("fp_filter.threshold must be 1-100, got %d", r.FPThreshold))
	}
	if r.PRFeedbackAgent != "" && !slices.Contains(agent.SupportedAgents, r.PRFeedbackAgent) {
		errs = append(errs, fmt.Sprintf("pr_feedback.agent must be one of %v, got %q", agent.SupportedAgents, r.PRFeedbackAgent))
	}
	return errs
}

// Validate checks that all resolved config values are semantically valid.
// Returns a single error summarizing all issues, or nil if valid.
func (r *ResolvedConfig) Validate() error {
	errs := r.ValidateAll()
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid resolved configuration:\n  - %s", strings.Join(errs, "\n  - "))
}

var Defaults = ResolvedConfig{
	Reviewers:         5,
	Concurrency:       0,
	Base:              "main",
	Timeout:           10 * time.Minute,
	Retries:           1,
	Fetch:             true,
	ReviewerAgents:    []string{agent.DefaultAgent},
	SummarizerAgent:   agent.DefaultSummarizerAgent,
	FPFilterEnabled:   true,
	FPThreshold:       75,
	PRFeedbackEnabled: true,
	PRFeedbackAgent:   "", // empty means use summarizer agent
}

type ResolvedConfig struct {
	Reviewers         int
	Concurrency       int
	Base              string
	Timeout           time.Duration
	Retries           int
	Fetch             bool
	ReviewerAgents    []string
	SummarizerAgent   string
	Guidance          string
	GuidanceFile      string
	FPFilterEnabled   bool
	FPThreshold       int
	PRFeedbackEnabled bool
	PRFeedbackAgent   string
}

type FlagState struct {
	ReviewersSet       bool
	ConcurrencySet     bool
	BaseSet            bool
	TimeoutSet         bool
	RetriesSet         bool
	FetchSet           bool
	ReviewerAgentsSet  bool
	SummarizerAgentSet bool
	GuidanceSet        bool
	GuidanceFileSet    bool
	NoFPFilterSet      bool
	FPThresholdSet     bool
	NoPRFeedbackSet    bool
	PRFeedbackAgentSet bool
}

type EnvState struct {
	Reviewers            int
	ReviewersSet         bool
	Concurrency          int
	ConcurrencySet       bool
	Base                 string
	BaseSet              bool
	Timeout              time.Duration
	TimeoutSet           bool
	Retries              int
	RetriesSet           bool
	Fetch                bool
	FetchSet             bool
	ReviewerAgents       []string
	ReviewerAgentsSet    bool
	SummarizerAgent      string
	SummarizerAgentSet   bool
	Guidance             string
	GuidanceSet          bool
	GuidanceFile         string
	GuidanceFileSet      bool
	FPFilterEnabled      bool
	FPFilterSet          bool
	FPThreshold          int
	FPThresholdSet       bool
	PRFeedbackEnabled    bool
	PRFeedbackEnabledSet bool
	PRFeedbackAgent      string
	PRFeedbackAgentSet   bool
}

// LoadEnvState reads environment variables and returns their state.
// Returns warnings for any environment variables that are set but have invalid values.
func LoadEnvState() (EnvState, []string) {
	var state EnvState
	var warnings []string

	if v := os.Getenv("ACR_REVIEWERS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Reviewers = i
			state.ReviewersSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_REVIEWERS=%q is not a valid integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_CONCURRENCY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Concurrency = i
			state.ConcurrencySet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_CONCURRENCY=%q is not a valid integer, ignoring", v))
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
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_RETRIES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			state.Retries = i
			state.RetriesSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_RETRIES=%q is not a valid integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_FETCH"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			state.Fetch = true
			state.FetchSet = true
		case "false", "0", "no":
			state.Fetch = false
			state.FetchSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_FETCH=%q is not a valid boolean (use true/false/1/0/yes/no), ignoring", v))
		}
	}
	if v := os.Getenv("ACR_REVIEWER_AGENT"); v != "" {
		if agents := parseCommaSeparated(v); agents != nil {
			state.ReviewerAgents = agents
			state.ReviewerAgentsSet = true
		}
	}
	if v := os.Getenv("ACR_SUMMARIZER_AGENT"); v != "" {
		state.SummarizerAgent = v
		state.SummarizerAgentSet = true
	}
	if v := os.Getenv("ACR_GUIDANCE"); v != "" {
		state.Guidance = v
		state.GuidanceSet = true
	}
	if v := os.Getenv("ACR_GUIDANCE_FILE"); v != "" {
		state.GuidanceFile = v
		state.GuidanceFileSet = true
	}

	if v := os.Getenv("ACR_FP_FILTER"); v != "" {
		switch v {
		case "true", "1":
			state.FPFilterEnabled = true
			state.FPFilterSet = true
		case "false", "0":
			state.FPFilterEnabled = false
			state.FPFilterSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_FP_FILTER=%q is not a valid boolean (use true/false/1/0), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_FP_THRESHOLD"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 1 && i <= 100 {
			state.FPThreshold = i
			state.FPThresholdSet = true
		} else if err != nil {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_THRESHOLD=%q is not a valid integer, ignoring", v))
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_THRESHOLD=%q is out of range (must be 1-100), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_PR_FEEDBACK"); v != "" {
		switch v {
		case "true", "1":
			state.PRFeedbackEnabled = true
			state.PRFeedbackEnabledSet = true
		case "false", "0":
			state.PRFeedbackEnabled = false
			state.PRFeedbackEnabledSet = true
		default:
			warnings = append(warnings, fmt.Sprintf("ACR_PR_FEEDBACK=%q is not a valid boolean (use true/false/1/0), ignoring", v))
		}
	}

	if v := os.Getenv("ACR_PR_FEEDBACK_AGENT"); v != "" {
		state.PRFeedbackAgent = v
		state.PRFeedbackAgentSet = true
	}

	return state, warnings
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
		if cfg.Fetch != nil {
			result.Fetch = *cfg.Fetch
		}
		// reviewer_agents array takes precedence over reviewer_agent scalar
		if len(cfg.ReviewerAgents) > 0 {
			result.ReviewerAgents = cfg.ReviewerAgents
		} else if cfg.ReviewerAgent != nil {
			result.ReviewerAgents = []string{*cfg.ReviewerAgent}
		}
		if cfg.SummarizerAgent != nil {
			result.SummarizerAgent = *cfg.SummarizerAgent
		}
		if cfg.FPFilter.Enabled != nil {
			result.FPFilterEnabled = *cfg.FPFilter.Enabled
		}
		if cfg.FPFilter.Threshold != nil {
			result.FPThreshold = *cfg.FPFilter.Threshold
		}
		if cfg.PRFeedback.Enabled != nil {
			result.PRFeedbackEnabled = *cfg.PRFeedback.Enabled
		}
		if cfg.PRFeedback.Agent != nil {
			result.PRFeedbackAgent = *cfg.PRFeedback.Agent
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
	if envState.FetchSet {
		result.Fetch = envState.Fetch
	}
	if envState.ReviewerAgentsSet {
		result.ReviewerAgents = envState.ReviewerAgents
	}
	if envState.SummarizerAgentSet {
		result.SummarizerAgent = envState.SummarizerAgent
	}
	if envState.FPFilterSet {
		result.FPFilterEnabled = envState.FPFilterEnabled
	}
	if envState.FPThresholdSet {
		result.FPThreshold = envState.FPThreshold
	}
	if envState.PRFeedbackEnabledSet {
		result.PRFeedbackEnabled = envState.PRFeedbackEnabled
	}
	if envState.PRFeedbackAgentSet {
		result.PRFeedbackAgent = envState.PRFeedbackAgent
	}

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
	if flagState.FetchSet {
		result.Fetch = flagValues.Fetch
	}
	if flagState.ReviewerAgentsSet {
		result.ReviewerAgents = flagValues.ReviewerAgents
	}
	if flagState.SummarizerAgentSet {
		result.SummarizerAgent = flagValues.SummarizerAgent
	}
	if flagState.NoFPFilterSet {
		result.FPFilterEnabled = flagValues.FPFilterEnabled
	}
	if flagState.FPThresholdSet {
		result.FPThreshold = flagValues.FPThreshold
	}
	if flagState.NoPRFeedbackSet {
		result.PRFeedbackEnabled = flagValues.PRFeedbackEnabled
	}
	if flagState.PRFeedbackAgentSet {
		result.PRFeedbackAgent = flagValues.PRFeedbackAgent
	}

	return result
}

// ResolveGuidance resolves the review guidance with custom precedence logic.
// Guidance is steering context appended to the built-in prompt, not a replacement.
//
// Precedence (highest to lowest):
// 1. --guidance flag
// 2. --guidance-file flag
// 3. ACR_GUIDANCE env var
// 4. ACR_GUIDANCE_FILE env var
// 5. guidance_file config field
// 6. Empty string (no guidance)
func ResolveGuidance(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig, configDir string) (string, error) {
	if flagState.GuidanceSet && flagValues.Guidance != "" {
		return flagValues.Guidance, nil
	}
	if flagState.GuidanceFileSet && flagValues.GuidanceFile != "" {
		content, err := os.ReadFile(flagValues.GuidanceFile)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", flagValues.GuidanceFile, err)
		}
		return string(content), nil
	}
	if envState.GuidanceSet && envState.Guidance != "" {
		return envState.Guidance, nil
	}
	if envState.GuidanceFileSet && envState.GuidanceFile != "" {
		content, err := os.ReadFile(envState.GuidanceFile)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", envState.GuidanceFile, err)
		}
		return string(content), nil
	}
	if cfg != nil && cfg.GuidanceFile != nil && *cfg.GuidanceFile != "" {
		guidancePath := *cfg.GuidanceFile
		if !filepath.IsAbs(guidancePath) && configDir != "" {
			guidancePath = filepath.Join(configDir, guidancePath)
		}
		content, err := os.ReadFile(guidancePath)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", *cfg.GuidanceFile, err)
		}
		return string(content), nil
	}
	return "", nil
}
