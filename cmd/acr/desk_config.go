package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func newDeskConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the persistent review workspace configuration",
		Long:  "Initialize, display, and validate workspace-level identity, scope, and posting policy configuration.",
	}

	cmd.AddCommand(newDeskConfigInitCmd())
	cmd.AddCommand(newDeskConfigShowCmd())
	cmd.AddCommand(newDeskConfigValidateCmd())

	return cmd
}

func newDeskConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a starter workspace configuration file",
		Long:  "Create a workspace.yaml file in the OS-appropriate configuration directory (override with ACR_CONFIG_DIR). Posting and own-PR review are disabled by default.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := workspace.ConfigDir()
			if err != nil {
				return err
			}

			defaultUser := github.GetCurrentUser(context.Background())
			path, err := workspace.Init(configDir, defaultUser)
			if err != nil {
				return err
			}

			fmt.Printf("Created %s with default settings.\n", path)
			if defaultUser == "" {
				fmt.Println("Could not determine the authenticated GitHub user; set identity.expected_user before using the desk.")
			}
			return nil
		},
	}
}

func newDeskConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display the resolved workspace configuration",
		Long:  "Show the workspace-level identity, scope, behavior, posting, and notification configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := workspace.ConfigDir()
			if err != nil {
				return err
			}
			cfg, err := workspace.Load(configDir)
			if err != nil {
				return err
			}

			fmt.Println("Resolved workspace configuration:")
			fmt.Println()
			fmt.Printf("  %-24s %s\n", "identity.expected_user:", orNone(cfg.Identity.ExpectedUser))
			fmt.Printf("  %-24s %s\n", "scope.organizations:", strings.Join(cfg.Scope.Organizations, ", "))
			fmt.Printf("  %-24s %s\n", "scope.teams:", strings.Join(cfg.Scope.Teams, ", "))
			fmt.Printf("  %-24s %s\n", "scope.repository_roots:", strings.Join(cfg.Scope.RepositoryRoots, ", "))
			fmt.Printf("  %-24s %s\n", "scope.include:", strings.Join(cfg.Scope.Include, ", "))
			fmt.Printf("  %-24s %s\n", "scope.exclude:", strings.Join(cfg.Scope.Exclude, ", "))
			fmt.Printf("  %-24s %d\n", "scope.path_overrides:", len(cfg.Scope.PathOverrides))
			fmt.Printf("  %-24s %s\n", "behavior.poll_interval:", cfg.Behavior.PollInterval.AsDuration())
			fmt.Printf("  %-24s %s\n", "behavior.settle_time:", cfg.Behavior.SettleTime.AsDuration())
			fmt.Printf("  %-24s %d\n", "behavior.concurrency:", cfg.Behavior.Concurrency)
			fmt.Printf("  %-24s %t\n", "behavior.auto_review:", cfg.Behavior.AutoReview)
			fmt.Printf("  %-24s %t\n", "behavior.re_review:", cfg.Behavior.ReReview)
			fmt.Printf("  %-24s %s\n", "behavior.own_pr_policy:", cfg.Behavior.OwnPRPolicy)
			fmt.Printf("  %-24s %t\n", "posting.enabled:", cfg.Posting.Enabled)
			fmt.Printf("  %-24s %t\n", "notifications.enabled:", cfg.Notifications.Enabled)

			return nil
		},
	}
}

func newDeskConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate workspace configuration and GitHub identity",
		Long:  "Load and validate the workspace configuration, reporting any errors, and confirm the authenticated GitHub user matches the configured identity.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !terminal.IsStdoutTTY() {
				terminal.DisableColors()
			}
			logger := terminal.NewLogger()

			configDir, err := workspace.ConfigDir()
			if err != nil {
				return err
			}
			cfg, err := workspace.Load(configDir)
			if err != nil {
				return err
			}

			var errs []string
			errs = append(errs, cfg.Validate()...)

			if identityErr := workspace.CheckIdentity(context.Background(), *cfg); identityErr != nil {
				errs = append(errs, identityErr.Error())
			}

			for _, e := range errs {
				logger.Logf(terminal.StyleError, "%s", e)
			}

			if len(errs) > 0 {
				return fmt.Errorf("workspace configuration has %d error(s)", len(errs))
			}

			logger.Log("Workspace configuration is valid.", terminal.StyleSuccess)
			return nil
		},
	}
}
