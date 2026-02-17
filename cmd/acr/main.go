// Package main provides the CLI entry point for the agentic code reviewer.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

var (
	reviewers           int
	concurrency         int
	baseRef             string
	timeout             time.Duration
	retries             int
	fetch               bool
	noFetch             bool
	guidance            string
	guidanceFile        string
	verbose             bool
	local               bool
	worktreeBranch      string
	prNumber            string
	autoYes             bool
	excludePatterns     []string
	noConfig            bool
	agentName           string
	summarizerAgentName string
	refFile             bool
	noFPFilter          bool
	fpThreshold         int
	summarizerTimeout   time.Duration
	fpFilterTimeout     time.Duration
	noPRFeedback        bool
	prFeedbackAgent     string
)

func main() {
	os.Exit(run())
}

func run() int {
	rootCmd := &cobra.Command{
		Use:   "acr",
		Short: "Agentic code reviewer - run parallel code reviews",
		Long: `Run parallel LLM-powered code reviews, deduplicate findings, and summarize results.

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

	// Configuration flags (defaults are resolved via config.Resolve with precedence: flag > env > config > default)
	rootCmd.Flags().IntVarP(&reviewers, "reviewers", "r", 0,
		"Number of parallel reviewers (default: 5, env: ACR_REVIEWERS)")
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0,
		"Max concurrent reviewers (default: same as --reviewers, env: ACR_CONCURRENCY)")
	rootCmd.Flags().StringVarP(&baseRef, "base", "b", "",
		"Base ref for review command (default: main, env: ACR_BASE_REF)")
	rootCmd.Flags().DurationVarP(&timeout, "timeout", "t", 0,
		"Timeout per reviewer (default: 10m, env: ACR_TIMEOUT)")
	rootCmd.Flags().IntVarP(&retries, "retries", "R", 0,
		"Retry failed reviewers N times (default: 1, env: ACR_RETRIES)")
	rootCmd.Flags().BoolVar(&fetch, "fetch", true,
		"Fetch latest base ref from origin before diff (default: true, env: ACR_FETCH)")
	rootCmd.Flags().BoolVar(&noFetch, "no-fetch", false,
		"Disable fetching base ref from origin (use local state)")
	rootCmd.Flags().StringVar(&guidance, "guidance", "",
		"Steering context appended to the review prompt (env: ACR_GUIDANCE)")
	rootCmd.Flags().StringVar(&guidanceFile, "guidance-file", "",
		"Path to file containing review guidance (env: ACR_GUIDANCE_FILE)")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"Print agent messages as they arrive")
	rootCmd.Flags().BoolVarP(&local, "local", "l", false,
		"Skip posting findings to a PR")
	rootCmd.Flags().StringVarP(&worktreeBranch, "worktree-branch", "B", "",
		"Review a branch in a temporary worktree")
	rootCmd.Flags().StringVar(&prNumber, "pr", "",
		"Review a PR by number (fetches into temp worktree)")

	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false,
		"Automatically submit review without prompting")

	// Filtering options
	rootCmd.Flags().StringArrayVar(&excludePatterns, "exclude-pattern", nil,
		"Exclude findings matching regex pattern (repeatable)")
	rootCmd.Flags().BoolVar(&noConfig, "no-config", false,
		"Skip loading .acr.yaml config file")
	rootCmd.Flags().StringVarP(&agentName, "reviewer-agent", "a", "codex",
		"Agent(s) for reviews (comma-separated): codex, claude, gemini (env: ACR_REVIEWER_AGENT)")
	rootCmd.Flags().StringVarP(&summarizerAgentName, "summarizer-agent", "s", "codex",
		"Agent to use for summarization: codex, claude, gemini (env: ACR_SUMMARIZER_AGENT)")
	rootCmd.Flags().BoolVar(&refFile, "ref-file", false,
		"Write diff to a temp file instead of embedding in prompt (auto-enabled for large diffs)")
	rootCmd.Flags().BoolVar(&noFPFilter, "no-fp-filter", false,
		"Disable false positive filtering (env: ACR_FP_FILTER=false to disable)")
	rootCmd.Flags().IntVar(&fpThreshold, "fp-threshold", 75,
		"False positive confidence threshold 1-100 (default: 75, env: ACR_FP_THRESHOLD)")
	rootCmd.Flags().DurationVar(&summarizerTimeout, "summarizer-timeout", 0,
		"Timeout for summarizer phase (default: 5m, env: ACR_SUMMARIZER_TIMEOUT)")
	rootCmd.Flags().DurationVar(&fpFilterTimeout, "fp-filter-timeout", 0,
		"Timeout for false positive filter phase (default: 5m, env: ACR_FP_FILTER_TIMEOUT)")
	rootCmd.Flags().BoolVar(&noPRFeedback, "no-pr-feedback", false,
		"Disable reading PR comments for feedback context (env: ACR_PR_FEEDBACK=false)")
	rootCmd.Flags().StringVar(&prFeedbackAgent, "pr-feedback-agent", "",
		"Agent for PR feedback summarization (default: same as --summarizer-agent, env: ACR_PR_FEEDBACK_AGENT)")

	rootCmd.AddCommand(newConfigCmd())

	setGroupedUsage(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		// Check if this is an exit code wrapper (not a real error)
		if exitErr, ok := err.(exitCodeError); ok {
			return exitErr.code.Int()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return domain.ExitError.Int()
	}

	return 0
}

// worktreeResult holds the outputs from worktree setup.
type worktreeResult struct {
	workDir          string
	detectedBase     string // PR base ref detected from GitHub (empty if not auto-detected)
	baseAutoDetected bool
	prRemote         string
	prRepoRoot       string
	cleanup          func() // Call to remove worktree; nil if no worktree created
}

// setupWorktree handles --pr and --worktree-branch modes.
// Returns a worktreeResult with the working directory and cleanup function.
// If neither flag is set, returns zero-value result (no worktree).
func setupWorktree(ctx context.Context, cmd *cobra.Command, logger *terminal.Logger) (worktreeResult, error) {
	var result worktreeResult

	// Validate mutual exclusivity
	if prNumber != "" && worktreeBranch != "" {
		logger.Log("--pr and --worktree-branch are mutually exclusive", terminal.StyleError)
		return result, exitCode(domain.ExitError)
	}

	// Handle PR-based review
	if prNumber != "" {
		if err := github.CheckGHAvailable(); err != nil {
			logger.Logf(terminal.StyleError, "--pr requires gh CLI: %v", err)
			return result, exitCode(domain.ExitError)
		}

		// Early validation: check PR exists and auth is valid
		if err := github.ValidatePR(ctx, prNumber); err != nil {
			if errors.Is(err, github.ErrNoPRFound) {
				logger.Logf(terminal.StyleError, "PR #%s not found", prNumber)
			} else if errors.Is(err, github.ErrAuthFailed) {
				logger.Logf(terminal.StyleError, "GitHub authentication failed. Run 'gh auth login' to authenticate.")
			} else {
				logger.Logf(terminal.StyleError, "Failed to access PR #%s: %v", prNumber, err)
			}
			return result, exitCode(domain.ExitError)
		}

		logger.Logf(terminal.StyleInfo, "Fetching PR %s#%s%s",
			terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))

		// Auto-detect base ref only if not explicitly set via flag OR env var
		// This respects user's intentional base configuration
		explicitBaseSet := cmd.Flags().Changed("base") || os.Getenv("ACR_BASE_REF") != ""
		if !explicitBaseSet {
			if detectedBase, err := github.GetPRBaseRef(ctx, prNumber); err == nil && detectedBase != "" {
				result.detectedBase = detectedBase
				result.baseAutoDetected = true // Ensures config.Resolve won't override it
				logger.Logf(terminal.StyleDim, "Auto-detected base: %s", detectedBase)
			}
		}

		// Get repo root for worktree creation
		repoRoot, err := git.GetRoot()
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}
		result.prRepoRoot = repoRoot

		// Detect the correct remote for PR refs (handles fork workflows)
		remote := github.GetRepoRemote(ctx)
		result.prRemote = remote

		// Create worktree from PR - uses FETCH_HEAD to avoid branch conflicts
		wt, err := git.CreateWorktreeFromPR(repoRoot, remote, prNumber)
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}
		result.cleanup = func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		result.workDir = wt.Path
	} else if worktreeBranch != "" {
		logger.Logf(terminal.StyleInfo, "Creating worktree for %s%s%s",
			terminal.Color(terminal.Bold), worktreeBranch, terminal.Color(terminal.Reset))

		// Check if this is fork notation (username:branch)
		var actualRef string
		var cleanupRemote func()

		forkRef, err := github.ResolveForkRef(ctx, worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}

		if forkRef != nil {
			// Fork flow: add remote, fetch, set ref
			logger.Logf(terminal.StyleInfo, "Resolved fork PR #%d from %s",
				forkRef.PRNumber, forkRef.Username)

			repoRoot, err := git.GetRoot()
			if err != nil {
				logger.Logf(terminal.StyleError, "Error getting repo root: %v", err)
				return result, exitCode(domain.ExitError)
			}

			// Add temporary remote
			if err := git.AddRemote(repoRoot, forkRef.RemoteName, forkRef.RepoURL); err != nil {
				logger.Logf(terminal.StyleError, "Error adding remote: %v", err)
				return result, exitCode(domain.ExitError)
			}
			cleanupRemote = func() {
				_ = git.RemoveRemote(repoRoot, forkRef.RemoteName)
			}

			// Fetch the branch
			logger.Logf(terminal.StyleDim, "Fetching %s from %s", forkRef.Branch, forkRef.RepoURL)
			if err := git.FetchBranch(ctx, repoRoot, forkRef.RemoteName, forkRef.Branch); err != nil {
				cleanupRemote()
				logger.Logf(terminal.StyleError, "Error fetching fork branch: %v", err)
				return result, exitCode(domain.ExitError)
			}

			actualRef = fmt.Sprintf("%s/%s", forkRef.RemoteName, forkRef.Branch)
		} else {
			// Normal branch
			actualRef = worktreeBranch
		}

		wt, err := git.CreateWorktree(actualRef)
		if err != nil {
			if cleanupRemote != nil {
				cleanupRemote()
			}
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}

		// Cleanup remote after worktree is created (worktree has the files, remote no longer needed)
		if cleanupRemote != nil {
			cleanupRemote()
		}

		result.cleanup = func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		result.workDir = wt.Path
	}

	return result, nil
}

// configResult holds the outputs from config loading and resolution.
type configResult struct {
	resolved        config.ResolvedConfig
	excludePatterns []string
}

// loadAndResolveConfig loads the config file, builds flag/env state, resolves
// configuration precedence, qualifies the base ref for PR mode, validates,
// and resolves guidance. It encapsulates all config-related setup.
func loadAndResolveConfig(cmd *cobra.Command, wt worktreeResult, logger *terminal.Logger) (configResult, error) {
	// Load config file (unless --no-config)
	// When using a worktree, load config from the worktree (branch-specific settings)
	var cfg *config.Config
	var configDir string
	if !noConfig {
		var result *config.LoadResult
		var err error
		if wt.workDir != "" {
			result, err = config.LoadFromDirWithWarnings(wt.workDir)
		} else {
			result, err = config.LoadWithWarnings()
		}
		if err != nil {
			logger.Logf(terminal.StyleError, "Config error: %v", err)
			return configResult{}, exitCode(domain.ExitError)
		}
		cfg = result.Config
		configDir = result.ConfigDir
		// Display warnings for unknown keys
		for _, warning := range result.Warnings {
			logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
		}
	}

	// Build flag state from cobra's Changed() method
	// For fetch, either --fetch or --no-fetch being set counts as explicit
	fetchFlagSet := cmd.Flags().Changed("fetch") || cmd.Flags().Changed("no-fetch")
	flagState := config.FlagState{
		ReviewersSet:         cmd.Flags().Changed("reviewers"),
		ConcurrencySet:       cmd.Flags().Changed("concurrency"),
		BaseSet:              cmd.Flags().Changed("base") || wt.baseAutoDetected,
		TimeoutSet:           cmd.Flags().Changed("timeout"),
		RetriesSet:           cmd.Flags().Changed("retries"),
		FetchSet:             fetchFlagSet,
		ReviewerAgentsSet:    cmd.Flags().Changed("reviewer-agent"),
		SummarizerAgentSet:   cmd.Flags().Changed("summarizer-agent"),
		SummarizerTimeoutSet: cmd.Flags().Changed("summarizer-timeout"),
		FPFilterTimeoutSet:   cmd.Flags().Changed("fp-filter-timeout"),
		GuidanceSet:          cmd.Flags().Changed("guidance"),
		GuidanceFileSet:      cmd.Flags().Changed("guidance-file"),
		NoFPFilterSet:        cmd.Flags().Changed("no-fp-filter"),
		FPThresholdSet:       cmd.Flags().Changed("fp-threshold"),
		NoPRFeedbackSet:      cmd.Flags().Changed("no-pr-feedback"),
		PRFeedbackAgentSet:   cmd.Flags().Changed("pr-feedback-agent"),
	}

	// Load env var state
	envState, envWarnings := config.LoadEnvState()
	for _, warning := range envWarnings {
		logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
	}

	// Build flag values struct
	// noFetch exists for shell alias ergonomics where --fetch=false is awkward.
	// Example: alias acr-nofetch='acr --no-fetch'
	// When both flags are set (unlikely), noFetch takes precedence.
	fetchValue := fetch && !noFetch
	// Use auto-detected base ref from PR if available, otherwise use the flag value
	resolvedBaseRef := baseRef
	if wt.detectedBase != "" {
		resolvedBaseRef = wt.detectedBase
	}

	flagValues := config.ResolvedConfig{
		Reviewers:         reviewers,
		Concurrency:       concurrency,
		Base:              resolvedBaseRef,
		Timeout:           timeout,
		Retries:           retries,
		Fetch:             fetchValue,
		ReviewerAgents:    agent.ParseAgentNames(agentName),
		SummarizerAgent:   summarizerAgentName,
		SummarizerTimeout: summarizerTimeout,
		FPFilterTimeout:   fpFilterTimeout,
		Guidance:          guidance,
		GuidanceFile:      guidanceFile,
		FPFilterEnabled:   !noFPFilter,
		FPThreshold:       fpThreshold,
		PRFeedbackEnabled: !noPRFeedback,
		PRFeedbackAgent:   prFeedbackAgent,
	}

	// Resolve final configuration (precedence: flags > env vars > config file > defaults)
	resolved := config.Resolve(cfg, envState, flagState, flagValues)

	// For PR mode: fetch and qualify the base ref so git diff works in the detached worktree
	// Only do this for unqualified branch names - skip for SHAs, tags, HEAD, or already-qualified refs
	// When baseAutoDetected is true, always qualify (PR base refs are always unqualified branches)
	if wt.prRemote != "" && git.ShouldQualifyBaseRef(resolved.Base, wt.baseAutoDetected) {
		// Fetch the base ref from the remote so it exists locally
		if err := git.FetchBaseRef(wt.prRepoRoot, wt.prRemote, resolved.Base); err != nil {
			logger.Logf(terminal.StyleWarning, "Could not fetch base ref: %v", err)
			// Don't qualify - keep original ref so git diff can try it directly
		} else {
			// Only qualify the base ref if fetch succeeded
			resolved.Base = git.QualifyBaseRef(wt.prRemote, resolved.Base)
		}
	}

	// Validate resolved config
	if err := resolved.Validate(); err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return configResult{}, exitCode(domain.ExitError)
	}

	// Default concurrency to reviewers if not specified (0 means same as reviewers)
	if resolved.Concurrency <= 0 {
		resolved.Concurrency = resolved.Reviewers
	}
	if resolved.Concurrency > resolved.Reviewers {
		resolved.Concurrency = resolved.Reviewers
	}

	// Merge exclude patterns (config patterns + CLI patterns)
	allExcludePatterns := config.Merge(cfg, excludePatterns)

	// Resolve guidance (precedence: flags > env vars > config file)
	resolvedGuidance, err := config.ResolveGuidance(cfg, envState, flagState, flagValues, configDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to resolve guidance: %v", err)
		return configResult{}, exitCode(domain.ExitError)
	}
	resolved.Guidance = resolvedGuidance

	return configResult{
		resolved:        resolved,
		excludePatterns: allExcludePatterns,
	}, nil
}

func runReview(cmd *cobra.Command, _ []string) error {
	// Disable colors if stdout is not a TTY
	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}

	logger := terminal.NewLogger()

	// Prune stale ACR worktrees from previous runs (only review-* dirs older than 2h)
	if err := git.PruneStaleWorktrees(); err != nil && verbose {
		logger.Logf(terminal.StyleDim, "Worktree prune: %v", err)
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

	// Set up worktree (--pr or --worktree-branch)
	wt, err := setupWorktree(ctx, cmd, logger)
	if err != nil {
		return err
	}
	if wt.cleanup != nil {
		defer wt.cleanup()
	}

	// Load and resolve configuration
	cfgResult, err := loadAndResolveConfig(cmd, wt, logger)
	if err != nil {
		return err
	}

	// Auto-detect PR for current branch if not explicitly specified and not in local mode
	// This enables PR feedback summarization even without --pr flag
	// Skip auto-detection if PR feedback is disabled since the PR number is only used for feedback
	detectedPR := prNumber
	if detectedPR == "" && !local && cfgResult.resolved.PRFeedbackEnabled && github.IsGHAvailable() {
		if detected, err := github.GetCurrentPRNumber(ctx, worktreeBranch); err == nil {
			detectedPR = detected
			if verbose {
				logger.Logf(terminal.StyleDim, "Auto-detected PR #%s for current branch", detectedPR)
			}
		}
	}

	// Run the review
	opts := ReviewOpts{
		ResolvedConfig:  cfgResult.resolved,
		Verbose:         verbose,
		Local:           local,
		AutoYes:         autoYes,
		PRNumber:        prNumber,
		DetectedPR:      detectedPR,
		WorktreeBranch:  worktreeBranch,
		UseRefFile:      refFile,
		ExcludePatterns: cfgResult.excludePatterns,
		WorkDir:         wt.workDir,
	}
	code := executeReview(ctx, opts, logger)
	return exitCode(code)
}
