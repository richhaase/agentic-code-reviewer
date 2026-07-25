package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/runner"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func newDeskDispatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dispatch <[host/]owner/repo#number>",
		Short: "Run a headless review for one desk item and persist the result",
		Long: "Resolve an eligible desk item's trusted repository, run its review in an isolated worktree, " +
			"and persist the result without posting to GitHub. Use `acr desk history` to inspect it afterward.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := desk.ParsePullRequestRef(args[0])
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return runDeskDispatch(ctx, key, github.NewDiscovery(), defaultDispatchService)
		},
	}
}

func runDeskDispatch(ctx context.Context, key store.PullRequestKeyV1, discovery github.Discovery, newService dispatchServiceFactory) error {
	if !terminal.IsStdoutTTY() {
		terminal.DisableColors()
	}

	configDir, err := workspace.ConfigDir()
	if err != nil {
		return err
	}
	cfg, err := workspace.Load(configDir)
	if err != nil {
		return err
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return fmt.Errorf("workspace configuration has %d error(s): %s", len(errs), strings.Join(errs, "; "))
	}

	dataDir, err := store.DataDir()
	if err != nil {
		return err
	}

	lock, err := store.AcquireWriteLock(dataDir)
	if err != nil {
		if errors.Is(err, store.ErrWriterLocked) {
			return fmt.Errorf("cannot dispatch %s: %w", key.String(), err)
		}
		return err
	}
	released := false
	release := func() error {
		if released {
			return nil
		}
		released = true
		return lock.Release()
	}
	defer func() { _ = release() }()

	if identityErr := workspace.CheckIdentity(ctx, *cfg); identityErr != nil {
		return fmt.Errorf("GitHub identity could not be verified: %w", identityErr)
	}

	inbox, err := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now())
	if err != nil {
		return err
	}

	item, ok := findDeskItem(inbox, key)
	if !ok {
		return fmt.Errorf("%s is not in the current desk view; run `acr desk --once` first", key.String())
	}
	if eligibleErr := checkDispatchEligible(item); eligibleErr != nil {
		return eligibleErr
	}

	logger := terminal.NewLogger()
	run, dispatchErr := dispatchReview(ctx, item, logger, newService)

	var saveErr error
	if run != nil {
		saveErr = persistDispatchRun(dataDir, *run)
	}

	refreshed, refreshErr := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now())

	if releaseErr := release(); releaseErr != nil {
		return releaseErr
	}

	if dispatchErr != nil {
		return dispatchErr
	}
	if run == nil {
		return errors.New("dispatch failed: no review result was produced")
	}
	if saveErr != nil {
		return fmt.Errorf("review completed but could not be saved: %w", saveErr)
	}

	fmt.Println(runner.RenderReviewRun(*run))

	if refreshErr != nil {
		fmt.Printf("\nwarning: could not refresh desk state after dispatch: %v\n", refreshErr)
		return nil
	}
	if resultItem, ok := findDeskItem(refreshed, key); ok {
		fmt.Printf("\nDesk state: %s — %s\n", resultItem.DeskState, resultItem.Reason)
	}
	fmt.Printf("Run `acr desk history %s` for the full timeline.\n", key.String())
	return nil
}

func findDeskItem(inbox desk.Inbox, key store.PullRequestKeyV1) (desk.Item, bool) {
	for _, item := range inbox.Items {
		if item.Key == key {
			return item, true
		}
	}
	return desk.Item{}, false
}

func checkDispatchEligible(item desk.Item) error {
	switch item.DeskState {
	case domain.DeskStateResolved:
		return fmt.Errorf("%s is already resolved", item.Key.String())
	case domain.DeskStateRepositoryUnavailable:
		return fmt.Errorf("%s: repository is not available locally", item.Key.String())
	case domain.DeskStateWaitingOnOthers:
		return fmt.Errorf("%s: %s", item.Key.String(), item.Reason)
	case domain.DeskStateRunning, domain.DeskStateQueued:
		return fmt.Errorf("%s: a review is already %s", item.Key.String(), item.DeskState)
	}
	if item.RepositoryPath == "" {
		return fmt.Errorf("%s has no local repository path", item.Key.String())
	}
	return nil
}

type dispatchServiceFactory func(ReviewOpts, *terminal.Logger) (semanticReviewService, error)

func dispatchReview(ctx context.Context, item desk.Item, logger *terminal.Logger, newService dispatchServiceFactory) (*domain.ReviewRun, error) {
	_ = git.PruneStaleWorktrees(item.RepositoryPath)

	prNumber := strconv.Itoa(item.Key.Number)

	remote, err := github.FindRepoRemote(ctx, item.RepositoryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve PR fetch remote: %w", err)
	}

	detectedBase, _ := github.GetPRBaseRef(ctx, item.RepositoryPath, prNumber)

	logger.Logf(terminal.StyleInfo, "Fetching PR %s#%s%s",
		terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))

	wt, err := git.CreateWorktreeFromPR(ctx, item.RepositoryPath, remote, prNumber)
	if err != nil {
		return nil, fmt.Errorf("create review worktree: %w", err)
	}
	defer func() {
		logger.Log("Cleaning up worktree", terminal.StyleDim)
		_ = wt.Remove()
	}()

	configSource, err := resolveTrustedReviewConfigSourceForRoot(ctx, item.RepositoryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve trusted review configuration: %w", err)
	}
	loadResult, err := configSource.LoadWithWarnings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load trusted review configuration: %w", err)
	}
	for _, warning := range loadResult.Warnings {
		logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
	}

	envState, envWarnings := config.LoadEnvState()
	for _, warning := range envWarnings {
		logger.Logf(terminal.StyleWarning, "Warning: %s", warning)
	}

	flagState := config.FlagState{BaseSet: detectedBase != ""}
	flagValues := config.ResolvedConfig{Base: detectedBase}
	resolved := config.Resolve(loadResult.Config, envState, flagState, flagValues)

	wtResult := worktreeResult{
		workDir:          wt.Path,
		repositoryRoot:   item.RepositoryPath,
		detectedBase:     detectedBase,
		baseAutoDetected: detectedBase != "",
		prRemote:         remote,
		prRepoRoot:       item.RepositoryPath,
	}
	prepareReviewBase(ctx, wtResult, &resolved, logger)

	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	if resolved.Concurrency <= 0 {
		resolved.Concurrency = resolved.Reviewers
	}
	if resolved.Concurrency > resolved.Reviewers {
		resolved.Concurrency = resolved.Reviewers
	}

	resolvedGuidance, err := config.ResolveGuidanceFromLoadResult(ctx, loadResult, envState, flagState, flagValues)
	if err != nil {
		return nil, fmt.Errorf("resolve review guidance: %w", err)
	}
	resolved.Guidance = resolvedGuidance
	excludePatterns := config.Merge(loadResult.Config, nil)

	pullRequestKey := item.Key.ToDomain()
	opts := ReviewOpts{
		ResolvedConfig:  resolved,
		PRNumber:        prNumber,
		DetectedPR:      prNumber,
		ExcludePatterns: excludePatterns,
		RepositoryRoot:  item.RepositoryPath,
		WorkDir:         wt.Path,
		PullRequest:     &pullRequestKey,
		ConfigSource:    loadResult.Source,
		Trigger:         domain.ReviewTriggerDesk,
	}

	if err := agent.ValidateAgentNames(opts.ReviewerAgents); err != nil {
		return nil, fmt.Errorf("invalid agent: %w", err)
	}

	resolvedBaseRef := resolveReviewBase(ctx, opts, logger)

	events := newCLIReviewEvents(opts, logger)
	defer events.Close()

	service, err := newService(opts, logger)
	if err != nil {
		return nil, fmt.Errorf("initialize review service: %w", err)
	}

	request, err := newReviewRequest(opts, resolvedBaseRef, events)
	if err != nil {
		return nil, err
	}

	run, err := service.Run(ctx, request)
	if run == nil && err != nil {
		return nil, err
	}
	return run, nil
}

func defaultDispatchService(opts ReviewOpts, logger *terminal.Logger) (semanticReviewService, error) {
	return reviewpkg.NewService(reviewpkg.WithDiffProvider(newGitDiffProvider(opts, logger)))
}

func persistDispatchRun(dataDir string, run domain.ReviewRun) error {
	schema, err := store.ToReviewRunSchema(run, store.RenderedOutcomeV1{})
	if err != nil {
		return err
	}
	_, err = store.NewFilesystemRunStore(dataDir).SaveRun(schema)
	return err
}
