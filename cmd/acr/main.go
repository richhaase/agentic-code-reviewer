// Package main provides the CLI entry point for the agentic code reviewer.
package main

import (
	"context"
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
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

var (
	reviewers           int
	concurrency         int
	baseRef             string
	timeout             time.Duration
	retries             int
	refFile             bool
	prompt              string
	promptFile          string
	verbose             bool
	local               bool
	worktreeBranch      string
	autoYes             bool
	autoNo              bool
	excludePatterns     []string
	noConfig            bool
	agentName           string
	summarizerAgentName string
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
	rootCmd.Flags().BoolVar(&refFile, "ref-file", false,
		"Enable ref-file mode (env: ACR_REF_FILE)")
	rootCmd.Flags().StringVar(&prompt, "prompt", "",
		"[experimental] Custom review prompt (env: ACR_REVIEW_PROMPT)")
	rootCmd.Flags().StringVar(&promptFile, "prompt-file", "",
		"[experimental] Path to file containing review prompt (env: ACR_REVIEW_PROMPT_FILE)")
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
	rootCmd.Flags().StringArrayVar(&excludePatterns, "exclude-pattern", nil,
		"Exclude findings matching regex pattern (repeatable)")
	rootCmd.Flags().BoolVar(&noConfig, "no-config", false,
		"Skip loading .acr.yaml config file")
	rootCmd.Flags().StringVarP(&agentName, "reviewer-agent", "a", "codex",
		"[experimental] Agent(s) for reviews (comma-separated): codex, claude, gemini (env: ACR_REVIEWER_AGENT)")
	rootCmd.Flags().StringVarP(&summarizerAgentName, "summarizer-agent", "s", "codex",
		"[experimental] Agent to use for summarization: codex, claude, gemini (env: ACR_SUMMARIZER_AGENT)")

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
	flagState := config.FlagState{
		ReviewersSet:        cmd.Flags().Changed("reviewers"),
		ConcurrencySet:      cmd.Flags().Changed("concurrency"),
		BaseSet:             cmd.Flags().Changed("base"),
		TimeoutSet:          cmd.Flags().Changed("timeout"),
		RetriesSet:          cmd.Flags().Changed("retries"),
		RefFileSet:          cmd.Flags().Changed("ref-file"),
		ReviewerAgentsSet:   cmd.Flags().Changed("reviewer-agent"),
		SummarizerAgentSet:  cmd.Flags().Changed("summarizer-agent"),
		ReviewPromptSet:     cmd.Flags().Changed("prompt"),
		ReviewPromptFileSet: cmd.Flags().Changed("prompt-file"),
	}

	// Load env var state
	envState := config.LoadEnvState()

	// Build flag values struct
	flagValues := config.ResolvedConfig{
		Reviewers:        reviewers,
		Concurrency:      concurrency,
		Base:             baseRef,
		Timeout:          timeout,
		Retries:          retries,
		RefFile:          refFile,
		ReviewerAgents:   agent.ParseAgentNames(agentName),
		SummarizerAgent:  summarizerAgentName,
		ReviewPrompt:     prompt,
		ReviewPromptFile: promptFile,
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

	// Resolve custom prompt (precedence: flags > env vars > config file)
	customPrompt, err := config.ResolvePrompt(cfg, envState, flagState, flagValues, configDir)
	if err != nil {
		logger.Logf(terminal.StyleError, "Failed to resolve prompt: %v", err)
		return exitCode(domain.ExitError)
	}

	// Run the review
	code := executeReview(ctx, workDir, allExcludePatterns, customPrompt, resolved.ReviewerAgents, logger)
	return exitCode(code)
}
