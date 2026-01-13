// Package main provides the CLI entry point for the agentic code reviewer.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/anthropics/agentic-code-reviewer/internal/config"
	"github.com/anthropics/agentic-code-reviewer/internal/domain"
	"github.com/anthropics/agentic-code-reviewer/internal/filter"
	"github.com/anthropics/agentic-code-reviewer/internal/git"
	"github.com/anthropics/agentic-code-reviewer/internal/github"
	"github.com/anthropics/agentic-code-reviewer/internal/runner"
	"github.com/anthropics/agentic-code-reviewer/internal/summarizer"
	"github.com/anthropics/agentic-code-reviewer/internal/terminal"
)

// Version information - injected via ldflags at build time
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	reviewers       int
	baseRef         string
	timeout         time.Duration
	retries         int
	verbose         bool
	local           bool
	worktreeBranch  string
	autoYes         bool
	autoNo          bool
	excludePatterns []string
	noConfig        bool
)

func main() {
	os.Exit(run())
}

func run() int {
	rootCmd := &cobra.Command{
		Use:   "acr",
		Short: "Agentic code reviewer - run parallel code reviews",
		Long: `Run codex review in parallel, parse JSONL output, and summarize findings.

Exit codes:
  0 - No findings
  1 - Findings found
  2 - Error
  130 - Interrupted`,
		RunE:          runReview,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       buildVersionString(),
	}

	rootCmd.SetVersionTemplate("{{.Version}}\n")

	// Configuration flags
	rootCmd.Flags().IntVarP(&reviewers, "reviewers", "r", getEnvInt("REVIEW_REVIEWERS", getEnvInt("REVIEW_WORKERS", 5)),
		"Number of parallel reviewers to run")
	rootCmd.Flags().StringVarP(&baseRef, "base", "b", getEnvStr("REVIEW_BASE_REF", "main"),
		"Base ref for review command")
	rootCmd.Flags().DurationVarP(&timeout, "timeout", "t", getEnvDuration("REVIEW_TIMEOUT", 5*time.Minute),
		"Timeout per reviewer (e.g., 5m, 300s)")
	rootCmd.Flags().IntVarP(&retries, "retries", "R", getEnvInt("REVIEW_RETRIES", 1),
		"Retry failed reviewers N times")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"Print agent messages as they arrive")
	rootCmd.Flags().BoolVarP(&local, "local", "l", false,
		"Skip posting findings to a PR")
	rootCmd.Flags().StringVarP(&worktreeBranch, "worktree-branch", "B", "",
		"Review a branch in a temporary worktree")

	// Mutually exclusive submit options
	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false,
		"Automatically submit review without prompting")
	rootCmd.Flags().BoolVarP(&autoNo, "no", "n", false,
		"Automatically skip submitting review")
	rootCmd.MarkFlagsMutuallyExclusive("yes", "no")

	// Filtering options
	rootCmd.Flags().StringArrayVar(&excludePatterns, "exclude-pattern",
		getEnvStringSlice("REVIEW_EXCLUDE_PATTERNS", nil),
		"Exclude findings matching regex pattern (repeatable)")
	rootCmd.Flags().BoolVar(&noConfig, "no-config", false,
		"Skip loading .acr.yaml config file")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return domain.ExitError.Int()
	}

	return 0
}

func runReview(cmd *cobra.Command, args []string) error {
	// Disable colors if stdout is not a TTY
	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}

	logger := terminal.NewLogger()

	// Validate inputs
	if reviewers < 1 {
		logger.Log("--reviewers must be >= 1", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	// Check dependencies
	if _, err := exec.LookPath("codex"); err != nil {
		logger.Log("'codex' not found in PATH", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr)
		logger.Log("Interrupted, shutting down...", terminal.StyleWarning)
		cancel()
	}()

	// Handle worktree-based review
	var workDir string
	if worktreeBranch != "" {
		logger.Logf(terminal.StyleInfo, "Creating worktree for %s%s%s",
			terminal.Color(terminal.Bold), worktreeBranch, terminal.Color(terminal.Reset))

		wt, err := git.CreateWorktree(worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}
		defer func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}()

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		workDir = wt.Path
	}

	// Load config and merge exclude patterns
	// When using a worktree, load config from the worktree (branch-specific settings)
	allExcludePatterns := excludePatterns
	if !noConfig {
		var cfg *config.Config
		var err error
		if workDir != "" {
			cfg, err = config.LoadFromDir(workDir)
		} else {
			cfg, err = config.Load()
		}
		if err != nil {
			logger.Logf(terminal.StyleError, "Config error: %v", err)
			return exitCode(domain.ExitError)
		}
		allExcludePatterns = config.Merge(cfg, excludePatterns)
	}

	// Run the review
	code := executeReview(ctx, workDir, allExcludePatterns, logger)
	return exitCode(code)
}

func executeReview(ctx context.Context, workDir string, excludePatterns []string, logger *terminal.Logger) domain.ExitCode {
	logger.Logf(terminal.StyleInfo, "Starting review %s(%d reviewers, base=%s)%s",
		terminal.Color(terminal.Dim), reviewers, baseRef, terminal.Color(terminal.Reset))

	if verbose {
		logger.Logf(terminal.StyleDim, "%sCommand: codex exec --json --color never review --base %s%s",
			terminal.Color(terminal.Dim), baseRef, terminal.Color(terminal.Reset))
	}

	// Run reviewers
	r := runner.New(runner.Config{
		Reviewers: reviewers,
		BaseRef:   baseRef,
		Timeout:   timeout,
		Retries:   retries,
		Verbose:   verbose,
		WorkDir:   workDir,
	}, logger)

	results, wallClock, err := r.Run(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ExitInterrupted
		}
		logger.Logf(terminal.StyleError, "Review failed: %v", err)
		return domain.ExitError
	}

	// Build statistics
	stats := runner.BuildStats(results, reviewers, wallClock)

	// Check if all reviewers failed
	if stats.AllFailed() {
		logger.Log("All reviewers failed", terminal.StyleError)
		return domain.ExitError
	}

	// Aggregate and summarize findings
	allFindings := runner.CollectFindings(results)
	aggregated := domain.AggregateFindings(allFindings)

	// Run summarizer with spinner
	phaseSpinner := terminal.NewPhaseSpinner("Summarizing")
	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		phaseSpinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	summaryResult, err := summarizer.Summarize(ctx, aggregated)
	spinnerCancel()
	<-spinnerDone

	if err != nil {
		logger.Logf(terminal.StyleError, "Summarizer error: %v", err)
		return domain.ExitError
	}

	stats.SummarizerDuration = summaryResult.Duration

	// Apply exclude patterns if configured
	if len(excludePatterns) > 0 {
		f, err := filter.New(excludePatterns)
		if err != nil {
			logger.Logf(terminal.StyleError, "Invalid exclude pattern: %v", err)
			return domain.ExitError
		}
		summaryResult.Grouped = f.Apply(summaryResult.Grouped)
	}

	// Render and print report
	report := runner.RenderReport(summaryResult.Grouped, summaryResult, stats)
	fmt.Println(report)

	if summaryResult.ExitCode != 0 {
		return domain.ExitError
	}

	// Handle PR actions
	if !summaryResult.Grouped.HasFindings() {
		return handleLGTM(ctx, allFindings, stats, logger)
	}

	return handleFindings(ctx, summaryResult.Grouped, aggregated, stats, logger)
}

func handleLGTM(ctx context.Context, allFindings []domain.Finding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	// Build reviewer comments
	reviewerComments := make(map[int]string)
	for _, f := range allFindings {
		reviewerComments[f.ReviewerID] = f.Text
	}

	lgtmBody := runner.RenderLGTMMarkdown(stats.TotalReviewers, stats.SuccessfulReviewers, reviewerComments)

	// Check CI status before approving
	if !local && !autoNo {
		if !github.IsGHAvailable() {
			return domain.ExitError
		}

		prNumber := github.GetCurrentPRNumber(worktreeBranch)
		if prNumber != "" {
			ciStatus := github.CheckCIStatus(prNumber)

			if ciStatus.Error != "" {
				logger.Logf(terminal.StyleError, "Failed to check CI status: %s", ciStatus.Error)
				return domain.ExitError
			}

			if !ciStatus.AllPassed {
				logger.Logf(terminal.StyleSuccess, "%s%sLGTM%s - No issues found by reviewers.",
					terminal.Color(terminal.Green), terminal.Color(terminal.Bold), terminal.Color(terminal.Reset))
				fmt.Println()

				if len(ciStatus.Failed) > 0 {
					logger.Logf(terminal.StyleError, "Cannot approve PR: %d CI check(s) failed", len(ciStatus.Failed))
					for i, check := range ciStatus.Failed {
						if i >= 5 {
							logger.Logf(terminal.StyleDim, "  ... and %d more", len(ciStatus.Failed)-5)
							break
						}
						logger.Logf(terminal.StyleDim, "  • %s", check)
					}
				}
				if len(ciStatus.Pending) > 0 {
					logger.Logf(terminal.StyleWarning, "Cannot approve PR: %d CI check(s) pending", len(ciStatus.Pending))
					for i, check := range ciStatus.Pending {
						if i >= 5 {
							logger.Logf(terminal.StyleDim, "  ... and %d more", len(ciStatus.Pending)-5)
							break
						}
						logger.Logf(terminal.StyleDim, "  • %s", check)
					}
				}

				return domain.ExitNoFindings
			}
		}
	}

	// Preview and confirm
	if err := confirmAndExecutePRAction(ctx, prAction{
		body:            lgtmBody,
		previewLabel:    "Approval comment preview",
		promptTemplate:  "Approve PR #%s?",
		successTemplate: "Approved PR #%s.",
		skipMessage:     "Skipped approving PR.",
		execute:         github.ApprovePR,
	}, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitNoFindings
}

func handleFindings(ctx context.Context, grouped domain.GroupedFindings, aggregated []domain.AggregatedFinding, stats domain.ReviewStats, logger *terminal.Logger) domain.ExitCode {
	selectedFindings := grouped.Findings

	// Interactive selection when in TTY and not auto-submitting
	if !autoYes && !autoNo && terminal.IsStdoutTTY() {
		indices, canceled, err := terminal.RunSelector(grouped.Findings)
		if err != nil {
			logger.Logf(terminal.StyleError, "Selector error: %v", err)
			return domain.ExitError
		}
		if canceled {
			logger.Log("Skipped posting findings.", terminal.StyleDim)
			return domain.ExitFindings
		}
		selectedFindings = filterFindingsByIndices(grouped.Findings, indices)

		if len(selectedFindings) == 0 {
			logger.Log("No findings selected to post.", terminal.StyleDim)
			return domain.ExitFindings
		}
	}

	// Create filtered GroupedFindings for rendering
	filteredGrouped := domain.GroupedFindings{
		Findings: selectedFindings,
		Info:     grouped.Info,
	}

	commentBody := runner.RenderCommentMarkdown(filteredGrouped, stats.TotalReviewers, aggregated)

	if err := confirmAndExecutePRAction(ctx, prAction{
		body:            commentBody,
		previewLabel:    "PR comment preview",
		promptTemplate:  "Post findings to PR #%s?",
		successTemplate: "Posted findings to PR #%s.",
		skipMessage:     "Skipped posting findings.",
		execute:         github.PostPRComment,
	}, logger); err != nil {
		return domain.ExitError
	}

	return domain.ExitFindings
}

type prAction struct {
	body            string
	previewLabel    string
	promptTemplate  string
	successTemplate string
	skipMessage     string
	execute         func(string, string) error
}

func confirmAndExecutePRAction(ctx context.Context, action prAction, logger *terminal.Logger) error {
	if local {
		logger.Log("Local mode enabled; skipping PR action.", terminal.StyleDim)
		return nil
	}

	if autoNo {
		logger.Log(action.skipMessage, terminal.StyleDim)
		return nil
	}

	// Preview
	fmt.Println()
	logger.Logf(terminal.StylePhase, "%s%s%s",
		terminal.Color(terminal.Bold), action.previewLabel, terminal.Color(terminal.Reset))
	fmt.Println()

	width := terminal.ReportWidth()
	divider := terminal.Ruler(width, "━")
	fmt.Println(divider)
	fmt.Println(action.body)
	fmt.Println(divider)

	if !github.IsGHAvailable() {
		return fmt.Errorf("gh not available")
	}

	prNumber := github.GetCurrentPRNumber(worktreeBranch)
	if prNumber == "" {
		branchDesc := "current branch"
		if worktreeBranch != "" {
			branchDesc = fmt.Sprintf("branch '%s'", worktreeBranch)
		}
		logger.Logf(terminal.StyleWarning, "No open PR found for %s.", branchDesc)
		return nil
	}

	// Confirm
	confirmed := autoYes
	if !autoYes {
		fmt.Println()
		prompt := fmt.Sprintf(action.promptTemplate,
			fmt.Sprintf("%s#%s%s", terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset)))
		fmt.Printf("%s?%s %s %s[y/N]:%s ",
			terminal.Color(terminal.Cyan), terminal.Color(terminal.Reset),
			prompt,
			terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSuffix(response, "\n")
		confirmed = response == "y" || response == "yes"
	}

	if !confirmed {
		logger.Log(action.skipMessage, terminal.StyleDim)
		return nil
	}

	// Execute
	if err := action.execute(prNumber, action.body); err != nil {
		logger.Logf(terminal.StyleError, "Failed: %v", err)
		return err
	}

	logger.Log(fmt.Sprintf(action.successTemplate, "#"+prNumber), terminal.StyleSuccess)
	return nil
}

// filterFindingsByIndices returns findings at the specified indices.
func filterFindingsByIndices(findings []domain.FindingGroup, indices []int) []domain.FindingGroup {
	indexSet := make(map[int]bool, len(indices))
	for _, i := range indices {
		indexSet[i] = true
	}

	result := make([]domain.FindingGroup, 0, len(indices))
	for i, f := range findings {
		if indexSet[i] {
			result = append(result, f)
		}
	}
	return result
}

// exitCode is a wrapper type for returning exit codes via error interface.
type exitCodeError struct {
	code domain.ExitCode
}

func (e exitCodeError) Error() string {
	return ""
}

func exitCode(code domain.ExitCode) error {
	if code == domain.ExitNoFindings {
		return nil
	}
	return exitCodeError{code: code}
}

// Helper functions for environment variables

func getEnvStr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		// Try parsing as duration first
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// Fall back to parsing as seconds (integer)
		var secs int
		if _, err := fmt.Sscanf(v, "%d", &secs); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultVal
}

func getEnvStringSlice(key string, defaultVal []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultVal
}

// buildVersionString formats version information for display.
func buildVersionString() string {
	ver, rev, buildDate := getVersionInfo()
	return fmt.Sprintf("acr %s (commit: %s, built: %s)", ver, rev, buildDate)
}

// getVersionInfo returns version information, falling back to debug.ReadBuildInfo()
// for binaries installed via `go install`.
func getVersionInfo() (ver, rev, buildDate string) {
	ver, rev, buildDate = version, commit, date

	// If version is still "dev", try to get info from build info (go install case)
	if ver == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				ver = info.Main.Version
			}
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if len(setting.Value) >= 7 {
						rev = setting.Value[:7]
					} else if setting.Value != "" {
						rev = setting.Value
					}
				case "vcs.time":
					if setting.Value != "" {
						buildDate = setting.Value
					}
				case "vcs.modified":
					if setting.Value == "true" && rev != "none" {
						rev += "-dirty"
					}
				}
			}
		}
	}

	return ver, rev, buildDate
}
