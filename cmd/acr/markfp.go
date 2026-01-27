package main

import (
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/fpcache"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func newMarkFPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-fp",
		Short: "Mark findings as false positives",
		Long: `Launch an interactive picker to mark findings as false positives.
Reads findings from .acr/last-run.json and saves selections to .acr/ignore.

Requires a previous review run to have generated the last-run file.`,
		RunE:          runMarkFP,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return cmd
}

func runMarkFP(_ *cobra.Command, _ []string) error {
	logger := terminal.NewLogger()

	// Determine paths
	lastRunPath := filepath.Join(".acr", "last-run.json")
	ignorePath := filepath.Join(".acr", "ignore")

	// Load last-run findings
	logger.Log("Loading findings from last review run", terminal.StyleInfo)
	lastRun, err := fpcache.LoadLastRun(lastRunPath)
	if err != nil {
		logger.Logf(terminal.StyleError, "Error: %v", err)
		logger.Log("Hint: Run 'acr' first to generate findings", terminal.StyleDim)
		return exitCode(domain.ExitError)
	}

	if len(lastRun.Findings) == 0 {
		logger.Log("No findings from last run", terminal.StyleInfo)
		return nil
	}

	logger.Logf(terminal.StyleInfo, "Found %d findings from last run", len(lastRun.Findings))

	// Load existing ignore patterns
	existingPatterns, err := fpcache.LoadIgnoreFile(ignorePath)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to load ignore file: %v", err)
		return exitCode(domain.ExitError)
	}

	// Launch picker
	selected, err := fpcache.RunPicker(lastRun.Findings, existingPatterns)
	if err != nil {
		logger.Logf(terminal.StyleError, "Picker error: %v", err)
		return exitCode(domain.ExitError)
	}

	// Check if user canceled
	if selected == nil {
		logger.Log("Canceled", terminal.StyleWarning)
		return nil
	}

	// Merge selected titles with existing patterns (deduplicating)
	allPatterns := append([]string{}, existingPatterns...)
	newCount := 0
	for _, title := range selected {
		if !slices.Contains(allPatterns, title) {
			allPatterns = append(allPatterns, title)
			newCount++
		}
	}

	// Save updated ignore file
	if err := fpcache.SaveIgnoreFile(ignorePath, allPatterns); err != nil {
		logger.Logf(terminal.StyleError, "Failed to save ignore file: %v", err)
		return exitCode(domain.ExitError)
	}

	// Print confirmation
	switch newCount {
	case 0:
		logger.Log("No new findings added (all already in .acr/ignore)", terminal.StyleInfo)
	case 1:
		logger.Log("Added 1 finding to .acr/ignore", terminal.StyleSuccess)
	default:
		logger.Logf(terminal.StyleSuccess, "Added %d findings to .acr/ignore", newCount)
	}

	return nil
}
