package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagGroup defines a named group of flags for help output.
type flagGroup struct {
	title string
	flags []string
}

// flagGroups defines the logical groupings for CLI flags.
// Flags not listed here appear under "Other Flags".
var flagGroups = []flagGroup{
	{
		title: "Review Settings",
		flags: []string{"reviewers", "concurrency", "base", "timeout", "retries", "verbose"},
	},
	{
		title: "Agent Settings",
		flags: []string{"reviewer-agent", "summarizer-agent", "pr-feedback-agent"},
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
		title: "Advanced",
		flags: []string{"fetch", "no-fetch", "no-config"},
	},
}

// setGroupedUsage configures the command to display flags in logical groups.
func setGroupedUsage(cmd *cobra.Command) {
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		fmt.Fprintf(c.OutOrStderr(), "Usage:\n  %s\n", c.UseLine())

		// Track which flags have been placed in a group
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

		// Collect ungrouped flags (help, version, any new flags not yet categorized)
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
