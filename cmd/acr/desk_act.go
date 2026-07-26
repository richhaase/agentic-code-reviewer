package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

type deskActAction int

const (
	deskActApprove deskActAction = iota
	deskActComment
	deskActRequestChanges
)

func newDeskActCmd() *cobra.Command {
	var approve, comment, requestChanges, autoYes bool

	cmd := &cobra.Command{
		Use:   "act <[host/]owner/repo#number>",
		Short: "Post a guarded review action for a decision-ready desk item",
		Long: "Post a comment, request-changes, or approval review for a pull request with a decision-ready " +
			"stored result. Identity, posting capability, repository availability, and the current PR head are " +
			"revalidated immediately before any GitHub mutation, and self-authored pull requests can only receive " +
			"a comment.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := desk.ParsePullRequestRef(args[0])
			if err != nil {
				return err
			}
			action, err := parseDeskActAction(approve, comment, requestChanges)
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

			return runDeskAct(ctx, key, action, autoYes, github.NewDiscovery())
		},
	}

	cmd.Flags().BoolVar(&approve, "approve", false, "Approve the pull request (only valid when the review found no issues)")
	cmd.Flags().BoolVar(&comment, "comment", false, "Post a comment-only review")
	cmd.Flags().BoolVar(&requestChanges, "request-changes", false, "Request changes (only valid when the review has findings)")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip the interactive confirmation prompt")

	return cmd
}

func parseDeskActAction(approve, comment, requestChanges bool) (deskActAction, error) {
	var action deskActAction
	count := 0
	if approve {
		action = deskActApprove
		count++
	}
	if comment {
		action = deskActComment
		count++
	}
	if requestChanges {
		action = deskActRequestChanges
		count++
	}
	if count != 1 {
		return 0, errors.New("exactly one of --approve, --comment, or --request-changes is required")
	}
	return action, nil
}

func runDeskAct(ctx context.Context, key store.PullRequestKeyV1, action deskActAction, autoYes bool, discovery github.Discovery) error {
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
		return fmt.Errorf("workspace configuration has %d error(s): %v", len(errs), errs)
	}

	if postingErr := cfg.RequirePosting(); postingErr != nil {
		return postingErr
	}

	dataDir, err := store.DataDir()
	if err != nil {
		return err
	}

	lock, err := store.AcquireWriteLock(dataDir)
	if err != nil {
		if errors.Is(err, store.ErrWriterLocked) {
			return fmt.Errorf("cannot act on %s: %w", key.String(), err)
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
	if item.DeskState != domain.DeskStateFindingsReady && item.DeskState != domain.DeskStateDecisionReady {
		return fmt.Errorf("%s has no decision-ready result (current state: %s); run `acr desk dispatch %s` first",
			key.String(), item.DeskState, key.String())
	}
	if _, corruptEvents, eventsErr := store.NewFilesystemEventStore(dataDir).ListEvents(key); eventsErr != nil {
		return eventsErr
	} else if len(corruptEvents) > 0 {
		return fmt.Errorf("%d stored event(s) could not be read; run `acr desk history %s` to investigate before acting",
			len(corruptEvents), key.String())
	}

	run, err := loadCurrentRun(dataDir, item)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("%s: no stored review run matches the current head", key.String())
	}
	if run.Conclusion == domain.ReviewConclusionNoChanges {
		return fmt.Errorf("%s: the stored review found no changes to act on", key.String())
	}
	if actionErr := validateActionForConclusion(action, run.Conclusion); actionErr != nil {
		return actionErr
	}

	prNumber := strconv.Itoa(item.Key.Number)
	logger := terminal.NewLogger()

	isSelfReview := github.IsSelfReview(ctx, item.RepositoryPath, prNumber)

	if !autoYes && !confirmDeskAction(action, prNumber, isSelfReview) {
		return errors.New("action was not confirmed; nothing was posted")
	}

	liveSnapshot, enrichErr := discovery.Enrich(ctx, key.ToDomain())
	if enrichErr != nil || liveSnapshot.HeadObjectID == "" {
		return fmt.Errorf("could not verify the current pull request head before posting: %w", enrichErr)
	}
	if liveSnapshot.HeadObjectID != run.Target.Revision.HeadObjectID || liveSnapshot.BaseObjectID != run.Target.Revision.BaseObjectID {
		if staleErr := appendStaleEvent(dataDir, key, run, liveSnapshot.HeadObjectID, liveSnapshot.BaseObjectID); staleErr != nil {
			return fmt.Errorf("the pull request revision moved since this review, and the stale result could not be recorded: %w", staleErr)
		}
		return errors.New("the pull request's head or base moved since this review; nothing was posted")
	}

	freshCfg, freshErr := workspace.Load(configDir)
	if freshErr != nil {
		return fmt.Errorf("could not reload workspace configuration before posting: %w", freshErr)
	}
	if errs := freshCfg.Validate(); len(errs) > 0 {
		return fmt.Errorf("workspace configuration has %d error(s): %v", len(errs), errs)
	}
	if postingErr := freshCfg.RequirePosting(); postingErr != nil {
		return fmt.Errorf("posting was disabled before this action could be posted: %w", postingErr)
	}
	if identityErr := workspace.CheckIdentity(ctx, *freshCfg); identityErr != nil {
		return fmt.Errorf("GitHub identity could not be verified before posting: %w", identityErr)
	}

	verifiedActor := freshCfg.Identity.ExpectedUser
	outcome := &CycleOutcome{}
	opts := ReviewOpts{
		RepositoryRoot:                item.RepositoryPath,
		PRNumber:                      prNumber,
		AutoYes:                       autoYes,
		SkipReviewTypePrompt:          true,
		SuppressZeroSelectionFallback: action == deskActRequestChanges,
		ForcePostComment:              action == deskActComment || isSelfReview,
		ExpectedHeadSHA:               run.Target.Revision.HeadObjectID,
		ExpectedBaseSHA:               run.Target.Revision.BaseObjectID,
		PreSubmitCheck: func() error {
			actor, checkErr := revalidateDeskEligibility(ctx, configDir, dataDir, key, discovery)
			if checkErr == nil {
				verifiedActor = actor
			}
			return checkErr
		},
		Outcome: outcome,
	}

	code := handleTypedReviewRun(ctx, opts, run, logger)

	actor := verifiedActor

	var recordErr error
	var deferredForCI bool
	var actionErrResult error
	if code == domain.ExitError || code == domain.ExitInterrupted {
		actionErrResult = errors.New("posting failed; see the log above for details")
	} else {
		switch outcome.Kind {
		case OutcomeLGTMApproved:
			recordErr = appendActionEvent(dataDir, key, store.EventTypeActionApprovalPosted, run, actor)
		case OutcomeLGTMComment:
			if outcome.CIDowngraded {
				deferredForCI = true
			} else {
				recordErr = appendActionEvent(dataDir, key, store.EventTypeActionCommentPosted, run, actor)
			}
		case OutcomeReviewComment:
			recordErr = appendActionEvent(dataDir, key, store.EventTypeActionCommentPosted, run, actor)
		case OutcomeReviewRequestChanges:
			recordErr = appendActionEvent(dataDir, key, store.EventTypeActionRequestChangesPosted, run, actor)
		case OutcomeStaleHead:
			moved, staleErr := recordStaleResult(ctx, dataDir, key, run, discovery)
			switch {
			case staleErr != nil:
				actionErrResult = fmt.Errorf("could not verify whether the pull request revision moved: %w", staleErr)
			case moved:
				actionErrResult = errors.New("the pull request head or base moved since this review; nothing was posted")
			default:
				actionErrResult = errors.New("the revision check could not be completed reliably while posting, but a follow-up check found the pull request unchanged; nothing was posted — please retry")
			}
		default:
			actionErrResult = fmt.Errorf("action was not posted (outcome: %d)", outcome.Kind)
		}
	}

	refreshed, refreshErr := desk.Refresh(ctx, *freshCfg, dataDir, discovery, time.Now())

	if releaseErr := release(); releaseErr != nil {
		return releaseErr
	}

	if recordErr != nil {
		return fmt.Errorf("action completed but could not be recorded: %w", recordErr)
	}
	if actionErrResult != nil {
		return actionErrResult
	}

	if refreshErr == nil {
		if resultItem, found := findDeskItem(refreshed, key); found {
			fmt.Printf("\nDesk state: %s — %s\n", resultItem.DeskState, resultItem.Reason)
		}
	}
	if deferredForCI {
		fmt.Printf("CI is not green; posted a comment instead of approving. The item remains actionable — run `acr desk act %s --approve` again once CI passes.\n", key.String())
	}
	return nil
}

func confirmDeskAction(action deskActAction, prNumber string, isSelfReview bool) bool {
	if !terminal.IsStdinTTY() {
		return false
	}
	label := "post a comment on"
	if !isSelfReview {
		switch action {
		case deskActApprove:
			label = "approve"
		case deskActRequestChanges:
			label = "request changes on"
		}
	} else if action != deskActComment {
		label = "post a comment on (self-authored PRs cannot be approved or receive request-changes)"
	}
	fmt.Print(formatPrompt(fmt.Sprintf("About to %s PR %s", label, formatPRRef(prNumber)), "[y/N]:"))
	response := readUserInput()
	return response == "y" || response == "yes"
}

func validateActionForConclusion(action deskActAction, conclusion domain.ReviewConclusion) error {
	switch action {
	case deskActApprove:
		if conclusion == domain.ReviewConclusionFindings {
			return errors.New("cannot approve while the review has unresolved findings; use --comment or --request-changes instead")
		}
	case deskActRequestChanges:
		if conclusion != domain.ReviewConclusionFindings {
			return errors.New("--request-changes requires a review with findings; use --approve or --comment instead")
		}
	}
	return nil
}

func loadCurrentRun(dataDir string, item desk.Item) (*domain.ReviewRun, error) {
	schemas, corrupt, err := store.NewFilesystemRunStore(dataDir).ListRuns(item.Key)
	if err != nil {
		return nil, err
	}
	if len(corrupt) > 0 {
		return nil, fmt.Errorf("%d stored review run(s) could not be read; run `acr desk history %s` to investigate before acting",
			len(corrupt), item.Key.String())
	}
	var latest *domain.ReviewRun
	for _, schema := range schemas {
		if schema.Target.Revision.HeadObjectID != item.HeadObjectID || schema.Target.Revision.BaseObjectID != item.BaseObjectID {
			continue
		}
		run, _, convErr := store.FromReviewRunSchema(schema)
		if convErr != nil {
			continue
		}
		if latest == nil || runResultTimestamp(run).After(runResultTimestamp(*latest)) {
			candidate := run
			latest = &candidate
		}
	}
	return latest, nil
}

func runResultTimestamp(run domain.ReviewRun) time.Time {
	if !run.CompletedAt.IsZero() {
		return run.CompletedAt
	}
	return run.StartedAt
}

func appendActionEvent(dataDir string, key store.PullRequestKeyV1, eventType store.ReviewEventTypeV1, run *domain.ReviewRun, actor string) error {
	now := time.Now()
	id, err := newDeskEventID(now)
	if err != nil {
		return err
	}
	event := store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            id,
		PullRequest:   key,
		Type:          eventType,
		OccurredAt:    now,
		RunID:         run.ID,
		HeadObjectID:  run.Target.Revision.HeadObjectID,
		BaseObjectID:  run.Target.Revision.BaseObjectID,
		Actor:         actor,
	}
	_, err = store.NewFilesystemEventStore(dataDir).AppendEvent(event)
	return err
}

func recordStaleResult(ctx context.Context, dataDir string, key store.PullRequestKeyV1, run *domain.ReviewRun, discovery github.Discovery) (bool, error) {
	snapshot, err := discovery.Enrich(ctx, key.ToDomain())
	if err != nil {
		return false, fmt.Errorf("re-check current PR head: %w", err)
	}
	moved := snapshot.HeadObjectID != run.Target.Revision.HeadObjectID || snapshot.BaseObjectID != run.Target.Revision.BaseObjectID
	if !moved {
		return false, nil
	}
	if appendErr := appendStaleEvent(dataDir, key, run, snapshot.HeadObjectID, snapshot.BaseObjectID); appendErr != nil {
		return true, appendErr
	}
	return true, nil
}

func appendStaleEvent(dataDir string, key store.PullRequestKeyV1, run *domain.ReviewRun, currentHeadObjectID, currentBaseObjectID string) error {
	now := time.Now()
	id, err := newDeskEventID(now)
	if err != nil {
		return err
	}
	event := store.ReviewEventV1{
		SchemaVersion:     store.CurrentSchemaVersion,
		ID:                id,
		PullRequest:       key,
		Type:              store.EventTypeReviewStale,
		OccurredAt:        now,
		RunID:             run.ID,
		HeadObjectID:      currentHeadObjectID,
		BaseObjectID:      currentBaseObjectID,
		PriorHeadObjectID: run.Target.Revision.HeadObjectID,
	}
	_, err = store.NewFilesystemEventStore(dataDir).AppendEvent(event)
	return err
}

func revalidateDeskEligibility(ctx context.Context, configDir, dataDir string, key store.PullRequestKeyV1, discovery github.Discovery) (string, error) {
	cfg, err := workspace.Load(configDir)
	if err != nil {
		return "", fmt.Errorf("could not reload workspace configuration: %w", err)
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return "", fmt.Errorf("workspace configuration has %d error(s): %v", len(errs), errs)
	}
	if err := cfg.RequirePosting(); err != nil {
		return "", err
	}
	if err := workspace.CheckIdentity(ctx, *cfg); err != nil {
		return "", err
	}
	inbox, err := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now())
	if err != nil {
		return "", fmt.Errorf("could not re-check desk eligibility: %w", err)
	}
	item, ok := findDeskItem(inbox, key)
	if ok && (item.DeskState == domain.DeskStateStale || item.DeskState == domain.DeskStateNeedsRereview) {
		return "", errRevisionMovedSinceReview
	}
	if !ok || (item.DeskState != domain.DeskStateFindingsReady && item.DeskState != domain.DeskStateDecisionReady) {
		return "", fmt.Errorf("%s is no longer eligible for this action under the current workspace configuration", key.String())
	}
	return cfg.Identity.ExpectedUser, nil
}

func newDeskEventID(now time.Time) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random[:]), nil
}
