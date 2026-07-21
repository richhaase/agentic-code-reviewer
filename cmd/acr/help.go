package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type flagGroup struct {
	title string
	flags []string
}

var flagGroups = []flagGroup{
	{
		title: "Review Settings",
		flags: []string{"reviewers", "concurrency", "base", "timeout", "retries", "verbose"},
	},
	{
		title: "Agent Settings",
		flags: []string{"reviewer-agent", "summarizer-agent", "reviewer-model", "summarizer-model", "pr-feedback-agent"},
	},
	{
		title: "PR Integration",
		flags: []string{"pr", "yes", "local", "no-pr-feedback", "worktree-branch"},
	},
	{
		title: "Filtering",
		flags: []string{"exclude-pattern", "no-fp-filter", "fp-threshold"},
	},
	{
		title: "Guidance",
		flags: []string{"guidance", "guidance-file", "ref-file"},
	},
	{
		title: "Watch",
		flags: []string{"post-mode", "poll-interval", "settle-time", "max-reviews", "max-duration"},
	},
	{
		title: "Advanced",
		flags: []string{"fetch", "no-fetch", "no-config"},
	},
}

func setGroupedUsage(cmd *cobra.Command) {
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		if c.HasAvailableSubCommands() {
			fmt.Fprintf(c.OutOrStderr(), "Usage:\n  %s [command]\n  %s [flags]\n", c.CommandPath(), c.CommandPath())
			fmt.Fprintf(c.OutOrStderr(), "\nAvailable Commands:\n")
			for _, sub := range c.Commands() {
				if !sub.Hidden && sub.Name() != "help" {
					fmt.Fprintf(c.OutOrStderr(), "  %-16s %s\n", sub.Name(), sub.Short)
				}
			}
		} else {
			fmt.Fprintf(c.OutOrStderr(), "Usage:\n  %s\n", c.UseLine())
		}

		grouped := make(map[string]bool)

		for _, group := range flagGroups {
			fs := pflag.NewFlagSet(group.title, pflag.ContinueOnError)
			for _, name := range group.flags {
				if f := c.Flags().Lookup(name); f != nil {
					fs.AddFlag(f)
					grouped[name] = true
				}
			}
			if usages := fs.FlagUsages(); strings.TrimSpace(usages) != "" {
				fmt.Fprintf(c.OutOrStderr(), "\n%s:\n%s", group.title, usages)
			}
		}

		other := pflag.NewFlagSet("other", pflag.ContinueOnError)
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if !grouped[f.Name] {
				other.AddFlag(f)
			}
		})
		if usages := other.FlagUsages(); strings.TrimSpace(usages) != "" {
			fmt.Fprintf(c.OutOrStderr(), "\nOther Flags:\n%s", usages)
		}

		return nil
	})
}
