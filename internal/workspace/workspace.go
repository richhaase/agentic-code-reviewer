package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

const (
	ConfigDirEnvVar      = "ACR_CONFIG_DIR"
	ConfigFileName       = "workspace.yaml"
	CurrentSchemaVersion = 1
)

type OwnPRPolicy string

const (
	OwnPRPolicyDisabled    OwnPRPolicy = "disabled"
	OwnPRPolicyCommentOnly OwnPRPolicy = "comment-only"
)

type IdentityConfig struct {
	ExpectedUser string `yaml:"expected_user"`
}

type ScopeConfig struct {
	Organizations   []string          `yaml:"organizations"`
	Teams           []string          `yaml:"teams"`
	RepositoryRoots []string          `yaml:"repository_roots"`
	Include         []string          `yaml:"include"`
	Exclude         []string          `yaml:"exclude"`
	PathOverrides   map[string]string `yaml:"path_overrides"`
}

type BehaviorConfig struct {
	PollInterval config.Duration `yaml:"poll_interval"`
	SettleTime   config.Duration `yaml:"settle_time"`
	Concurrency  int             `yaml:"concurrency"`
	AutoReview   bool            `yaml:"auto_review"`
	ReReview     bool            `yaml:"re_review"`
	OwnPRPolicy  OwnPRPolicy     `yaml:"own_pr_policy"`
}

type PostingConfig struct {
	Enabled bool `yaml:"enabled"`
}

type NotificationsConfig struct {
	Enabled bool `yaml:"enabled"`
}

type Config struct {
	SchemaVersion int                 `yaml:"schema_version"`
	Identity      IdentityConfig      `yaml:"identity"`
	Scope         ScopeConfig         `yaml:"scope"`
	Behavior      BehaviorConfig      `yaml:"behavior"`
	Posting       PostingConfig       `yaml:"posting"`
	Notifications NotificationsConfig `yaml:"notifications"`
}

func ConfigDir() (string, error) {
	if dir := os.Getenv(ConfigDirEnvVar); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve workspace configuration directory: %w", err)
	}
	return filepath.Join(base, "acr"), nil
}

func ConfigPath(configDir string) string {
	return filepath.Join(configDir, ConfigFileName)
}

func Load(configDir string) (*Config, error) {
	path := ConfigPath(configDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("workspace configuration not found at %s; run `acr desk config init` first", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace configuration: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ConfigFileName, err)
	}
	return &cfg, nil
}

func Init(configDir string, defaultUser string) (string, error) {
	path := ConfigPath(configDir)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists; remove it first or edit it directly", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to check %s: %w", path, err)
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create workspace configuration directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(starterYAML(defaultUser)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}
	return path, nil
}

func starterYAML(defaultUser string) string {
	return fmt.Sprintf(`schema_version: %d
identity:
  expected_user: %q
scope:
  organizations: []
  teams: []
  repository_roots: []
  include: []
  exclude: []
  path_overrides: {}
behavior:
  poll_interval: 1m
  settle_time: 10m
  concurrency: 0
  auto_review: false
  re_review: false
  own_pr_policy: disabled
posting:
  enabled: false
notifications:
  enabled: false
`, CurrentSchemaVersion, defaultUser)
}
