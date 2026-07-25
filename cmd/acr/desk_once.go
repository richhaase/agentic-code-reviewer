package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

func runDeskOnce(jsonOutput bool) error {
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

	ctx := context.Background()
	if err := workspace.CheckIdentity(ctx, *cfg); err != nil {
		return err
	}

	dataDir, err := store.DataDir()
	if err != nil {
		return err
	}
	now := time.Now()

	lock, lockErr := store.AcquireWriteLock(dataDir)
	if lockErr != nil {
		if !errors.Is(lockErr, store.ErrWriterLocked) {
			return lockErr
		}
		inbox, err := desk.LoadStored(dataDir, *cfg, now)
		if err != nil {
			return err
		}
		renderInbox(inbox, jsonOutput, now)
		return nil
	}

	inbox, refreshErr := desk.Refresh(ctx, *cfg, dataDir, github.NewDiscovery(), now)
	if releaseErr := lock.Release(); releaseErr != nil && refreshErr == nil {
		return releaseErr
	}
	if refreshErr != nil {
		return refreshErr
	}
	renderInbox(inbox, jsonOutput, now)
	return nil
}

func renderInbox(inbox desk.Inbox, jsonOutput bool, now time.Time) {
	if jsonOutput {
		data, err := json.MarshalIndent(inbox, "", "  ")
		if err != nil {
			fmt.Printf("failed to render inbox as JSON: %v\n", err)
			return
		}
		fmt.Println(string(data))
		return
	}

	if inbox.FromLiveLock {
		fmt.Println("Another acr process owns the workspace; showing the last stored snapshot without refreshing.")
	}
	fmt.Printf("Desk snapshot at %s\n\n", inbox.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"))

	if len(inbox.Items) == 0 {
		fmt.Println("Nothing needs your attention.")
	} else {
		grouped := groupItemsByState(inbox.Items)
		for _, group := range desk.DeskStateDisplayOrder {
			items := grouped[group]
			if len(items) == 0 {
				continue
			}
			fmt.Printf("%s (%d)\n", deskStateLabels[group], len(items))
			for _, item := range items {
				renderInboxItem(item, now)
			}
			fmt.Println()
		}
	}

	if len(inbox.Warnings) > 0 {
		fmt.Printf("%d warning(s):\n", len(inbox.Warnings))
		for _, warning := range inbox.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}
}

func renderInboxItem(item desk.Item, now time.Time) {
	fmt.Printf("  %s\n", item.Title)
	fmt.Printf("    %s\n", item.URL)
	fmt.Printf("    %s", item.Reason)
	if item.RepositoryPath != "" {
		fmt.Printf("  repo=%s", item.RepositoryPath)
	}
	if item.SnapshotStale {
		fmt.Printf("  (stale snapshot, GitHub was unreachable)")
	}
	age := item.SnapshotAge(now)
	fmt.Printf("  age=%s\n", terminal.FormatDuration(age))
}

func groupItemsByState(items []desk.Item) map[domain.DeskState][]desk.Item {
	grouped := make(map[domain.DeskState][]desk.Item)
	for _, item := range items {
		grouped[item.DeskState] = append(grouped[item.DeskState], item)
	}
	return grouped
}

var deskStateLabels = map[domain.DeskState]string{
	domain.DeskStateFailed:                "Failed",
	domain.DeskStateFindingsReady:         "Findings ready",
	domain.DeskStateDecisionReady:         "Decision ready",
	domain.DeskStateNeedsReview:           "Needs review",
	domain.DeskStateNeedsRereview:         "Needs re-review",
	domain.DeskStateRunning:               "Running",
	domain.DeskStateQueued:                "Queued",
	domain.DeskStateStale:                 "Stale (settling)",
	domain.DeskStateWaitingOnOthers:       "Waiting on others",
	domain.DeskStateRepositoryUnavailable: "Repository unavailable",
	domain.DeskStateResolved:              "Resolved",
}
