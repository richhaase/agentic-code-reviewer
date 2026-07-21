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
	reviewerModel       string
	summarizerModel     string
	refFile             bool
	noFPFilter          bool
	fpThreshold         int
	summarizerTimeout   time.Duration
	fpFilterTimeout     time.Duration
	noPRFeedback        bool
	prFeedbackAgent     string

	watchPostMode     string
	watchPollInterval time.Duration
	watchSettleTime   time.Duration
	watchMaxReviews   int
	watchMaxDuration  time.Duration
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

	registerSharedReviewFlags(rootCmd)

	rootCmd.Flags().BoolVarP(&autoYes, "yes", "y", false,
		"Automatically submit review without prompting")
	rootCmd.Flags().BoolVarP(&local, "local", "l", false,
		"Skip posting findings to a PR")
	rootCmd.Flags().StringVarP(&worktreeBranch, "worktree-branch", "B", "",
		"Review a branch in a temporary worktree")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newWatchCmd())

	setGroupedUsage(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		if exitErr, ok := err.(exitCodeError); ok {
			return exitErr.code.Int()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return domain.ExitError.Int()
	}

	return 0
}

func registerSharedReviewFlags(cmd *cobra.Command) {
	cmd.Flags().IntVarP(&reviewers, "reviewers", "r", 0,
		"Number of parallel reviewers (default: 5, env: ACR_REVIEWERS)")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0,
		"Max concurrent reviewers (default: same as --reviewers, env: ACR_CONCURRENCY)")
	cmd.Flags().StringVarP(&baseRef, "base", "b", "",
		"Base ref for review command (default: main, env: ACR_BASE_REF)")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 0,
		"Timeout per reviewer (default: 10m, env: ACR_TIMEOUT)")
	cmd.Flags().IntVarP(&retries, "retries", "R", 0,
		"Retry failed reviewers N times (default: 1, env: ACR_RETRIES)")
	cmd.Flags().BoolVar(&fetch, "fetch", true,
		"Fetch latest base ref from origin before diff (default: true, env: ACR_FETCH)")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false,
		"Disable fetching the review base ref; trusted config refresh still runs")
	cmd.Flags().StringVar(&guidance, "guidance", "",
		"Steering context appended to the review prompt (env: ACR_GUIDANCE)")
	cmd.Flags().StringVar(&guidanceFile, "guidance-file", "",
		"Path to file containing review guidance (env: ACR_GUIDANCE_FILE)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"Print agent messages as they arrive")
	cmd.Flags().StringVar(&prNumber, "pr", "",
		"Review a PR by number (fetches into temp worktree)")

	cmd.Flags().StringArrayVar(&excludePatterns, "exclude-pattern", nil,
		"Exclude findings matching regex pattern (repeatable)")
	cmd.Flags().BoolVar(&noConfig, "no-config", false,
		"Disable trusted repository .acr.yaml for this review")
	cmd.Flags().StringVarP(&agentName, "reviewer-agent", "a", "codex",
		"Agent(s) for reviews (comma-separated): agy, codex, claude, gemini (env: ACR_REVIEWER_AGENT)")
	cmd.Flags().StringVarP(&summarizerAgentName, "summarizer-agent", "s", "codex",
		"Agent to use for summarization: agy, codex, claude, gemini (env: ACR_SUMMARIZER_AGENT)")
	cmd.Flags().StringVar(&reviewerModel, "reviewer-model", "",
		"LLM model for review agents (env: ACR_REVIEWER_MODEL)")
	cmd.Flags().StringVar(&summarizerModel, "summarizer-model", "",
		"LLM model for summarizer/FP filter agents (env: ACR_SUMMARIZER_MODEL)")
	cmd.Flags().BoolVar(&refFile, "ref-file", false,
		"Write diff to a temp file instead of embedding in prompt (auto-enabled for large diffs)")
	cmd.Flags().BoolVar(&noFPFilter, "no-fp-filter", false,
		"Disable false positive filtering (env: ACR_FP_FILTER=false to disable)")
	cmd.Flags().IntVar(&fpThreshold, "fp-threshold", 75,
		"False positive confidence threshold 1-100 (default: 75, env: ACR_FP_THRESHOLD)")
	cmd.Flags().DurationVar(&summarizerTimeout, "summarizer-timeout", 0,
		"Timeout for summarizer phase (default: 5m, env: ACR_SUMMARIZER_TIMEOUT)")
	cmd.Flags().DurationVar(&fpFilterTimeout, "fp-filter-timeout", 0,
		"Timeout for false positive filter phase (default: 5m, env: ACR_FP_FILTER_TIMEOUT)")
	cmd.Flags().BoolVar(&noPRFeedback, "no-pr-feedback", false,
		"Disable reading PR comments for feedback context (env: ACR_PR_FEEDBACK=false)")
	cmd.Flags().StringVar(&prFeedbackAgent, "pr-feedback-agent", "",
		"Agent for PR feedback summarization (default: same as --summarizer-agent, env: ACR_PR_FEEDBACK_AGENT)")
}

type worktreeResult struct {
	workDir          string
	detectedBase     string
	baseAutoDetected bool
	prRemote         string
	prRepoRoot       string
	cleanup          func()
}

func setupWorktree(ctx context.Context, cmd *cobra.Command, logger *terminal.Logger) (worktreeResult, error) {
	var result worktreeResult

	if prNumber != "" && worktreeBranch != "" {
		logger.Log("--pr and --worktree-branch are mutually exclusive", terminal.StyleError)
		return result, exitCode(domain.ExitError)
	}

	if prNumber != "" {
		if err := github.CheckGHAvailable(); err != nil {
			logger.Logf(terminal.StyleError, "--pr requires gh CLI: %v", err)
			return result, exitCode(domain.ExitError)
		}

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

		explicitBaseSet := cmd.Flags().Changed("base") || os.Getenv("ACR_BASE_REF") != ""
		if !explicitBaseSet {
			if detectedBase, err := github.GetPRBaseRef(ctx, prNumber); err == nil && detectedBase != "" {
				result.detectedBase = detectedBase
				result.baseAutoDetected = true
				logger.Logf(terminal.StyleDim, "Auto-detected base: %s", detectedBase)
			}
		}

		repoRoot, err := git.GetRoot()
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}
		result.prRepoRoot = repoRoot

		remote := github.GetRepoRemote(ctx)
		result.prRemote = remote

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

		var actualRef string
		var cleanupRemote func()

		forkRef, err := github.ResolveForkRef(ctx, worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "%v", err)
			return result, exitCode(domain.ExitError)
		}

		if forkRef != nil {

			logger.Logf(terminal.StyleInfo, "Resolved fork PR #%d from %s",
				forkRef.PRNumber, forkRef.Username)

			repoRoot, err := git.GetRoot()
			if err != nil {
				logger.Logf(terminal.StyleError, "Error getting repo root: %v", err)
				return result, exitCode(domain.ExitError)
			}

			if err := git.AddRemote(repoRoot, forkRef.RemoteName, forkRef.RepoURL); err != nil {
				logger.Logf(terminal.StyleError, "Error adding remote: %v", err)
				return result, exitCode(domain.ExitError)
			}
			cleanupRemote = func() {
				_ = git.RemoveRemote(repoRoot, forkRef.RemoteName)
			}

			logger.Logf(terminal.StyleDim, "Fetching %s from %s", forkRef.Branch, forkRef.RepoURL)
			if err := git.FetchBranch(ctx, repoRoot, forkRef.RemoteName, forkRef.Branch); err != nil {
				cleanupRemote()
				logger.Logf(terminal.StyleError, "Error fetching fork branch: %v", err)
				return result, exitCode(domain.ExitError)
			}

			actualRef = fmt.Sprintf("%s/%s", forkRef.RemoteName, forkRef.Branch)
		} else {

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

type configResult struct {
	resolved        config.ResolvedConfig
	excludePatterns []string
	source          config.SourceIdentity
}

func loadAndResolveConfig(ctx context.Context, cmd *cobra.Command, wt worktreeResult, source config.Source, logger *terminal.Logger) (configResult, error) {

	var cfg *config.Config
	var loadResult *config.LoadResult
	if source == nil {
		logger.Log("Config error: no trusted review configuration source was selected", terminal.StyleError)
		return configResult{}, exitCode(domain.ExitError)
	}
	loaded, err := source.LoadWithWarnings(ctx)
	if err != nil {
		logger.Logf(terminal.StyleError, "Config error: %v", err)
		return configResult{}, exitCode(domain.ExitError)
	}
	loadResult = loaded
	cfg = loaded.Config

	for _, warning := range loaded.Warnings {
		logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
	}
	if verbose {
		logger.Logf(terminal.StyleDim, "Review configuration source: %s %s %s", loaded.Source.Kind, loaded.Source.Ref, loaded.Source.Revision)
	}

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
		ReviewerModelSet:     cmd.Flags().Changed("reviewer-model"),
		SummarizerModelSet:   cmd.Flags().Changed("summarizer-model"),
		SummarizerTimeoutSet: cmd.Flags().Changed("summarizer-timeout"),
		FPFilterTimeoutSet:   cmd.Flags().Changed("fp-filter-timeout"),
		GuidanceSet:          cmd.Flags().Changed("guidance"),
		GuidanceFileSet:      cmd.Flags().Changed("guidance-file"),
		NoFPFilterSet:        cmd.Flags().Changed("no-fp-filter"),
		FPThresholdSet:       cmd.Flags().Changed("fp-threshold"),
		NoPRFeedbackSet:      cmd.Flags().Changed("no-pr-feedback"),
		PRFeedbackAgentSet:   cmd.Flags().Changed("pr-feedback-agent"),
		WatchPollIntervalSet: cmd.Flags().Changed("poll-interval"),
		WatchSettleTimeSet:   cmd.Flags().Changed("settle-time"),
		WatchMaxReviewsSet:   cmd.Flags().Changed("max-reviews"),
		WatchMaxDurationSet:  cmd.Flags().Changed("max-duration"),
	}

	envState, envWarnings := config.LoadEnvState()
	for _, warning := range envWarnings {
		logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
	}

	fetchValue := fetch && !noFetch

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
		ReviewerModel:     reviewerModel,
		SummarizerModel:   summarizerModel,
		SummarizerTimeout: summarizerTimeout,
		FPFilterTimeout:   fpFilterTimeout,
		Guidance:          guidance,
		GuidanceFile:      guidanceFile,
		FPFilterEnabled:   !noFPFilter,
		FPThreshold:       fpThreshold,
		PRFeedbackEnabled: !noPRFeedback,
		PRFeedbackAgent:   prFeedbackAgent,
		WatchPollInterval: watchPollInterval,
		WatchSettleTime:   watchSettleTime,
		WatchMaxReviews:   watchMaxReviews,
		WatchMaxDuration:  watchMaxDuration,
	}

	resolved := config.Resolve(cfg, envState, flagState, flagValues)

	if wt.prRemote != "" && git.ShouldQualifyBaseRef(resolved.Base, wt.baseAutoDetected) {

		if err := git.FetchBaseRef(ctx, wt.prRepoRoot, wt.prRemote, resolved.Base); err != nil {
			logger.Logf(terminal.StyleWarning, "Could not fetch base ref: %v", err)

		} else {

			resolved.Base = git.QualifyBaseRef(wt.prRemote, resolved.Base)
		}
	}

	if err := resolved.Validate(); err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return configResult{}, exitCode(domain.ExitError)
	}

	if resolved.Concurrency <= 0 {
		resolved.Concurrency = resolved.Reviewers
	}
	if resolved.Concurrency > resolved.Reviewers {
		resolved.Concurrency = resolved.Reviewers
	}

	allExcludePatterns := config.Merge(cfg, excludePatterns)

	resolvedGuidance, err := config.ResolveGuidanceFromLoadResult(ctx, loadResult, envState, flagState, flagValues)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to resolve guidance: %v", err)
		return configResult{}, exitCode(domain.ExitError)
	}
	resolved.Guidance = resolvedGuidance

	result := configResult{
		resolved:        resolved,
		excludePatterns: allExcludePatterns,
	}
	result.source = loadResult.Source
	return result, nil
}

func runReview(cmd *cobra.Command, _ []string) error {

	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}

	logger := terminal.NewLogger()

	if err := git.PruneStaleWorktrees(); err != nil && verbose {
		logger.Logf(terminal.StyleDim, "Worktree prune: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr)
		logger.Log("Interrupted, shutting down...", terminal.StyleWarning)
		cancel()
	}()

	configSource, err := resolveTrustedReviewConfigSource(ctx, noConfig)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return contextualExit(ctx, exitCode(domain.ExitError))
	}

	wt, err := setupWorktree(ctx, cmd, logger)
	if err != nil {
		return err
	}
	if wt.cleanup != nil {
		defer wt.cleanup()
	}

	cfgResult, err := loadAndResolveConfig(ctx, cmd, wt, configSource, logger)
	if err != nil {
		return contextualExit(ctx, err)
	}

	detectedPR := prNumber
	if detectedPR == "" && !local && cfgResult.resolved.PRFeedbackEnabled && github.IsGHAvailable() {
		if detected, err := github.GetCurrentPRNumber(ctx, worktreeBranch); err == nil {
			detectedPR = detected
			if verbose {
				logger.Logf(terminal.StyleDim, "Auto-detected PR #%s for current branch", detectedPR)
			}
		}
	}

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
