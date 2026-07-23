package config

import (
	"context"
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

const ConfigFileName = ".acr.yaml"

type Duration time.Duration

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

func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}

type Config struct {
	Reviewers         *int               `yaml:"reviewers"`
	Concurrency       *int               `yaml:"concurrency"`
	Base              *string            `yaml:"base"`
	Timeout           *Duration          `yaml:"timeout"`
	Retries           *int               `yaml:"retries"`
	Fetch             *bool              `yaml:"fetch"`
	ReviewerAgent     *string            `yaml:"reviewer_agent"`
	ReviewerAgents    []string           `yaml:"reviewer_agents"`
	SummarizerAgent   *string            `yaml:"summarizer_agent"`
	ReviewerModel     *string            `yaml:"reviewer_model"`
	SummarizerModel   *string            `yaml:"summarizer_model"`
	SummarizerTimeout *Duration          `yaml:"summarizer_timeout"`
	FPFilterTimeout   *Duration          `yaml:"fp_filter_timeout"`
	GuidanceFile      *string            `yaml:"guidance_file"`
	Filters           FilterConfig       `yaml:"filters"`
	FPFilter          FPFilterConfig     `yaml:"fp_filter"`
	PRFeedback        PRFeedbackConfig   `yaml:"pr_feedback"`
	Watch             WatchConfig        `yaml:"watch"`
	Adjudication      AdjudicationConfig `yaml:"adjudication"`
}

// AdjudicationConfig carries the review convergence loop's budget policy,
// stop policy, and evaluation guidance. Unlike every other Config field,
// callers must never resolve this section from a plain filesystem or
// worktree read: it is only ever meaningful when loaded from a trusted
// control-plane source outside the reviewed pull request head and worktree,
// via internal/store's ResolveAdjudicationPolicy.
type AdjudicationConfig struct {
	MaxIterations       *int     `yaml:"max_iterations"`
	MaxCostUSD          *float64 `yaml:"max_cost_usd"`
	StopOnCleanRun      *bool    `yaml:"stop_on_clean_run"`
	StopOnNoNewFindings *bool    `yaml:"stop_on_no_new_findings"`
	EvaluationGuidance  *string  `yaml:"evaluation_guidance"`
}

type WatchConfig struct {
	PollInterval *Duration `yaml:"poll_interval"`
	SettleTime   *Duration `yaml:"settle_time"`
	MaxReviews   *int      `yaml:"max_reviews"`
	MaxDuration  *Duration `yaml:"max_duration"`
}

type FPFilterConfig struct {
	Enabled   *bool `yaml:"enabled"`
	Threshold *int  `yaml:"threshold"`
}

type PRFeedbackConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Agent   *string `yaml:"agent"`
}

type FilterConfig struct {
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

func LoadWithWarnings() (*LoadResult, error) {
	repoRoot, err := git.GetRoot()
	if err != nil {

		return &LoadResult{Config: &Config{}}, nil
	}

	configPath := filepath.Join(repoRoot, ConfigFileName)
	data, err := git.ReadFileWithinRepository(repoRoot, ConfigFileName)
	if os.IsNotExist(err) {
		result := newRepositoryFileSystemLoadResult(repoRoot, configPath, nil, false)
		result.Config = &Config{}
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	result, err := loadDataWithWarnings(data)
	fileSystemResult := newRepositoryFileSystemLoadResult(repoRoot, configPath, data, true)
	if result != nil {
		result.ConfigDir = fileSystemResult.ConfigDir
		result.Source = fileSystemResult.Source
		result.readConfigRelative = fileSystemResult.readConfigRelative
	}
	return result, err
}

func LoadFromDirWithWarnings(dir string) (*LoadResult, error) {
	configPath := filepath.Join(dir, ConfigFileName)
	return LoadFromPathWithWarnings(configPath)
}

type LoadResult struct {
	Config             *Config
	ConfigDir          string
	Warnings           []string
	Source             SourceIdentity
	readConfigRelative func(context.Context, string) ([]byte, error)
}

func LoadFromPathWithWarnings(path string) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		result := newFileSystemLoadResult(path, nil, false)
		result.Config = &Config{}
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	result, err := loadDataWithWarnings(data)
	fileSystemResult := newFileSystemLoadResult(path, data, true)
	if result != nil {
		result.ConfigDir = fileSystemResult.ConfigDir
		result.Source = fileSystemResult.Source
		result.readConfigRelative = fileSystemResult.readConfigRelative
	}
	return result, err
}

func newRepositoryFileSystemLoadResult(repositoryRoot, path string, data []byte, configPresent bool) *LoadResult {
	result := newFileSystemLoadResult(path, data, configPresent)
	result.ConfigDir = repositoryRoot
	result.readConfigRelative = func(_ context.Context, relativePath string) ([]byte, error) {
		return git.ReadFileWithinRepository(repositoryRoot, relativePath)
	}
	return result
}

func (c *Config) validatePatterns() error {
	for _, pattern := range c.Filters.ExcludePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex pattern %q in %s: %w", pattern, ConfigFileName, err)
		}
	}
	return nil
}

var knownTopLevelKeys = []string{"reviewers", "concurrency", "base", "timeout", "retries", "fetch", "reviewer_agent", "reviewer_agents", "summarizer_agent", "reviewer_model", "summarizer_model", "summarizer_timeout", "fp_filter_timeout", "guidance_file", "filters", "fp_filter", "pr_feedback", "watch", "adjudication"}

var knownFPFilterKeys = []string{"enabled", "threshold"}

var knownPRFeedbackKeys = []string{"enabled", "agent"}

var knownWatchKeys = []string{"poll_interval", "settle_time", "max_reviews", "max_duration"}

var knownFilterKeys = []string{"exclude_patterns"}

var knownAdjudicationKeys = []string{"max_iterations", "max_cost_usd", "stop_on_clean_run", "stop_on_no_new_findings", "evaluation_guidance"}

func checkUnknownKeys(data []byte) []string {
	var warnings []string

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {

		return nil
	}

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

	if watch, ok := raw["watch"].(map[string]any); ok {
		for key := range watch {
			if !slices.Contains(knownWatchKeys, key) {
				warning := fmt.Sprintf("unknown key %q in watch section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownWatchKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if adjudication, ok := raw["adjudication"].(map[string]any); ok {
		for key := range adjudication {
			if !slices.Contains(knownAdjudicationKeys, key) {
				warning := fmt.Sprintf("unknown key %q in adjudication section of %s", key, ConfigFileName)
				if suggestion := findSimilar(key, knownAdjudicationKeys); suggestion != "" {
					warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	return warnings
}

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

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	matrix := make([][]int, len(ra)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(rb)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	return matrix[len(ra)][len(rb)]
}

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

func Merge(cfg *Config, cliPatterns []string) []string {
	if cfg == nil {
		return cliPatterns
	}
	return append(cfg.Filters.ExcludePatterns, cliPatterns...)
}

func (c *Config) Validate() error {
	resolved := Resolve(c, EnvState{}, FlagState{}, Defaults)
	errs := resolved.ValidateAll()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

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
	if r.SummarizerTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("summarizer_timeout must be > 0, got %s", r.SummarizerTimeout))
	}
	if r.FPFilterTimeout <= 0 {
		errs = append(errs, fmt.Sprintf("fp_filter_timeout must be > 0, got %s", r.FPFilterTimeout))
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
	if r.WatchPollInterval <= 0 {
		errs = append(errs, fmt.Sprintf("watch.poll_interval must be > 0, got %s", r.WatchPollInterval))
	}
	if r.WatchSettleTime < 0 {
		errs = append(errs, fmt.Sprintf("watch.settle_time must be >= 0, got %s", r.WatchSettleTime))
	}
	if r.WatchMaxReviews < 1 {
		errs = append(errs, fmt.Sprintf("watch.max_reviews must be >= 1, got %d", r.WatchMaxReviews))
	}
	if r.WatchMaxDuration <= 0 {
		errs = append(errs, fmt.Sprintf("watch.max_duration must be > 0, got %s", r.WatchMaxDuration))
	}
	return errs
}

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
	SummarizerTimeout: 5 * time.Minute,
	FPFilterTimeout:   5 * time.Minute,
	FPFilterEnabled:   true,
	FPThreshold:       75,
	PRFeedbackEnabled: true,
	PRFeedbackAgent:   "",
	WatchPollInterval: time.Minute,
	WatchSettleTime:   10 * time.Minute,
	WatchMaxReviews:   10,
	WatchMaxDuration:  24 * time.Hour,
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
	ReviewerModel     string
	SummarizerModel   string
	SummarizerTimeout time.Duration
	FPFilterTimeout   time.Duration
	Guidance          string
	GuidanceFile      string
	FPFilterEnabled   bool
	FPThreshold       int
	PRFeedbackEnabled bool
	PRFeedbackAgent   string
	WatchPollInterval time.Duration
	WatchSettleTime   time.Duration
	WatchMaxReviews   int
	WatchMaxDuration  time.Duration
}

type FlagState struct {
	ReviewersSet         bool
	ConcurrencySet       bool
	BaseSet              bool
	TimeoutSet           bool
	RetriesSet           bool
	FetchSet             bool
	ReviewerAgentsSet    bool
	SummarizerAgentSet   bool
	ReviewerModelSet     bool
	SummarizerModelSet   bool
	SummarizerTimeoutSet bool
	FPFilterTimeoutSet   bool
	GuidanceSet          bool
	GuidanceFileSet      bool
	NoFPFilterSet        bool
	FPThresholdSet       bool
	NoPRFeedbackSet      bool
	PRFeedbackAgentSet   bool

	WatchPollIntervalSet bool
	WatchSettleTimeSet   bool
	WatchMaxReviewsSet   bool
	WatchMaxDurationSet  bool
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
	ReviewerModel        string
	ReviewerModelSet     bool
	SummarizerModel      string
	SummarizerModelSet   bool
	SummarizerTimeout    time.Duration
	SummarizerTimeoutSet bool
	FPFilterTimeout      time.Duration
	FPFilterTimeoutSet   bool
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
	WatchPollInterval    time.Duration
	WatchPollIntervalSet bool
}

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
	if v := os.Getenv("ACR_REVIEWER_MODEL"); v != "" {
		state.ReviewerModel = v
		state.ReviewerModelSet = true
	}
	if v := os.Getenv("ACR_SUMMARIZER_MODEL"); v != "" {
		state.SummarizerModel = v
		state.SummarizerModelSet = true
	}
	if v := os.Getenv("ACR_SUMMARIZER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.SummarizerTimeout = d
			state.SummarizerTimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.SummarizerTimeout = time.Duration(secs) * time.Second
			state.SummarizerTimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_SUMMARIZER_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
	}
	if v := os.Getenv("ACR_FP_FILTER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.FPFilterTimeout = d
			state.FPFilterTimeoutSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.FPFilterTimeout = time.Duration(secs) * time.Second
			state.FPFilterTimeoutSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_FP_FILTER_TIMEOUT=%q is not a valid duration or integer, ignoring", v))
		}
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
	if v := os.Getenv("ACR_WATCH_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			state.WatchPollInterval = d
			state.WatchPollIntervalSet = true
		} else if secs, err := strconv.Atoi(v); err == nil {
			state.WatchPollInterval = time.Duration(secs) * time.Second
			state.WatchPollIntervalSet = true
		} else {
			warnings = append(warnings, fmt.Sprintf("ACR_WATCH_POLL_INTERVAL=%q is not a valid duration or integer, ignoring", v))
		}
	}

	return state, warnings
}

func Resolve(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig) ResolvedConfig {
	result := Defaults

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

		if len(cfg.ReviewerAgents) > 0 {
			result.ReviewerAgents = cfg.ReviewerAgents
		} else if cfg.ReviewerAgent != nil {
			result.ReviewerAgents = []string{*cfg.ReviewerAgent}
		}
		if cfg.SummarizerAgent != nil {
			result.SummarizerAgent = *cfg.SummarizerAgent
		}
		if cfg.ReviewerModel != nil {
			result.ReviewerModel = *cfg.ReviewerModel
		}
		if cfg.SummarizerModel != nil {
			result.SummarizerModel = *cfg.SummarizerModel
		}
		if cfg.SummarizerTimeout != nil {
			result.SummarizerTimeout = cfg.SummarizerTimeout.AsDuration()
		}
		if cfg.FPFilterTimeout != nil {
			result.FPFilterTimeout = cfg.FPFilterTimeout.AsDuration()
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
		if cfg.Watch.PollInterval != nil {
			result.WatchPollInterval = cfg.Watch.PollInterval.AsDuration()
		}
		if cfg.Watch.SettleTime != nil {
			result.WatchSettleTime = cfg.Watch.SettleTime.AsDuration()
		}
		if cfg.Watch.MaxReviews != nil {
			result.WatchMaxReviews = *cfg.Watch.MaxReviews
		}
		if cfg.Watch.MaxDuration != nil {
			result.WatchMaxDuration = cfg.Watch.MaxDuration.AsDuration()
		}
	}

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
	if envState.ReviewerModelSet {
		result.ReviewerModel = envState.ReviewerModel
	}
	if envState.SummarizerModelSet {
		result.SummarizerModel = envState.SummarizerModel
	}
	if envState.SummarizerTimeoutSet {
		result.SummarizerTimeout = envState.SummarizerTimeout
	}
	if envState.FPFilterTimeoutSet {
		result.FPFilterTimeout = envState.FPFilterTimeout
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
	if envState.WatchPollIntervalSet {
		result.WatchPollInterval = envState.WatchPollInterval
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
	if flagState.ReviewerModelSet {
		result.ReviewerModel = flagValues.ReviewerModel
	}
	if flagState.SummarizerModelSet {
		result.SummarizerModel = flagValues.SummarizerModel
	}
	if flagState.SummarizerTimeoutSet {
		result.SummarizerTimeout = flagValues.SummarizerTimeout
	}
	if flagState.FPFilterTimeoutSet {
		result.FPFilterTimeout = flagValues.FPFilterTimeout
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
	if flagState.WatchPollIntervalSet {
		result.WatchPollInterval = flagValues.WatchPollInterval
	}
	if flagState.WatchSettleTimeSet {
		result.WatchSettleTime = flagValues.WatchSettleTime
	}
	if flagState.WatchMaxReviewsSet {
		result.WatchMaxReviews = flagValues.WatchMaxReviews
	}
	if flagState.WatchMaxDurationSet {
		result.WatchMaxDuration = flagValues.WatchMaxDuration
	}

	return result
}

func ResolveGuidance(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig, configDir string) (string, error) {
	readConfigRelative := func(_ context.Context, relativePath string) ([]byte, error) {
		guidancePath := relativePath
		if !filepath.IsAbs(guidancePath) && configDir != "" {
			guidancePath = filepath.Join(configDir, guidancePath)
		}
		return os.ReadFile(guidancePath)
	}
	return resolveGuidance(context.Background(), cfg, envState, flagState, flagValues, readConfigRelative)
}

func ResolveGuidanceFromLoadResult(ctx context.Context, result *LoadResult, envState EnvState, flagState FlagState, flagValues ResolvedConfig) (string, error) {
	if result == nil {
		return resolveGuidance(ctx, nil, envState, flagState, flagValues, nil)
	}
	return resolveGuidance(ctx, result.Config, envState, flagState, flagValues, result.readConfigRelative)
}

func resolveGuidance(ctx context.Context, cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig, readConfigRelative func(context.Context, string) ([]byte, error)) (string, error) {
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
		if readConfigRelative == nil {
			return "", fmt.Errorf("failed to read guidance file %q: no trusted configuration input source", *cfg.GuidanceFile)
		}
		content, err := readConfigRelative(ctx, *cfg.GuidanceFile)
		if err != nil {
			return "", fmt.Errorf("failed to read guidance file %q: %w", *cfg.GuidanceFile, err)
		}
		return string(content), nil
	}
	return "", nil
}
