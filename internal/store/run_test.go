package store

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func buildTestReviewRun(t *testing.T, status domain.ReviewStatus) domain.ReviewRun {
	t.Helper()

	pr := domain.PullRequestKey{Host: "github.com", Owner: "richhaase", Repository: "agentic-code-reviewer", Number: 196}
	cfg := newTestReviewConfiguration(t)

	run := domain.ReviewRun{
		ID:      "run-1",
		Trigger: domain.ReviewTriggerManual,
		Target: domain.ReviewTarget{
			RepositoryRoot: "/repo",
			WorktreeRoot:   "/worktree",
			Revision: domain.RevisionEvidence{
				RequestedBaseRef: "main",
				ResolvedBaseRef:  "refs/remotes/origin/main",
				HeadObjectID:     "headsha",
				BaseObjectID:     "basesha",
			},
			PullRequest: &pr,
		},
		Engine:        domain.ReviewEngine{Name: "acr", Version: "1.2.3"},
		StartedAt:     time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		Configuration: cfg,
		ConfigurationSource: domain.ConfigurationSourceIdentity{
			Kind:          "repository-revision",
			Locator:       "/repo",
			Ref:           "refs/acr/trusted-config/origin/main",
			Revision:      "trustedsha",
			ConfigPresent: true,
			ConfigDigest:  "digest",
		},
		ConfigurationFingerprint: cfg.Fingerprint(),
		ReviewerResults: []domain.ReviewerResult{
			{
				ReviewerID: 0,
				AgentName:  "claude",
				Findings:   []domain.Finding{{Text: "possible nil deref", ReviewerID: 0}},
				ExitCode:   0,
				Attempts:   1,
				Duration:   3 * time.Second,
			},
			{
				ReviewerID: 1,
				AgentName:  "codex",
				Findings:   []domain.Finding{{Text: "possible nil deref", ReviewerID: 1}},
				ExitCode:   0,
				Attempts:   1,
				Duration:   4 * time.Second,
			},
		},
		Stats: domain.ReviewStats{
			TotalReviewers:      2,
			SuccessfulReviewers: 2,
			ReviewerDurations:   map[int]time.Duration{0: 3 * time.Second, 1: 4 * time.Second},
			ReviewerAgentNames:  map[int]string{0: "claude", 1: "codex"},
			WallClockDuration:   4 * time.Second,
			SummarizerDuration:  time.Second,
			FPFilterDuration:    time.Second,
		},
		Summarizer: domain.SummarizerOutcome{
			ExitCode: 0,
			Duration: time.Second,
			Warnings: []string{"cleanup warning"},
		},
		RawFindings: []domain.Finding{
			{Text: "possible nil deref", ReviewerID: 0},
			{Text: "possible nil deref", ReviewerID: 1},
		},
		AggregatedFindings: []domain.AggregatedFinding{
			{Text: "possible nil deref", Reviewers: []int{0, 1}},
		},
		PreFilterSummary: domain.GroupedFindings{
			Findings: []domain.FindingGroup{{Title: "Possible nil deref", ReviewerCount: 2, Sources: []int{0}}},
		},
		FindingRecords: []domain.ReviewFinding{
			{
				ID:          "finding-1",
				Kind:        domain.ReviewFindingActionable,
				Group:       domain.FindingGroup{Title: "Possible nil deref", ReviewerCount: 2, Sources: []int{0}},
				Disposition: domain.Disposition{Kind: domain.DispositionSurvived, GroupTitle: "Possible nil deref"},
			},
		},
		Findings: []domain.ReviewFinding{
			{
				ID:          "finding-1",
				Kind:        domain.ReviewFindingActionable,
				Group:       domain.FindingGroup{Title: "Possible nil deref", ReviewerCount: 2, Sources: []int{0}},
				Disposition: domain.Disposition{Kind: domain.DispositionSurvived, GroupTitle: "Possible nil deref"},
			},
		},
		FalsePositiveFilter: domain.FalsePositiveFilterOutcome{
			Enabled:  true,
			Applied:  true,
			Duration: time.Second,
		},
		ExcludeFilter: domain.ExcludeFilterOutcome{
			Patterns: []string{"vendor/.*"},
		},
		Dispositions: map[int]domain.Disposition{
			0: {Kind: domain.DispositionSurvived, GroupTitle: "Possible nil deref"},
		},
	}

	switch status {
	case domain.ReviewStatusCompleted:
		run.Status = domain.ReviewStatusCompleted
		run.Conclusion = domain.ReviewConclusionFindings
		run.CompletedAt = run.StartedAt.Add(10 * time.Second)
	case domain.ReviewStatusFailed:
		run.Status = domain.ReviewStatusFailed
		run.CompletedAt = run.StartedAt.Add(2 * time.Second)
		run.Failure = &domain.ReviewFailure{Phase: domain.ReviewPhaseReviewers, Message: "all reviewers failed"}
	case domain.ReviewStatusInterrupted:
		run.Status = domain.ReviewStatusInterrupted
		run.CompletedAt = run.StartedAt.Add(1 * time.Second)
	}

	return run
}

func TestReviewRunV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		status domain.ReviewStatus
	}{
		{name: "completed run with findings", status: domain.ReviewStatusCompleted},
		{name: "failed run", status: domain.ReviewStatusFailed},
		{name: "interrupted run", status: domain.ReviewStatusInterrupted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := buildTestReviewRun(t, tt.status)
			rendered := RenderedOutcomeV1{ReviewBody: "review body", LGTMBody: "lgtm body"}

			schema, err := ToReviewRunSchema(run, rendered)
			if err != nil {
				t.Fatalf("ToReviewRunSchema: %v", err)
			}

			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ReviewRunV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			gotRun, gotRendered, err := FromReviewRunSchema(decoded)
			if err != nil {
				t.Fatalf("FromReviewRunSchema: %v", err)
			}

			if gotRendered != rendered {
				t.Fatalf("rendered outcome mismatch: got %+v, want %+v", gotRendered, rendered)
			}
			if gotRun.ID != run.ID || gotRun.Status != run.Status || gotRun.Conclusion != run.Conclusion {
				t.Fatalf("run identity mismatch: got %+v, want %+v", gotRun, run)
			}
			if (gotRun.Failure == nil) != (run.Failure == nil) {
				t.Fatalf("failure presence mismatch: got %+v, want %+v", gotRun.Failure, run.Failure)
			}
			if gotRun.Failure != nil && *gotRun.Failure != *run.Failure {
				t.Fatalf("failure mismatch: got %+v, want %+v", *gotRun.Failure, *run.Failure)
			}
			if gotRun.ConfigurationFingerprint != run.ConfigurationFingerprint {
				t.Fatalf("configuration fingerprint mismatch: got %s, want %s", gotRun.ConfigurationFingerprint, run.ConfigurationFingerprint)
			}
			if gotRun.ConfigurationSource != run.ConfigurationSource {
				t.Fatalf("configuration source mismatch: got %+v, want %+v", gotRun.ConfigurationSource, run.ConfigurationSource)
			}
			if !reflect.DeepEqual(gotRun.FinalGroupedFindings(), run.FinalGroupedFindings()) {
				t.Fatalf("rendered findings differ after round trip:\ngot  %+v\nwant %+v", gotRun.FinalGroupedFindings(), run.FinalGroupedFindings())
			}
			if len(gotRun.ReviewerResults) != len(run.ReviewerResults) {
				t.Fatalf("reviewer results length mismatch: got %d, want %d", len(gotRun.ReviewerResults), len(run.ReviewerResults))
			}
			if !reflect.DeepEqual(gotRun.Dispositions, run.Dispositions) {
				t.Fatalf("dispositions mismatch: got %+v, want %+v", gotRun.Dispositions, run.Dispositions)
			}
		})
	}
}

func TestReviewRunV1_PreservesSummarizerDiagnosticsForFailedRuns(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusFailed)
	run.Summarizer.Stderr = "summarizer stderr: connection reset\n[truncated]"
	run.Summarizer.DiagnosticOutput = "partial summarizer stdout before failure\n[truncated]"

	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	if schema.Summarizer.Stderr != run.Summarizer.Stderr {
		t.Fatalf("schema stderr = %q, want %q", schema.Summarizer.Stderr, run.Summarizer.Stderr)
	}
	if schema.Summarizer.DiagnosticOutput != run.Summarizer.DiagnosticOutput {
		t.Fatalf("schema diagnostic output = %q, want %q", schema.Summarizer.DiagnosticOutput, run.Summarizer.DiagnosticOutput)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewRunV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	gotRun, _, err := FromReviewRunSchema(decoded)
	if err != nil {
		t.Fatalf("FromReviewRunSchema: %v", err)
	}
	if gotRun.Summarizer.Stderr != run.Summarizer.Stderr {
		t.Fatalf("a failed run's stored summarizer stderr must survive a restart: got %q, want %q", gotRun.Summarizer.Stderr, run.Summarizer.Stderr)
	}
	if gotRun.Summarizer.DiagnosticOutput != run.Summarizer.DiagnosticOutput {
		t.Fatalf("a failed run's stored summarizer diagnostic output must survive a restart: got %q, want %q", gotRun.Summarizer.DiagnosticOutput, run.Summarizer.DiagnosticOutput)
	}
}

func TestReviewRunV1_RejectsUnsupportedSchemaVersion(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	schema.SchemaVersion = CurrentSchemaVersion + 1

	if _, _, err := FromReviewRunSchema(schema); err == nil {
		t.Fatal("expected an explicit error for an unsupported schema version")
	}

	schema.SchemaVersion = 0
	if _, _, err := FromReviewRunSchema(schema); err == nil {
		t.Fatal("expected an explicit error for a zero schema version")
	}
}

func TestReviewRunV1_RejectsCorruptedConfigurationFingerprint(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	schema.Configuration.Fingerprint = "sha256:corrupted"

	if _, _, err := FromReviewRunSchema(schema); err == nil {
		t.Fatal("expected an explicit error for a corrupted configuration fingerprint")
	}
}

func TestReviewRunV1_RejectsMismatchedTopLevelConfigurationFingerprint(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	schema.ConfigurationFingerprint = "sha256:top-level-mismatch"

	if _, _, err := FromReviewRunSchema(schema); err == nil {
		t.Fatal("expected an explicit error when the top-level configuration_fingerprint disagrees with the nested configuration fingerprint")
	}
}

func TestReviewRunV1_RejectsUnknownStatus(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}
	schema.Status = "abandoned"

	if _, _, err := FromReviewRunSchema(schema); err == nil {
		t.Fatal("expected an explicit error for an unknown stored review status")
	}
}

func TestReviewRunV1_StaleAndSupersededTransitionsAreSeparateEventsNotRunMutations(t *testing.T) {
	run := buildTestReviewRun(t, domain.ReviewStatusCompleted)
	schema, err := ToReviewRunSchema(run, RenderedOutcomeV1{})
	if err != nil {
		t.Fatalf("ToReviewRunSchema: %v", err)
	}

	staleEvent := ReviewEventV1{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "event-stale-1",
		PullRequest:   testPullRequestKey(),
		Type:          EventTypeReviewStale,
		OccurredAt:    run.CompletedAt.Add(time.Hour),
		RunID:         schema.ID,
		Reason:        "head moved",
	}
	if err := staleEvent.Validate(); err != nil {
		t.Fatalf("stale event Validate: %v", err)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewRunV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Status != string(domain.ReviewStatusCompleted) {
		t.Fatalf("a later stale/superseded event must not rewrite the run's original terminal status: got %q", decoded.Status)
	}

	gotRun, _, err := FromReviewRunSchema(decoded)
	if err != nil {
		t.Fatalf("FromReviewRunSchema: %v", err)
	}
	if gotRun.Status != domain.ReviewStatusCompleted {
		t.Fatalf("original outcome not preserved: got %q", gotRun.Status)
	}
}
