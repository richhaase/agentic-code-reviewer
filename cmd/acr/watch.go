package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a PR and re-review until an LGTM is posted",
		Long: `Run a review against one PR, post the result according to --post-mode, then
keep watching the PR and re-review when a re-review is requested or new
commits settle, until a terminal LGTM is posted or a safety bound is reached.

The watched PR is selected with --pr or detected from the current branch.

Post modes:
  interactive  Prompt for every submission decision (default; requires a TTY)
  comment      Unattended; every result is posted as a comment review only
  approve      Unattended; LGTM approves the PR once CI is green

Exit codes:
  0 - LGTM posted (or declined interactively), or PR merged
  1 - Safety bound reached or PR closed without an LGTM
  2 - Error
  130 - Interrupted`,
		Args:          cobra.NoArgs,
		RunE:          runWatch,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	registerSharedReviewFlags(cmd)

	cmd.Flags().StringVar(&watchPostMode, "post-mode", "interactive",
		"How results are posted: interactive, comment, or approve")
	cmd.Flags().DurationVar(&watchPollInterval, "poll-interval", 0,
		"How often to refresh the watched PR's state (default: 1m)")
	cmd.Flags().DurationVar(&watchSettleTime, "settle-time", 0,
		"Quiet period after the latest pushed commit before re-reviewing (default: 10m)")
	cmd.Flags().IntVar(&watchMaxReviews, "max-reviews", 0,
		"Maximum total review runs, including the initial run (default: 10)")
	cmd.Flags().DurationVar(&watchMaxDuration, "max-duration", 0,
		"Maximum wall-clock watch lifetime (default: 24h)")

	setGroupedUsage(cmd)

	return cmd
}

func runWatch(cmd *cobra.Command, _ []string) error {
	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}
	logger := terminal.NewLogger()

	mode, err := watch.ParsePostMode(watchPostMode)
	if err != nil {
		logger.Logf(terminal.StyleError, "%v", err)
		return exitCode(domain.ExitError)
	}
	if mode == watch.PostModeInteractive && !terminal.IsStdinTTY() {
		logger.Log("Interactive watch requires a TTY on stdin. Use --post-mode comment or approve for unattended runs.", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	if err := github.CheckGHAvailable(); err != nil {
		logger.Logf(terminal.StyleError, "acr watch requires the gh CLI: %v", err)
		return exitCode(domain.ExitError)
	}

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

	watchPR := prNumber
	if watchPR == "" {
		detected, err := github.GetCurrentPRNumber(ctx, "")
		switch {
		case errors.Is(err, github.ErrAuthFailed):
			logger.Log("GitHub authentication failed. Run 'gh auth login' to authenticate.", terminal.StyleError)
			return exitCode(domain.ExitError)
		case errors.Is(err, github.ErrNoPRFound), err == nil && detected == "":
			logger.Log("No open PR found for the current branch; use --pr to select one.", terminal.StyleError)
			return exitCode(domain.ExitError)
		case err != nil:
			logger.Logf(terminal.StyleError, "Failed to detect PR for current branch: %v", err)
			return exitCode(domain.ExitError)
		}
		watchPR = detected
		logger.Logf(terminal.StyleDim, "Detected PR #%s for current branch", watchPR)
	}
	if err := github.ValidatePR(ctx, watchPR); err != nil {
		logger.Logf(terminal.StyleError, "Failed to access PR #%s: %v", watchPR, err)
		return exitCode(domain.ExitError)
	}

	prNumber = watchPR

	cfgResult, err := loadAndResolveConfig(cmd, worktreeResult{}, logger)
	if err != nil {
		return err
	}
	wcfg := watch.Config{
		Mode:         mode,
		PollInterval: cfgResult.resolved.WatchPollInterval,
		SettleTime:   cfgResult.resolved.WatchSettleTime,
		MaxReviews:   cfgResult.resolved.WatchMaxReviews,
		MaxDuration:  cfgResult.resolved.WatchMaxDuration,
	}

	currentUser := github.GetCurrentUser(ctx)
	if currentUser == "" {
		logger.Log("Could not determine the authenticated gh user; re-review request triggers are disabled.", terminal.StyleWarning)
	}

	logger.Logf(terminal.StyleInfo, "Watching PR %s (mode=%s, poll=%s, settle=%s, max-reviews=%d, max-duration=%s)",
		formatPRRef(watchPR), mode, wcfg.PollInterval, wcfg.SettleTime, wcfg.MaxReviews, wcfg.MaxDuration)

	deps := watch.Deps{
		Clock: watch.RealClock{},
		Logf: func(format string, args ...any) {
			logger.Logf(terminal.StyleInfo, format, args...)
		},
		State: func(ctx context.Context) (watch.PRState, error) {
			st, err := github.GetPRWatchState(ctx, watchPR)
			if err != nil {
				return watch.PRState{}, err
			}
			return watch.PRState{
				HeadSHA:         st.HeadSHA,
				Closed:          st.Closed(),
				Merged:          st.Merged(),
				ReviewRequested: st.ReviewRequestedFrom(currentUser),
			}, nil
		},
		CIGreen: func(ctx context.Context) (bool, error) {
			status := github.CheckCIStatus(ctx, watchPR)
			if status.Error != "" {
				return false, fmt.Errorf("%s", status.Error)
			}
			return status.AllPassed, nil
		},
		Approve: func(ctx context.Context, body string) error {
			return github.ApprovePR(ctx, watchPR, body)
		},
		RunCycle: func(ctx context.Context, _ int, _ string) (watch.Cycle, error) {
			return runWatchCycle(ctx, cmd, watchPR, mode, logger)
		},
	}

	reason := watch.Run(ctx, wcfg, deps)
	logger.Logf(terminal.StyleInfo, "Watch finished: %s", reason)

	switch reason {
	case watch.ReasonLGTM, watch.ReasonDeclined, watch.ReasonMerged:
		return nil
	case watch.ReasonInterrupted:
		return exitCode(domain.ExitInterrupted)
	case watch.ReasonError:
		return exitCode(domain.ExitError)
	default:
		return exitCode(domain.ExitFindings)
	}
}

func runWatchCycle(ctx context.Context, cmd *cobra.Command, watchPR string, mode watch.PostMode, logger *terminal.Logger) (watch.Cycle, error) {
	wt, err := setupWorktree(ctx, cmd, logger)
	if err != nil {
		return watch.Cycle{Result: watch.CycleError}, err
	}
	if wt.cleanup != nil {
		defer wt.cleanup()
	}

	reviewedHead := ""
	if wt.workDir != "" {
		if sha, err := git.GetHeadSHA(wt.workDir); err == nil {
			reviewedHead = sha
		}
	}

	cfgSource := wt
	cfgSource.workDir = ""
	cfgResult, err := loadAndResolveConfig(cmd, cfgSource, logger)
	if err != nil {
		return watch.Cycle{Result: watch.CycleError}, err
	}

	outcome := &CycleOutcome{}
	opts := ReviewOpts{
		ResolvedConfig:   cfgResult.resolved,
		Verbose:          verbose,
		AutoYes:          mode != watch.PostModeInteractive,
		PRNumber:         watchPR,
		DetectedPR:       watchPR,
		UseRefFile:       refFile,
		ExcludePatterns:  cfgResult.excludePatterns,
		WorkDir:          wt.workDir,
		ForcePostComment: mode == watch.PostModeComment,
		ExpectedHeadSHA:  reviewedHead,
		Outcome:          outcome,
	}

	code := executeReview(ctx, opts, logger)
	if code == domain.ExitInterrupted {
		return watch.Cycle{Result: watch.CycleError}, ctx.Err()
	}
	if code == domain.ExitError {
		return watch.Cycle{Result: watch.CycleError}, fmt.Errorf("review cycle failed")
	}

	return watch.Cycle{Result: mapCycleOutcome(outcome), LGTMBody: outcome.LGTMBody, HeadSHA: reviewedHead}, nil
}

func mapCycleOutcome(o *CycleOutcome) watch.CycleResult {
	switch o.Kind {
	case OutcomeNoChanges:
		return watch.CycleNoChanges
	case OutcomeFindings:
		return watch.CycleFindings
	case OutcomeLGTMApproved:
		return watch.CycleLGTMApproved
	case OutcomeLGTMComment:
		if o.CIDowngraded {
			return watch.CycleLGTMCommentCIPending
		}
		return watch.CycleLGTMComment
	case OutcomeLGTMDeclined:
		return watch.CycleLGTMDeclined
	case OutcomeLGTMSkipped:
		return watch.CycleLGTMSkipped
	case OutcomeStaleHead:
		return watch.CycleStaleHead
	default:
		return watch.CycleError
	}
}
