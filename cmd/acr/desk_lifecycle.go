package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func newDeskResolveCmd() *cobra.Command {
	return newDeskLifecycleCmd("resolve", "Mark a pull request's current revision as resolved",
		"Mark the pull request's current head as resolved without posting to GitHub. "+
			"The item drops out of the actionable desk view until a new head arrives.",
		store.EventTypeUserResolved)
}

func newDeskReleaseCmd() *cobra.Command {
	return newDeskLifecycleCmd("release", "Stop tracking a pull request on the desk",
		"Release a pull request from the desk workspace entirely. It will reappear only if discovery finds it again.",
		store.EventTypeUserReleased)
}

func newDeskResumeCmd() *cobra.Command {
	return newDeskLifecycleCmd("resume", "Resume tracking a released or snoozed pull request",
		"Cancel a prior release or snooze so the pull request is tracked and re-reviewed normally again.",
		store.EventTypeUserResumed)
}

func newDeskSnoozeCmd() *cobra.Command {
	return newDeskLifecycleCmd("snooze", "Defer re-review of a pull request until resumed",
		"Suppress automatic re-review for this pull request until `acr desk resume` is run, "+
			"without releasing it from the desk workspace.",
		store.EventTypeUserSnoozed)
}

func newDeskLifecycleCmd(use, short, long string, eventType store.ReviewEventTypeV1) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <[host/]owner/repo#number>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := desk.ParsePullRequestRef(args[0])
			if err != nil {
				return err
			}
			return runDeskLifecycleAction(context.Background(), key, eventType, github.NewDiscovery())
		},
	}
}

func runDeskLifecycleAction(ctx context.Context, key store.PullRequestKeyV1, eventType store.ReviewEventTypeV1, discovery github.Discovery) error {
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

	dataDir, err := store.DataDir()
	if err != nil {
		return err
	}

	lock, err := store.AcquireWriteLock(dataDir)
	if err != nil {
		if errors.Is(err, store.ErrWriterLocked) {
			return fmt.Errorf("cannot update %s: %w", key.String(), err)
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

	if _, err := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now()); err != nil {
		return err
	}

	storedSnapshot, snapErr := store.NewFilesystemSnapshotStore(dataDir).LoadSnapshot(key)
	if snapErr != nil {
		return fmt.Errorf("%s is not known to the desk; run `acr desk --once` first", key.String())
	}

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
		Actor:         cfg.Identity.ExpectedUser,
	}
	if eventType == store.EventTypeUserResolved {
		event.HeadObjectID = storedSnapshot.HeadObjectID
		event.BaseObjectID = storedSnapshot.BaseObjectID
	}
	if _, appendErr := store.NewFilesystemEventStore(dataDir).AppendEvent(event); appendErr != nil {
		return appendErr
	}

	refreshed, refreshErr := desk.Refresh(ctx, *cfg, dataDir, discovery, time.Now())

	if releaseErr := release(); releaseErr != nil {
		return releaseErr
	}

	if refreshErr != nil {
		fmt.Printf("warning: could not refresh desk state: %v\n", refreshErr)
		return nil
	}
	if resultItem, found := findDeskItem(refreshed, key); found {
		fmt.Printf("Desk state: %s — %s\n", resultItem.DeskState, resultItem.Reason)
	} else {
		fmt.Printf("%s is no longer tracked on the desk.\n", key.String())
	}
	return nil
}
