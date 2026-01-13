// Package config provides configuration file support for acr.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/anthropics/agentic-code-reviewer/internal/git"
)

// ConfigFileName is the name of the config file.
const ConfigFileName = ".acr.yaml"

// Config represents the acr configuration file.
type Config struct {
	Filters FilterConfig `yaml:"filters"`
}

// FilterConfig holds filter-related configuration.
type FilterConfig struct {
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

// Load reads .acr.yaml from the git repository root.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func Load() (*Config, error) {
	repoRoot, err := git.GetRoot()
	if err != nil {
		// Not in a git repo - return empty config
		return &Config{}, nil
	}

	configPath := filepath.Join(repoRoot, ConfigFileName)
	return LoadFromPath(configPath)
}

// LoadFromDir reads .acr.yaml from the specified directory.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromDir(dir string) (*Config, error) {
	configPath := filepath.Join(dir, ConfigFileName)
	return LoadFromPath(configPath)
}

// LoadFromPath reads a config file from the specified path.
// Returns an empty config (not error) if the file doesn't exist.
// Returns an error if the file exists but is invalid YAML or contains invalid regex patterns.
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ConfigFileName, err)
	}

	// Validate regex patterns
	if err := cfg.validatePatterns(); err != nil {
		return nil, err
	}

	return &cfg, nil
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

// Merge combines config file patterns with CLI patterns.
// CLI patterns are appended after config patterns (both are applied).
func Merge(cfg *Config, cliPatterns []string) []string {
	if cfg == nil {
		return cliPatterns
	}
	return append(cfg.Filters.ExcludePatterns, cliPatterns...)
}
