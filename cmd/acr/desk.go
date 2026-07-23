package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/richhaase/agentic-code-reviewer/internal/desk"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

func newDeskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desk",
		Short: "Inspect and manage the persistent review workspace",
		Long:  "View and manage locally stored pull request review history.",
	}

	cmd.AddCommand(newDeskHistoryCmd())
	cmd.AddCommand(newDeskForgetCmd())

	return cmd
}

func newDeskHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <[host/]owner/repo#number>",
		Short: "Show the chronological review history for a pull request",
		Long:  "Render every stored run, event, adjudication, loop decision, and economics record for a pull request as one chronological timeline.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := desk.ParsePullRequestRef(args[0])
			if err != nil {
				return err
			}
			dataDir, err := store.DataDir()
			if err != nil {
				return err
			}
			history, err := desk.LoadHistory(dataDir, key)
			if err != nil {
				return err
			}
			renderHistory(history)
			return nil
		},
	}
}

func newDeskForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <[host/]owner/repo#number>",
		Short: "Permanently delete a pull request's stored review history",
		Long:  "Delete every stored run, event, adjudication, loop decision, economics record, and snapshot for a pull request. Refuses while another acr process owns the workspace.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := desk.ParsePullRequestRef(args[0])
			if err != nil {
				return err
			}
			dataDir, err := store.DataDir()
			if err != nil {
				return err
			}
			lock, err := store.AcquireWriteLock(dataDir)
			if err != nil {
				if errors.Is(err, store.ErrWriterLocked) {
					return fmt.Errorf("cannot forget %s: %w", key.String(), err)
				}
				return err
			}

			report, forgetErr := store.ForgetPullRequest(dataDir, key)
			if releaseErr := lock.Release(); releaseErr != nil && forgetErr == nil {
				return releaseErr
			}
			if forgetErr != nil {
				return forgetErr
			}
			renderForgetReport(report)
			return nil
		},
	}
}

func renderHistory(history desk.History) {
	if len(history.Entries) == 0 {
		fmt.Printf("No stored history found for %s.\n", history.PullRequest.String())
	} else {
		fmt.Printf("History for %s (%d record(s)):\n\n", history.PullRequest.String(), len(history.Entries))
		for _, entry := range history.Entries {
			renderTimelineEntry(entry)
		}
	}

	if len(history.Corrupt) > 0 {
		fmt.Printf("\n%d record(s) could not be read and are not shown above:\n", len(history.Corrupt))
		for _, corrupt := range history.Corrupt {
			fmt.Printf("  - %s: %v\n", corrupt.Path, corrupt.Err)
		}
	}
}

func renderTimelineEntry(entry desk.TimelineEntry) {
	timestamp := entry.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
	switch entry.Kind {
	case desk.TimelineEntryEvent:
		renderEventEntry(timestamp, entry.Event)
	case desk.TimelineEntryRun:
		renderRunEntry(timestamp, entry.Run)
	case desk.TimelineEntryAdjudication:
		renderAdjudicationEntry(timestamp, entry.Adjudication)
	case desk.TimelineEntryLoopDecision:
		renderLoopDecisionEntry(timestamp, entry.LoopDecision)
	case desk.TimelineEntryEconomics:
		renderEconomicsEntry(timestamp, entry.Economics)
	}
}

func renderEventEntry(timestamp string, event *store.ReviewEventV1) {
	fmt.Printf("%s  event          %s", timestamp, event.Type)
	if event.RunID != "" {
		fmt.Printf("  run=%s", event.RunID)
	}
	if event.HeadObjectID != "" {
		fmt.Printf("  head=%s", event.HeadObjectID)
	}
	if event.PriorHeadObjectID != "" {
		fmt.Printf("  prior_head=%s", event.PriorHeadObjectID)
	}
	if event.FindingID != "" {
		fmt.Printf("  finding=%s", event.FindingID)
	}
	if event.Actor != "" {
		fmt.Printf("  actor=%s", event.Actor)
	}
	fmt.Println()
	if event.Reason != "" {
		fmt.Printf("                            reason: %s\n", event.Reason)
	}
	if event.Message != "" {
		fmt.Printf("                            message: %s\n", event.Message)
	}
}

func renderRunEntry(timestamp string, run *store.ReviewRunV1) {
	fmt.Printf("%s  run            %s  %s/%s  engine=%s/%s  head=%s\n",
		timestamp, run.ID, run.Status, orNone(run.Conclusion), run.Engine.Name, run.Engine.Version, run.Target.Revision.HeadObjectID)
	if run.Failure != nil {
		fmt.Printf("                            failure: %s: %s\n", run.Failure.Phase, run.Failure.Message)
	}
	fmt.Printf("                            findings: raw=%d aggregated=%d fp_filtered=%d exclude_filtered=%d final=%d info=%d\n",
		len(run.RawFindings), len(run.AggregatedFindings), len(run.FalsePositiveFilter.Removed), len(run.ExcludeFilter.Removed), len(run.Findings), len(run.Info))
}

func renderAdjudicationEntry(timestamp string, record *store.AdjudicationRecordV1) {
	ref := record.FindingRef.FindingID
	if ref == "" {
		ref = record.FindingRef.ClusterID
	}
	fmt.Printf("%s  adjudication   finding=%s  disposition=%s  actor=%s:%s",
		timestamp, ref, record.Disposition, record.DecidingActor.Kind, record.DecidingActor.Identity)
	if record.RelationToPrior != store.AdjudicationRelationNone {
		fmt.Printf("  relation=%s  supersedes=%s", record.RelationToPrior, record.SupersedesRecordID)
	}
	fmt.Println()
	fmt.Printf("                            rationale: %s\n", record.Rationale)
	fmt.Printf("                            scope: head=%s config=%s\n", record.Scope.HeadObjectID, record.Scope.ConfigurationFingerprint)
	if len(record.InvalidationConditions) > 0 {
		fmt.Printf("                            invalidation conditions: %s\n", strings.Join(record.InvalidationConditions, "; "))
	}
}

func renderLoopDecisionEntry(timestamp string, decision *store.LoopDecisionV1) {
	fmt.Printf("%s  loop_decision  %s  iteration=%d  reason=%s\n",
		timestamp, decision.Decision, decision.IterationCount, decision.Reason)
	if decision.Budget.Known {
		fmt.Printf("                            budget: iterations %d/%d  cost $%.2f/$%.2f\n",
			decision.Budget.IterationsUsed, decision.Budget.IterationsLimit, decision.Budget.CostUSDUsed, decision.Budget.CostUSDLimit)
	} else {
		fmt.Println("                            budget: unknown")
	}
	if len(decision.SupportingAdjudicationIDs) > 0 {
		fmt.Printf("                            supporting adjudications: %s\n", strings.Join(decision.SupportingAdjudicationIDs, ", "))
	}
}

func renderEconomicsEntry(timestamp string, record *store.EconomicsRecordV1) {
	economics := record.Economics
	fmt.Printf("%s  economics      run=%s  reviewer_calls=%d  model_calls=%d  duration=%s\n",
		timestamp, economics.RunID, economics.ReviewerCallCount, economics.ModelCallCount, economics.Duration)
	for _, usage := range economics.ProviderUsage {
		if usage.Usage.Known {
			fmt.Printf("                            %s/%s: tokens=%d cost=$%.4f\n", usage.Provider, usage.Model, usage.Usage.TotalTokens, usage.Usage.CostUSD)
		} else {
			fmt.Printf("                            %s/%s: usage unknown\n", usage.Provider, usage.Model)
		}
	}
}

func orNone(conclusion string) string {
	if conclusion == "" {
		return "none"
	}
	return conclusion
}

func renderForgetReport(report store.ForgetReport) {
	if !report.Existed {
		fmt.Printf("No stored history found for %s.\n", report.PullRequest.String())
		return
	}
	fmt.Printf("Removed stored history for %s:\n", report.PullRequest.String())
	fmt.Printf("  snapshot:        %t\n", report.SnapshotRemoved)
	fmt.Printf("  runs:            %d\n", report.RunsRemoved)
	fmt.Printf("  events:          %d\n", report.EventsRemoved)
	fmt.Printf("  adjudications:   %d\n", report.AdjudicationsRemoved)
	fmt.Printf("  loop decisions:  %d\n", report.LoopDecisionsRemoved)
	fmt.Printf("  economics:       %d\n", report.EconomicsRemoved)
}
