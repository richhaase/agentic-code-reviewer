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
	rootCmd.Flags().BoolVar(&noPRFeedback, "no-pr-feedback", false,
		"Disable reading PR comments for feedback context (env: ACR_PR_FEEDBACK=false)")
	rootCmd.Flags().StringVar(&prFeedbackAgent, "pr-feedback-agent", "",
		"Agent for PR feedback summarization (default: same as --summarizer-agent, env: ACR_PR_FEEDBACK_AGENT)")

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

func runReview(cmd *cobra.Command, _ []string) error {
	// Disable colors if stdout is not a TTY
	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}

	logger := terminal.NewLogger()

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
	var baseAutoDetected bool // Track if base was auto-detected from PR
	var prRemote string       // Track remote for PR mode (used to qualify base ref)
	var prRepoRoot string     // Track repo root for PR mode (used to fetch base ref)

	// Validate mutual exclusivity
	if prNumber != "" && worktreeBranch != "" {
		logger.Log("--pr and --worktree-branch are mutually exclusive", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	// Handle PR-based review
	if prNumber != "" {
		if err := github.CheckGHAvailable(); err != nil {
			logger.Logf(terminal.StyleError, "--pr requires gh CLI: %v", err)
			return exitCode(domain.ExitError)
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
			return exitCode(domain.ExitError)
		}

		logger.Logf(terminal.StyleInfo, "Fetching PR %s#%s%s",
			terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))

		// Auto-detect base ref only if not explicitly set via flag OR env var
		// This respects user's intentional base configuration
		explicitBaseSet := cmd.Flags().Changed("base") || os.Getenv("ACR_BASE_REF") != ""
		if !explicitBaseSet {
			if detectedBase, err := github.GetPRBaseRef(ctx, prNumber); err == nil && detectedBase != "" {
				baseRef = detectedBase
				baseAutoDetected = true // Ensures config.Resolve won't override it
				logger.Logf(terminal.StyleDim, "Auto-detected base: %s", baseRef)
			}
		}

		// Get repo root for worktree creation
		repoRoot, err := git.GetRoot()
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return exitCode(domain.ExitError)
		}
		prRepoRoot = repoRoot

		// Detect the correct remote for PR refs (handles fork workflows)
		remote := github.GetRepoRemote(ctx)
		prRemote = remote

		// Create worktree from PR - uses FETCH_HEAD to avoid branch conflicts
		wt, err := git.CreateWorktreeFromPR(repoRoot, remote, prNumber)
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return exitCode(domain.ExitError)
		}
		defer func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}()

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		workDir = wt.Path
	} else if worktreeBranch != "" {
		logger.Logf(terminal.StyleInfo, "Creating worktree for %s%s%s",
			terminal.Color(terminal.Bold), worktreeBranch, terminal.Color(terminal.Reset))

		// Check if this is fork notation (username:branch)
		var actualRef string
		var cleanupRemote func()

		forkRef, err := github.ResolveForkRef(ctx, worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return exitCode(domain.ExitError)
		}

		if forkRef != nil {
			// Fork flow: add remote, fetch, set ref
			logger.Logf(terminal.StyleInfo, "Resolved fork PR #%d from %s",
				forkRef.PRNumber, forkRef.Username)

			repoRoot, err := git.GetRoot()
			if err != nil {
				logger.Logf(terminal.StyleError, "Error getting repo root: %v", err)
				return exitCode(domain.ExitError)
			}

			// Add temporary remote
			if err := git.AddRemote(repoRoot, forkRef.RemoteName, forkRef.RepoURL); err != nil {
				logger.Logf(terminal.StyleError, "Error adding remote: %v", err)
				return exitCode(domain.ExitError)
			}
			cleanupRemote = func() {
				_ = git.RemoveRemote(repoRoot, forkRef.RemoteName)
			}

			// Fetch the branch
			logger.Logf(terminal.StyleDim, "Fetching %s from %s", forkRef.Branch, forkRef.RepoURL)
			if err := git.FetchBranch(ctx, repoRoot, forkRef.RemoteName, forkRef.Branch); err != nil {
				cleanupRemote()
				logger.Logf(terminal.StyleError, "Error fetching fork branch: %v", err)
				return exitCode(domain.ExitError)
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
			return exitCode(domain.ExitError)
		}

		// Cleanup remote after worktree is created (worktree has the files, remote no longer needed)
		if cleanupRemote != nil {
			cleanupRemote()
		}

		defer func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}()

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		workDir = wt.Path
	}

	// Load config file (unless --no-config)
	// When using a worktree, load config from the worktree (branch-specific settings)
	var cfg *config.Config
	var configDir string
	if !noConfig {
		var result *config.LoadResult
		var err error
		if workDir != "" {
			result, err = config.LoadFromDirWithWarnings(workDir)
		} else {
			result, err = config.LoadWithWarnings()
		}
		if err != nil {
			logger.Logf(terminal.StyleError, "Config error: %v", err)
			return exitCode(domain.ExitError)
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
		ReviewersSet:       cmd.Flags().Changed("reviewers"),
		ConcurrencySet:     cmd.Flags().Changed("concurrency"),
		BaseSet:            cmd.Flags().Changed("base") || baseAutoDetected,
		TimeoutSet:         cmd.Flags().Changed("timeout"),
		RetriesSet:         cmd.Flags().Changed("retries"),
		FetchSet:           fetchFlagSet,
		ReviewerAgentsSet:  cmd.Flags().Changed("reviewer-agent"),
		SummarizerAgentSet: cmd.Flags().Changed("summarizer-agent"),
		GuidanceSet:        cmd.Flags().Changed("guidance"),
		GuidanceFileSet:    cmd.Flags().Changed("guidance-file"),
		NoFPFilterSet:      cmd.Flags().Changed("no-fp-filter"),
		FPThresholdSet:     cmd.Flags().Changed("fp-threshold"),
		NoPRFeedbackSet:    cmd.Flags().Changed("no-pr-feedback"),
		PRFeedbackAgentSet: cmd.Flags().Changed("pr-feedback-agent"),
	}

	// Load env var state
	envState := config.LoadEnvState()

	// Build flag values struct
	// noFetch exists for shell alias ergonomics where --fetch=false is awkward.
	// Example: alias acr-nofetch='acr --no-fetch'
	// When both flags are set (unlikely), noFetch takes precedence.
	fetchValue := fetch && !noFetch
	flagValues := config.ResolvedConfig{
		Reviewers:         reviewers,
		Concurrency:       concurrency,
		Base:              baseRef,
		Timeout:           timeout,
		Retries:           retries,
		Fetch:             fetchValue,
		ReviewerAgents:    agent.ParseAgentNames(agentName),
		SummarizerAgent:   summarizerAgentName,
		Guidance:          guidance,
		GuidanceFile:      guidanceFile,
		FPFilterEnabled:   !noFPFilter,
		FPThreshold:       fpThreshold,
		PRFeedbackEnabled: !noPRFeedback,
		PRFeedbackAgent:   prFeedbackAgent,
	}

	// Resolve final configuration (precedence: flags > env vars > config file > defaults)
	resolved := config.Resolve(cfg, envState, flagState, flagValues)

	// Apply resolved values
	reviewers = resolved.Reviewers
	concurrency = resolved.Concurrency
	baseRef = resolved.Base
	timeout = resolved.Timeout
	retries = resolved.Retries
	summarizerAgentName = resolved.SummarizerAgent

	// For PR mode: fetch and qualify the base ref so git diff works in the detached worktree
	// Only do this for unqualified branch names - skip for SHAs, tags, HEAD, or already-qualified refs
	// When baseAutoDetected is true, always qualify (PR base refs are always unqualified branches)
	if prRemote != "" && git.ShouldQualifyBaseRef(baseRef, baseAutoDetected) {
		// Fetch the base ref from the remote so it exists locally
		if err := git.FetchBaseRef(prRepoRoot, prRemote, baseRef); err != nil {
			logger.Logf(terminal.StyleWarning, "Could not fetch base ref: %v", err)
			// Don't qualify - keep original ref so git diff can try it directly
		} else {
			// Only qualify the base ref if fetch succeeded
			baseRef = git.QualifyBaseRef(prRemote, baseRef)
		}
	}

	// Validate resolved config
	if reviewers < 1 {
		logger.Log("reviewers must be >= 1", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	// Default concurrency to reviewers if not specified (0 means same as reviewers)
	if concurrency <= 0 {
		concurrency = reviewers
	}
	if concurrency > reviewers {
		concurrency = reviewers
	}

	// Merge exclude patterns (config patterns + CLI patterns)
	allExcludePatterns := config.Merge(cfg, excludePatterns)

	// Resolve guidance (precedence: flags > env vars > config file)
	resolvedGuidance, err := config.ResolveGuidance(cfg, envState, flagState, flagValues, configDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to resolve guidance: %v", err)
		return exitCode(domain.ExitError)
	}

	// Auto-detect PR for current branch if not explicitly specified and not in local mode
	// This enables PR feedback summarization even without --pr flag
	// Skip auto-detection if PR feedback is disabled since the PR number is only used for feedback
	detectedPR := prNumber
	if detectedPR == "" && !local && resolved.PRFeedbackEnabled && github.IsGHAvailable() {
		if detected, err := github.GetCurrentPRNumber(ctx, worktreeBranch); err == nil {
			detectedPR = detected
			if verbose {
				logger.Logf(terminal.StyleDim, "Auto-detected PR #%s for current branch", detectedPR)
			}
		}
	}

	// Run the review
	code := executeReview(ctx, workDir, allExcludePatterns, resolvedGuidance, resolved.ReviewerAgents, resolved.SummarizerAgent, resolved.Fetch, refFile, resolved.FPFilterEnabled, resolved.FPThreshold, resolved.PRFeedbackEnabled, resolved.PRFeedbackAgent, detectedPR, logger)
	return exitCode(code)
}
