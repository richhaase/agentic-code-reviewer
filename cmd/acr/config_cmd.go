package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage acr configuration",
		Long:  "View, initialize, and validate acr configuration files and environment variables.",
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display resolved configuration",
		Long:  "Show the fully resolved configuration from defaults, config file, and environment variables.",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := config.LoadWithWarnings()
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}

			envState, _ := config.LoadEnvState()

			resolved := config.Resolve(result.Config, envState, config.FlagState{}, config.ResolvedConfig{})

			fmt.Println("Resolved configuration:")
			fmt.Println()
			fmt.Printf("  %-22s %d\n", "reviewers:", resolved.Reviewers)
			fmt.Printf("  %-22s %d\n", "concurrency:", resolved.Concurrency)
			fmt.Printf("  %-22s %s\n", "base:", resolved.Base)
			fmt.Printf("  %-22s %s\n", "timeout:", resolved.Timeout)
			fmt.Printf("  %-22s %d\n", "retries:", resolved.Retries)
			fmt.Printf("  %-22s %t\n", "fetch:", resolved.Fetch)
			fmt.Printf("  %-22s %s\n", "reviewer_agents:", strings.Join(resolved.ReviewerAgents, ", "))
			fmt.Printf("  %-22s %s\n", "summarizer_agent:", resolved.SummarizerAgent)
			fmt.Printf("  %-22s %t\n", "fp_filter.enabled:", resolved.FPFilterEnabled)
			fmt.Printf("  %-22s %d\n", "fp_filter.threshold:", resolved.FPThreshold)
			fmt.Printf("  %-22s %t\n", "pr_feedback.enabled:", resolved.PRFeedbackEnabled)
			if resolved.PRFeedbackAgent != "" {
				fmt.Printf("  %-22s %s\n", "pr_feedback.agent:", resolved.PRFeedbackAgent)
			} else {
				fmt.Printf("  %-22s %s\n", "pr_feedback.agent:", "(same as summarizer_agent)")
			}

			return nil
		},
	}
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a starter .acr.yaml file",
		Long:  "Create a commented .acr.yaml configuration file in the git repository root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Write to git repo root (same location runtime loading uses)
			repoRoot, err := git.GetRoot()
			if err != nil {
				return fmt.Errorf("not in a git repository: %w", err)
			}
			configPath := filepath.Join(repoRoot, config.ConfigFileName)

			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("%s already exists; remove it first or edit it directly", configPath)
			}

			starter := `# acr configuration file
# See https://github.com/richhaase/agentic-code-reviewer for documentation.

# Number of parallel reviewers to run (default: 5)
# reviewers: 5

# Maximum concurrent reviewers (default: same as reviewers)
# concurrency: 0

# Base branch for diff comparison (default: main)
# base: main

# Timeout per reviewer, Go duration format (default: 10m)
# timeout: 10m

# Retry failed reviewers N times (default: 1)
# retries: 1

# Fetch latest base ref from origin before diff (default: true)
# fetch: true

# Agent(s) for reviews: codex, claude, gemini
# reviewer_agents:
#   - codex

# Agent for summarization: codex, claude, gemini
# summarizer_agent: codex

# Path to file containing review guidance
# guidance_file: ""

# Filtering configuration
# filters:
#   exclude_patterns:
#     - "pattern to exclude"

# False positive filtering
# fp_filter:
#   enabled: true
#   threshold: 75

# PR feedback summarization
# pr_feedback:
#   enabled: true
#   agent: ""
`
			if err := os.WriteFile(configPath, []byte(starter), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", configPath, err)
			}

			fmt.Printf("Created %s with default settings (commented out).\n", configPath)
			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment variables",
		Long:  "Load and validate the config file and environment variables, reporting any warnings or errors.",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := terminal.NewLogger()
			hasIssues := false

			// Load and validate config file
			result, err := config.LoadWithWarnings()
			if err != nil {
				logger.Logf(terminal.StyleError, "Config file error: %v", err)
				return err
			}

			for _, warning := range result.Warnings {
				logger.Logf(terminal.StyleWarning, "Config: %s", warning)
				hasIssues = true
			}

			// Check env vars
			_, envWarnings := config.LoadEnvState()
			for _, warning := range envWarnings {
				logger.Logf(terminal.StyleWarning, "Env: %s", warning)
				hasIssues = true
			}

			if !hasIssues {
				logger.Log("Configuration is valid.", terminal.StyleSuccess)
			}

			return nil
		},
	}
}
