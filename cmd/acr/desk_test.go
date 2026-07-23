package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	func() {
		os.Stdout = writer
		defer func() { os.Stdout = original }()
		run()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDeskHistory_NoStoredHistory(t *testing.T) {
	t.Setenv("ACR_DATA_DIR", t.TempDir())

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"history", "richhaase/agentic-code-reviewer#198"})

	var runErr error
	output := captureStdout(t, func() {
		runErr = cmd.Execute()
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !strings.Contains(output, "No stored history found for github.com/richhaase/agentic-code-reviewer#198") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestDeskHistory_CorruptOnlyHistoryIsNotReportedAsMissing(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)

	key := store.PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 198}
	event := store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "event-good",
		PullRequest:   key,
		Type:          store.EventTypePRDiscovered,
		OccurredAt:    time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
	}
	path, err := store.NewFilesystemEventStore(dataDir).AppendEvent(event)
	if err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the only readable record, leaving a corrupt-only history: %v", err)
	}
	corruptPath := filepath.Join(filepath.Dir(path), "20260722T120000.000000000Z-event-corrupt.json")
	if err := os.WriteFile(corruptPath, []byte(`{"schema_version": 1, "id": "event-corrupt", "truncated`), 0o600); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"history", "richhaase/agentic-code-reviewer#198"})
	var runErr error
	output := captureStdout(t, func() {
		runErr = cmd.Execute()
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}

	if strings.Contains(output, "No stored history found") {
		t.Fatalf("a corrupt-only history must not be reported as missing, got:\n%s", output)
	}
	if !strings.Contains(output, "could not be read") {
		t.Fatalf("expected the corrupt record to be reported, got:\n%s", output)
	}
}

func TestDeskHistory_RejectsInvalidRef(t *testing.T) {
	t.Setenv("ACR_DATA_DIR", t.TempDir())

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"history", "not-a-valid-ref"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an invalid pull request reference")
	}
}

func TestDeskHistory_RendersStoredRecordsHonestly(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)

	key := store.PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 198}
	recordedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	economics := store.ReviewEconomicsV1{
		SchemaVersion:     store.CurrentSchemaVersion,
		RunID:             "run-1",
		ReviewerCallCount: 2,
		ModelCallCount:    2,
		ProviderUsage: []store.ProviderUsageRecordV1{
			{Provider: "anthropic", Model: "claude-opus", Usage: store.ProviderUsageV1{Known: true, TotalTokens: 100, CostUSD: 0.05}},
			{Provider: "codex", Model: "gpt", Usage: store.ProviderUsageV1{Known: false}},
		},
	}
	if _, err := store.NewFilesystemEconomicsStore(dataDir).SaveEconomics(key, recordedAt, economics); err != nil {
		t.Fatalf("save economics: %v", err)
	}

	adjudication := store.AdjudicationRecordV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "adjudication-1",
		FindingRef:    store.AdjudicationFindingRefV1{FindingID: "finding-1"},
		Disposition:   store.AdjudicationFalsePositive,
		DecidingActor: store.AdjudicationActorV1{Kind: store.AdjudicationActorHuman, Identity: "richhaase"},
		Rationale:     "not actually a bug",
		Scope: store.AdjudicationScopeV1{
			PullRequest:              key,
			HeadObjectID:             "headsha",
			ConfigurationFingerprint: "sha256:abc",
		},
		InvalidationConditions: []string{"head changes"},
		RecordedAt:             recordedAt.Add(time.Minute),
	}
	if _, err := store.NewFilesystemAdjudicationStore(dataDir).SaveAdjudication(adjudication); err != nil {
		t.Fatalf("save adjudication: %v", err)
	}

	decision := store.LoopDecisionV1{
		SchemaVersion:  store.CurrentSchemaVersion,
		ID:             "loop-1",
		PullRequest:    key,
		RunID:          "run-1",
		Decision:       store.LoopDecisionStop,
		Reason:         "clean run",
		IterationCount: 1,
		DecidedAt:      recordedAt.Add(2 * time.Minute),
	}
	if _, err := store.NewFilesystemLoopDecisionStore(dataDir).SaveLoopDecision(decision); err != nil {
		t.Fatalf("save loop decision: %v", err)
	}

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"history", "richhaase/agentic-code-reviewer#198"})
	output := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	for _, want := range []string{
		"disposition=false_positive",
		"not actually a bug",
		"head changes",
		"tokens=100 cost=$0.0500",
		"usage unknown",
		"budget: unknown",
		"clean run",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "cost=$0.0000") {
		t.Fatalf("expected unknown usage to render as unknown, not zero, got:\n%s", output)
	}
}

func TestDeskForget_RefusesWhileWriterLockHeld(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)

	lock, err := store.AcquireWriteLock(dataDir)
	if err != nil {
		t.Fatalf("acquire write lock: %v", err)
	}
	defer lock.Release()

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"forget", "richhaase/agentic-code-reviewer#198"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected forget to refuse while another process holds the write lock")
	}
}

func TestDeskForget_RemovesHistoryAndReportsIt(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ACR_DATA_DIR", dataDir)

	key := store.PullRequestKeyV1{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 198}
	event := store.ReviewEventV1{
		SchemaVersion: store.CurrentSchemaVersion,
		ID:            "event-1",
		PullRequest:   key,
		Type:          store.EventTypePRDiscovered,
		OccurredAt:    time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
	}
	if _, err := store.NewFilesystemEventStore(dataDir).AppendEvent(event); err != nil {
		t.Fatalf("append event: %v", err)
	}

	cmd := newDeskCmd()
	cmd.SetArgs([]string{"forget", "richhaase/agentic-code-reviewer#198"})
	output := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(output, "Removed stored history for github.com/richhaase/agentic-code-reviewer#198") {
		t.Fatalf("unexpected output: %q", output)
	}
	if !strings.Contains(output, "events:          1") {
		t.Fatalf("expected events removed count in output, got: %q", output)
	}

	events, _, err := store.NewFilesystemEventStore(dataDir).ListEvents(key)
	if err != nil {
		t.Fatalf("list events after forget: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events remaining after forget, got %+v", events)
	}
}
