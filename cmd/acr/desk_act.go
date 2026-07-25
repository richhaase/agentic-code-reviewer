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

	run, err := loadCurrentRun(dataDir, item)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("%s: no stored review run matches the current head", key.String())
	}
	if actionErr := validateActionForConclusion(action, run.Conclusion); actionErr != nil {
		return actionErr
	}

	prNumber := strconv.Itoa(item.Key.Number)
	logger := terminal.NewLogger()
	outcome := &CycleOutcome{}
	opts := ReviewOpts{
		RepositoryRoot:   item.RepositoryPath,
		PRNumber:         prNumber,
		AutoYes:          autoYes,
		ForcePostComment: action == deskActComment,
		ExpectedHeadSHA:  run.Target.Revision.HeadObjectID,
		Outcome:          outcome,
	}

	handleTypedReviewRun(ctx, opts, run, logger)

	var recordErr error
	switch outcome.Kind {
	case OutcomeLGTMApproved:
		recordErr = appendActionEvent(dataDir, key, store.EventTypeActionApprovalPosted, run, cfg.Identity.ExpectedUser)
	case OutcomeLGTMComment, OutcomeReviewComment:
		recordErr = appendActionEvent(dataDir, key, store.EventTypeActionCommentPosted, run, cfg.Identity.ExpectedUser)
	case OutcomeReviewRequestChanges:
		recordErr = appendActionEvent(dataDir, key, store.EventTypeActionRequestChangesPosted, run, cfg.Identity.ExpectedUser)
	case OutcomeStaleHead:
		recordErr = recordStaleResult(ctx, dataDir, key, run, discovery)
	}

	refreshed, refreshErr := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now())

	if releaseErr := release(); releaseErr != nil {
		return releaseErr
	}

	if recordErr != nil {
		return fmt.Errorf("action completed but could not be recorded: %w", recordErr)
	}

	if refreshErr == nil {
		if resultItem, found := findDeskItem(refreshed, key); found {
			fmt.Printf("\nDesk state: %s — %s\n", resultItem.DeskState, resultItem.Reason)
		}
	}

	switch outcome.Kind {
	case OutcomeStaleHead:
		return errors.New("the pull request head moved since this review; nothing was posted")
	case OutcomeReviewSkipped, OutcomeLGTMDeclined, OutcomeLGTMSkipped:
		return errors.New("action was not posted")
	}
	return nil
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
	schemas, _, err := store.NewFilesystemRunStore(dataDir).ListRuns(item.Key)
	if err != nil {
		return nil, err
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

func recordStaleResult(ctx context.Context, dataDir string, key store.PullRequestKeyV1, run *domain.ReviewRun, discovery github.Discovery) error {
	snapshot, err := discovery.Enrich(ctx, key.ToDomain())
	if err != nil {
		return fmt.Errorf("re-check current PR head: %w", err)
	}
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
		HeadObjectID:      snapshot.HeadObjectID,
		PriorHeadObjectID: run.Target.Revision.HeadObjectID,
	}
	_, err = store.NewFilesystemEventStore(dataDir).AppendEvent(event)
	return err
}

func newDeskEventID(now time.Time) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random[:]), nil
}
