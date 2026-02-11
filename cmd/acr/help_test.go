package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestSetGroupedUsage(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("reviewers", 5, "Number of reviewers")
	cmd.Flags().String("base", "main", "Base ref")
	cmd.Flags().String("reviewer-agent", "codex", "Agent for reviews")
	cmd.Flags().String("pr", "", "PR number")
	cmd.Flags().Bool("no-config", false, "Skip config")
	cmd.Flags().Bool("help", false, "help")

	setGroupedUsage(cmd)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Usage()
	if err != nil {
		t.Fatalf("Usage() returned error: %v", err)
	}

	output := buf.String()

	// Check that group headers appear
	for _, header := range []string{"Review Settings:", "Agent Settings:", "PR Integration:", "Advanced:"} {
		if !strings.Contains(output, header) {
			t.Errorf("expected group header %q in output, got:\n%s", header, output)
		}
	}

	// Check that flags appear under correct groups
	reviewIdx := strings.Index(output, "Review Settings:")
	agentIdx := strings.Index(output, "Agent Settings:")
	reviewersIdx := strings.Index(output, "--reviewers")
	agentFlagIdx := strings.Index(output, "--reviewer-agent")

	if reviewersIdx < reviewIdx || reviewersIdx > agentIdx {
		t.Error("expected --reviewers under Review Settings")
	}
	if agentFlagIdx < agentIdx {
		t.Error("expected --reviewer-agent under Agent Settings")
	}

	// Ungrouped flags go to Other Flags
	if !strings.Contains(output, "Other Flags:") {
		t.Errorf("expected 'Other Flags:' section for ungrouped flags, got:\n%s", output)
	}
	otherIdx := strings.Index(output, "Other Flags:")
	helpIdx := strings.Index(output, "--help")
	if helpIdx < otherIdx {
		t.Error("expected --help under Other Flags")
	}
}

func TestSetGroupedUsage_EmptyGroupsOmitted(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	// Only add a flag from one group
	cmd.Flags().Int("reviewers", 5, "Number of reviewers")

	setGroupedUsage(cmd)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	_ = cmd.Usage()
	output := buf.String()

	// Groups with no matching flags should not appear
	if strings.Contains(output, "Filtering:") {
		t.Error("Filtering group should be omitted when no filtering flags are defined")
	}
}

func TestFlagGroupsCoverAllFlags(t *testing.T) {
	// Verify that all non-help/version flags in the real command are accounted for
	// in flagGroups. This catches new flags that haven't been categorized.
	grouped := make(map[string]bool)
	for _, g := range flagGroups {
		for _, name := range g.flags {
			grouped[name] = true
		}
	}

	// These are expected to be ungrouped (they go in "Other Flags")
	exempt := map[string]bool{
		"help":    true,
		"version": true,
	}

	// Build the real command's flag set
	cmd := &cobra.Command{Use: "acr"}
	cmd.Flags().IntVarP(&reviewers, "reviewers", "r", 0, "")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "")
	cmd.Flags().StringVarP(&baseRef, "base", "b", "", "")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 0, "")
	cmd.Flags().IntVarP(&retries, "retries", "R", 0, "")
	cmd.Flags().BoolVar(&fetch, "fetch", true, "")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "")
	cmd.Flags().StringVar(&guidance, "guidance", "", "")
	cmd.Flags().StringVar(&guidanceFile, "guidance-file", "", "")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "")
	cmd.Flags().BoolVarP(&local, "local", "l", false, "")
	cmd.Flags().StringVarP(&worktreeBranch, "worktree-branch", "B", "", "")
	cmd.Flags().StringVar(&prNumber, "pr", "", "")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "")
	cmd.Flags().StringArrayVar(&excludePatterns, "exclude-pattern", nil, "")
	cmd.Flags().BoolVar(&noConfig, "no-config", false, "")
	cmd.Flags().StringVarP(&agentName, "reviewer-agent", "a", "codex", "")
	cmd.Flags().StringVarP(&summarizerAgentName, "summarizer-agent", "s", "codex", "")
	cmd.Flags().BoolVar(&refFile, "ref-file", false, "")
	cmd.Flags().BoolVar(&noFPFilter, "no-fp-filter", false, "")
	cmd.Flags().IntVar(&fpThreshold, "fp-threshold", 75, "")
	cmd.Flags().BoolVar(&noPRFeedback, "no-pr-feedback", false, "")
	cmd.Flags().StringVar(&prFeedbackAgent, "pr-feedback-agent", "", "")

	var uncategorized []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if !grouped[f.Name] && !exempt[f.Name] {
			uncategorized = append(uncategorized, f.Name)
		}
	})

	if len(uncategorized) > 0 {
		t.Errorf("flags not assigned to any group in flagGroups: %v\nAdd them to a group in help.go", uncategorized)
	}
}
