package review

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type mockReviewAgent struct {
	name                string
	review              func(context.Context, *agent.ReviewConfig) (string, int, string, error)
	summary             func(context.Context, int64, string, []byte) (string, int, string, error)
	reviewClose         error
	summaryClose        error
	summaryCloseForCall func(int64) error
	summaryConfigs      chan agent.SummaryConfig
	summaryCalls        atomic.Int64
}

type reviewErrorReadCloser struct {
	io.Reader
	err error
}

func (r *reviewErrorReadCloser) Close() error {
	return r.err
}

func (m *mockReviewAgent) Name() string {
	return m.name
}

func (m *mockReviewAgent) IsAvailable() error {
	return nil
}

func (m *mockReviewAgent) ExecuteReview(ctx context.Context, config *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	output := codexReviewOutput("src/service.go:10: missing validation")
	exitCode := 0
	stderr := ""
	if m.review != nil {
		var err error
		output, exitCode, stderr, err = m.review(ctx, config)
		if err != nil {
			return nil, err
		}
	}
	if m.reviewClose != nil {
		reader := &reviewErrorReadCloser{Reader: strings.NewReader(output), err: m.reviewClose}
		return agent.NewExecutionResult(reader, func() int { return exitCode }, func() string { return stderr }), nil
	}
	return executionResult(output, exitCode, stderr), nil
}

func (m *mockReviewAgent) ExecuteSummary(ctx context.Context, config *agent.SummaryConfig) (*agent.ExecutionResult, error) {
	call := m.summaryCalls.Add(1)
	if m.summaryConfigs != nil {
		m.summaryConfigs <- *config
	}
	output := codexSummaryOutput(`{"findings":[{"title":"Missing validation","summary":"Input is not checked.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`)
	exitCode := 0
	stderr := ""
	if m.summary != nil {
		var err error
		output, exitCode, stderr, err = m.summary(ctx, call, config.Prompt, config.Input)
		if err != nil {
			return nil, err
		}
	}
	closeErr := m.summaryClose
	if m.summaryCloseForCall != nil {
		closeErr = m.summaryCloseForCall(call)
	}
	if closeErr != nil {
		reader := &reviewErrorReadCloser{Reader: strings.NewReader(output), err: closeErr}
		return agent.NewExecutionResult(reader, func() int { return exitCode }, func() string { return stderr }), nil
	}
	return executionResult(output, exitCode, stderr), nil
}

func executionResult(output string, exitCode int, stderr string) *agent.ExecutionResult {
	return agent.NewExecutionResult(io.NopCloser(strings.NewReader(output)), func() int { return exitCode }, func() string { return stderr })
}

func codexReviewOutput(findings ...string) string {
	var lines []string
	for _, finding := range findings {
		payload, _ := json.Marshal(map[string]any{
			"item": map[string]string{
				"type": "agent_message",
				"text": finding,
			},
		})
		lines = append(lines, string(payload))
	}
	return strings.Join(lines, "\n")
}

func codexSummaryOutput(content string) string {
	return `{"type":"item.completed","item":{"type":"agent_message","text":` + strconv.Quote(content) + `}}`
}

func validRequest(t *testing.T, root string) Request {
	t.Helper()
	configuration, err := domain.NewReviewConfiguration(domain.ReviewConfigurationValues{
		Reviewers:         2,
		Concurrency:       1,
		Timeout:           time.Minute,
		Retries:           0,
		ReviewerAgents:    []string{"codex"},
		ReviewerModel:     "review-model",
		SummarizerAgent:   "codex",
		SummarizerModel:   "summary-model",
		SummarizerTimeout: time.Minute,
		FPFilterTimeout:   time.Minute,
		FPFilterEnabled:   false,
		FPThreshold:       75,
		PRFeedbackEnabled: false,
	})
	if err != nil {
		t.Fatalf("create configuration: %v", err)
	}
	return Request{
		Target: domain.ReviewTarget{
			RepositoryRoot: root,
			WorktreeRoot:   root,
			Revision: domain.RevisionEvidence{
				RequestedBaseRef: "main",
				ResolvedBaseRef:  "origin/main",
			},
		},
		Trigger:       domain.ReviewTriggerManual,
		Engine:        domain.ReviewEngine{Name: "acr", Version: "test"},
		Configuration: configuration,
		ConfigurationSource: domain.ConfigurationSourceIdentity{
			Kind:          "test",
			Locator:       "test-fixture",
			ConfigPresent: true,
			ConfigDigest:  "test-digest",
		},
	}
}

func serviceForTest(t *testing.T, reviewAgent agent.Agent, diff string, options ...Option) *Service {
	t.Helper()
	fixedTime := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	baseOptions := []Option{
		WithClock(func() time.Time { return fixedTime }),
		WithRunIDGenerator(func(time.Time) (string, error) { return "run-test", nil }),
		WithAgentFactory(func(string, string) (agent.Agent, error) { return reviewAgent, nil }),
		WithRevisionProvider(func(_ context.Context, target domain.ReviewTarget) (domain.RevisionEvidence, error) {
			revision := target.Revision
			revision.HeadObjectID = "head-object"
			revision.BaseObjectID = "base-object"
			return revision, nil
		}),
		WithDiffProvider(func(context.Context, domain.ReviewTarget) (string, error) { return diff, nil }),
		WithPriorFeedbackProvider(func(context.Context, domain.ReviewTarget, string, string) (string, error) { return "", nil }),
	}
	service, err := NewService(append(baseOptions, options...)...)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service
}

func TestServiceRunsMockAgentsHeadlessly(t *testing.T) {
	root := t.TempDir()
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "diff --git a/file b/file")
	request := validRequest(t, root)
	var events []Event
	request.Events = EventSinkFunc(func(event Event) {
		events = append(events, event)
	})

	beforeCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	stdoutPath := filepath.Join(t.TempDir(), "stdout")
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	os.Stdout = stdout
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	})

	run, runErr := service.Run(context.Background(), request)
	os.Stdout = originalStdout
	os.Stderr = originalStderr
	if err := stdout.Close(); err != nil {
		t.Fatalf("close stdout capture: %v", err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatalf("close stderr capture: %v", err)
	}
	if runErr != nil {
		t.Fatalf("run review: %v", runErr)
	}

	assertEmptyFile(t, stdoutPath)
	assertEmptyFile(t, stderrPath)
	afterCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd after review: %v", err)
	}
	if afterCWD != beforeCWD {
		t.Fatalf("process cwd changed from %q to %q", beforeCWD, afterCWD)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read target directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("headless service created files: %v", entries)
	}

	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings {
		t.Fatalf("unexpected outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if run.ID != "run-test" || run.ConfigurationFingerprint == "" {
		t.Fatalf("run identity not populated: %#v", run)
	}
	if run.ConfigurationSource != request.ConfigurationSource || run.ConfigurationSource.ConfigDigest == run.ConfigurationFingerprint {
		t.Fatalf("configuration source evidence was lost or conflated: source=%#v fingerprint=%q", run.ConfigurationSource, run.ConfigurationFingerprint)
	}
	if run.Target.PullRequest != nil {
		t.Fatalf("local review unexpectedly acquired PR identity: %#v", run.Target.PullRequest)
	}
	if run.Target.Revision.HeadObjectID != "head-object" || run.Target.Revision.BaseObjectID != "base-object" {
		t.Fatalf("revision evidence not populated: %#v", run.Target.Revision)
	}
	if len(run.ReviewerResults) != 2 || run.Stats.SuccessfulReviewers != 2 {
		t.Fatalf("reviewer evidence incomplete: results=%d stats=%#v", len(run.ReviewerResults), run.Stats)
	}
	if len(run.RawFindings) != 2 || len(run.AggregatedFindings) != 1 {
		t.Fatalf("finding evidence incomplete: raw=%d aggregated=%d", len(run.RawFindings), len(run.AggregatedFindings))
	}
	if !slicesEqual(run.AggregatedFindings[0].Reviewers, []int{1, 2}) {
		t.Fatalf("aggregate reviewer evidence = %v", run.AggregatedFindings[0].Reviewers)
	}
	if len(run.PreFilterSummary.Findings) != 1 || len(run.Findings) != 1 {
		t.Fatalf("summary evidence incomplete: pre=%#v final=%#v", run.PreFilterSummary, run.Findings)
	}
	if run.Findings[0].ID != "finding-001" || run.Findings[0].Disposition.Kind != domain.DispositionSurvived {
		t.Fatalf("run-local finding identity or disposition missing: %#v", run.Findings[0])
	}
	if run.Dispositions[0].Kind != domain.DispositionSurvived {
		t.Fatalf("raw finding disposition missing: %#v", run.Dispositions)
	}
	if len(events) == 0 || events[0].Kind != EventRunStarted || events[len(events)-1].Kind != EventRunCompleted {
		t.Fatalf("unexpected event boundaries: %#v", events)
	}
	foundReviewerOutput := false
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("event %d has sequence %d", i, event.Sequence)
		}
		if event.Kind == EventReviewerOutput {
			foundReviewerOutput = true
		}
	}
	if !foundReviewerOutput {
		t.Fatal("reviewer output event was not emitted")
	}
}

func TestServicePinsEveryReviewerToCapturedRevisionAndDiff(t *testing.T) {
	configs := make(chan agent.ReviewConfig, 2)
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(_ context.Context, config *agent.ReviewConfig) (string, int, string, error) {
			configs <- *config
			return codexReviewOutput("src/service.go:10: missing validation"), 0, "", nil
		},
	}
	service := serviceForTest(t, reviewAgent, "captured diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.ReviewStatusCompleted {
		t.Fatalf("run status = %q", run.Status)
	}
	for range 2 {
		config := <-configs
		if config.BaseRef != "base-object" || !config.DiffPrecomputed || config.Diff != "captured diff" {
			t.Fatalf("reviewer received mutable review input: %#v", config)
		}
	}
}

func TestServiceIsolatesSummaryAndFalsePositiveExecutionFromReviewTarget(t *testing.T) {
	root := t.TempDir()
	summaryConfigs := make(chan agent.SummaryConfig, 2)
	reviewAgent := &mockReviewAgent{
		name:           "codex",
		summaryConfigs: summaryConfigs,
		summary: func(_ context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			return codexSummaryOutput(`{"evaluations":[{"id":0,"fp_score":0,"reasoning":"actionable"}]}`), 0, "", nil
		},
	}
	request := validRequest(t, root)
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatal(err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.ReviewStatusCompleted {
		t.Fatalf("run status = %q", run.Status)
	}
	if len(summaryConfigs) != 2 {
		t.Fatalf("summary calls = %d, want 2", len(summaryConfigs))
	}
	var postProcessDir string
	for range 2 {
		config := <-summaryConfigs
		if config.WorkDir == root {
			t.Fatalf("summary workdir uses reviewed target %q", root)
		}
		if postProcessDir == "" {
			postProcessDir = config.WorkDir
		} else if config.WorkDir != postProcessDir {
			t.Fatalf("post-processing workdirs differ: %q and %q", postProcessDir, config.WorkDir)
		}
	}
	if _, err := os.Stat(postProcessDir); !os.IsNotExist(err) {
		t.Fatalf("post-processing workspace remains after review: %v", err)
	}
}

func TestServiceEmitsReviewerCleanupWarningsHeadlessly(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex", reviewClose: errors.New("temporary file cleanup failed")}
	service := serviceForTest(t, reviewAgent, "diff --git a/file b/file")
	request := validRequest(t, t.TempDir())
	var events []Event
	request.Events = EventSinkFunc(func(event Event) {
		events = append(events, event)
	})
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() { os.Stderr = originalStderr })

	run, err := service.Run(context.Background(), request)
	os.Stderr = originalStderr
	if err := stderr.Close(); err != nil {
		t.Fatalf("close stderr capture: %v", err)
	}

	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings {
		t.Fatalf("cleanup warning changed run outcome: %#v", run)
	}
	if len(run.ReviewerResults) != 2 {
		t.Fatalf("reviewer results = %d, want 2", len(run.ReviewerResults))
	}
	for _, result := range run.ReviewerResults {
		if len(result.Warnings) != 1 || result.Warnings[0].Kind != domain.ReviewerWarningCleanup {
			t.Fatalf("reviewer cleanup warning missing: %#v", result)
		}
	}
	warningEvents := 0
	completedWithWarning := 0
	for _, event := range events {
		if event.Kind == EventWarning && event.Phase == domain.ReviewPhaseReviewers && strings.Contains(event.Message, "reviewer cleanup failed") {
			warningEvents++
		}
		if event.Kind == EventReviewerCompleted && len(event.ReviewerResult.Warnings) == 1 {
			completedWithWarning++
		}
	}
	if warningEvents != 2 || completedWithWarning != 2 {
		t.Fatalf("cleanup warning events incomplete: warnings=%d completed=%d events=%#v", warningEvents, completedWithWarning, events)
	}
	captured, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	if len(captured) != 0 {
		t.Fatalf("headless service wrote stderr for cleanup warnings: %q", captured)
	}
}

func TestServiceRetainsSummarizerCleanupWarningsHeadlessly(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex", summaryClose: errors.New("temporary summary cleanup failed")}
	service := serviceForTest(t, reviewAgent, "diff")
	request := validRequest(t, t.TempDir())
	var events []Event
	request.Events = EventSinkFunc(func(event Event) { events = append(events, event) })

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings {
		t.Fatalf("cleanup warning changed run outcome: %#v", run)
	}
	if len(run.Summarizer.Warnings) != 1 || !strings.Contains(run.Summarizer.Warnings[0], "summary cleanup failed") {
		t.Fatalf("summarizer cleanup warning missing: %#v", run.Summarizer)
	}
	warningEvents := 0
	for _, event := range events {
		if event.Kind == EventWarning && event.Phase == domain.ReviewPhaseSummarization && event.Message == run.Summarizer.Warnings[0] {
			warningEvents++
		}
	}
	if warningEvents != 1 {
		t.Fatalf("summarizer cleanup warning events = %d, events=%#v", warningEvents, events)
	}
}

func TestServiceDefaultDiffIncludesStagedAndUnstagedChanges(t *testing.T) {
	root := t.TempDir()
	runGitForServiceTest(t, root, "init")
	runGitForServiceTest(t, root, "config", "user.email", "test@example.com")
	runGitForServiceTest(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("initial staged"), 0o600); err != nil {
		t.Fatalf("write staged fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "unstaged.txt"), []byte("initial unstaged"), 0o600); err != nil {
		t.Fatalf("write unstaged fixture: %v", err)
	}
	runGitForServiceTest(t, root, "add", ".")
	runGitForServiceTest(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("changed staged"), 0o600); err != nil {
		t.Fatalf("modify staged fixture: %v", err)
	}
	runGitForServiceTest(t, root, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(root, "unstaged.txt"), []byte("changed unstaged"), 0o600); err != nil {
		t.Fatalf("modify unstaged fixture: %v", err)
	}

	reviewAgent := &mockReviewAgent{name: "codex"}
	fixedTime := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	service, err := NewService(
		WithClock(func() time.Time { return fixedTime }),
		WithRunIDGenerator(func(time.Time) (string, error) { return "run-local-diff", nil }),
		WithAgentFactory(func(string, string) (agent.Agent, error) { return reviewAgent, nil }),
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	request := validRequest(t, root)
	request.Target.Revision.RequestedBaseRef = "HEAD"
	request.Target.Revision.ResolvedBaseRef = "HEAD"

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run local review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings {
		t.Fatalf("staged and unstaged changes were not reviewed: status=%q conclusion=%q failure=%#v", run.Status, run.Conclusion, run.Failure)
	}
}

func TestServiceFailsWhenHeadMovesDuringDiffCapture(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex"}
	var revisionCalls atomic.Int32
	service := serviceForTest(t, reviewAgent, "diff", WithRevisionProvider(func(_ context.Context, target domain.ReviewTarget) (domain.RevisionEvidence, error) {
		revision := target.Revision
		revision.BaseObjectID = "base-object"
		if revisionCalls.Add(1) == 1 {
			revision.HeadObjectID = "head-a"
		} else {
			revision.HeadObjectID = "head-b"
		}
		return revision, nil
	}))

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseDiff {
		t.Fatalf("moving head was accepted: %#v", run)
	}
	if !strings.Contains(run.Failure.Message, "review head changed") {
		t.Fatalf("failure = %#v", run.Failure)
	}
	if len(run.ReviewerResults) != 0 || reviewAgent.summaryCalls.Load() != 0 {
		t.Fatalf("review continued after head movement: %#v", run)
	}
}

func TestServiceReturnsNoChangesWithoutReviewExecution(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "")
	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionNoChanges {
		t.Fatalf("unexpected no-change outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if len(run.ReviewerResults) != 0 || reviewAgent.summaryCalls.Load() != 0 {
		t.Fatalf("review work ran for empty diff: results=%d summaries=%d", len(run.ReviewerResults), reviewAgent.summaryCalls.Load())
	}
}

func TestServiceInterruptsNoChangesRunCanceledAtDiffBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "")
	request := validRequest(t, t.TempDir())
	request.Events = EventSinkFunc(func(event Event) {
		if event.Kind == EventPhaseCompleted && event.Phase == domain.ReviewPhaseDiff {
			cancel()
		}
	})

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Conclusion != domain.ReviewConclusionNone {
		t.Fatalf("canceled no-change run completed: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseDiff {
		t.Fatalf("diff-boundary interruption evidence missing: %#v", run.Failure)
	}
	if len(run.ReviewerResults) != 0 || reviewAgent.summaryCalls.Load() != 0 {
		t.Fatalf("review work ran after diff cancellation: results=%d summaries=%d", len(run.ReviewerResults), reviewAgent.summaryCalls.Load())
	}
}

func TestServiceReturnsCleanRunWithoutFindings(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(context.Context, *agent.ReviewConfig) (string, int, string, error) {
			return codexReviewOutput("No issues found."), 0, "", nil
		},
	}
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionClean {
		t.Fatalf("unexpected clean outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if len(run.ReviewerResults) != 2 || run.Stats.SuccessfulReviewers != 2 || len(run.RawFindings) != 0 {
		t.Fatalf("clean run evidence incomplete: results=%d stats=%#v raw=%d", len(run.ReviewerResults), run.Stats, len(run.RawFindings))
	}
}

func TestServiceCompletesWithPartialReviewerFailures(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(ctx context.Context, config *agent.ReviewConfig) (string, int, string, error) {
			switch config.ReviewerID {
			case "2":
				return codexReviewOutput("src/service.go:10: missing validation"), 1, "failed", nil
			case "3":
				<-ctx.Done()
				return "", 0, "", ctx.Err()
			default:
				return codexReviewOutput("src/service.go:10: missing validation"), 0, "", nil
			}
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.Reviewers = 3
	values.Concurrency = 3
	values.Timeout = 20 * time.Millisecond
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create partial-failure configuration: %v", err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings {
		t.Fatalf("partial failure did not complete: status=%q conclusion=%q failure=%#v", run.Status, run.Conclusion, run.Failure)
	}
	if run.Stats.SuccessfulReviewers != 1 || !slicesEqual(run.Stats.FailedReviewers, []int{2}) || !slicesEqual(run.Stats.TimedOutReviewers, []int{3}) {
		t.Fatalf("partial failures not classified: %#v", run.Stats)
	}
	results := make(map[int]domain.ReviewerResult)
	for _, result := range run.ReviewerResults {
		results[result.ReviewerID] = result
	}
	if results[2].Failure == nil || results[2].Failure.Kind != domain.ReviewerFailureExit {
		t.Fatalf("ordinary failure missing: %#v", results[2])
	}
	if results[3].Failure == nil || results[3].Failure.Kind != domain.ReviewerFailureTimeout {
		t.Fatalf("timeout failure missing: %#v", results[3])
	}
	if len(run.RawFindings) != 2 || !slicesEqual(run.AggregatedFindings[0].Reviewers, []int{1, 2}) {
		t.Fatalf("failed reviewer findings were not retained: raw=%#v aggregated=%#v", run.RawFindings, run.AggregatedFindings)
	}
}

func TestServiceReturnsPopulatedFailedRunAfterAcceptance(t *testing.T) {
	root := t.TempDir()
	request := validRequest(t, root)
	var events []Event
	request.Events = EventSinkFunc(func(event Event) { events = append(events, event) })
	fixedTime := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	service, err := NewService(
		WithClock(func() time.Time { return fixedTime }),
		WithRunIDGenerator(func(time.Time) (string, error) { return "run-failed", nil }),
		WithAgentFactory(func(string, string) (agent.Agent, error) { return nil, errors.New("backend unavailable") }),
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	run, runErr := service.Run(context.Background(), request)
	if runErr != nil {
		t.Fatalf("accepted execution returned Go error: %v", runErr)
	}
	if run == nil || run.ID != "run-failed" || run.Target.WorktreeRoot != root {
		t.Fatalf("failed run lost request evidence: %#v", run)
	}
	if run.Status != domain.ReviewStatusFailed || run.Conclusion != domain.ReviewConclusionNone {
		t.Fatalf("unexpected failed outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseInitialization || !strings.Contains(run.Failure.Message, "backend unavailable") {
		t.Fatalf("failure evidence missing: %#v", run.Failure)
	}
	if run.StartedAt.IsZero() || run.CompletedAt.IsZero() || run.ConfigurationFingerprint == "" {
		t.Fatalf("failed run is not persistence-ready: %#v", run)
	}
	assertOrderedEventBoundaries(t, events, domain.ReviewStatusFailed)
}

func TestServiceReturnsPopulatedInterruptedRunAfterAcceptance(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "diff")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := validRequest(t, t.TempDir())
	var events []Event
	request.Events = EventSinkFunc(func(event Event) { events = append(events, event) })

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Conclusion != domain.ReviewConclusionNone {
		t.Fatalf("unexpected interrupted outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if run.Failure == nil || !errors.Is(ctx.Err(), context.Canceled) || !strings.Contains(run.Failure.Message, "canceled") {
		t.Fatalf("interruption evidence missing: %#v", run.Failure)
	}
	if run.ID == "" || run.ConfigurationFingerprint == "" || run.CompletedAt.IsZero() {
		t.Fatalf("interrupted run is not persistence-ready: %#v", run)
	}
	assertOrderedEventBoundaries(t, events, domain.ReviewStatusInterrupted)
}

func TestServiceInterruptDuringReviewersRetainsCompletedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(reviewCtx context.Context, config *agent.ReviewConfig) (string, int, string, error) {
			if config.ReviewerID == "2" {
				<-reviewCtx.Done()
				return "", 0, "", reviewCtx.Err()
			}
			return codexReviewOutput("src/service.go:10: missing validation"), 0, "", nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.Concurrency = 2
	configuration, configurationErr := domain.NewReviewConfiguration(values)
	if configurationErr != nil {
		t.Fatalf("create interrupt configuration: %v", configurationErr)
	}
	request.Configuration = configuration
	var events []Event
	request.Events = EventSinkFunc(func(event Event) {
		events = append(events, event)
		if event.Kind == EventReviewerCompleted && event.ReviewerID == 1 {
			cancel()
		}
	})
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseReviewers {
		t.Fatalf("unexpected interrupted run: %#v", run)
	}
	if len(run.ReviewerResults) != 2 || len(run.RawFindings) != 1 || len(run.AggregatedFindings) != 1 {
		t.Fatalf("completed reviewer state was lost: results=%#v raw=%#v aggregated=%#v", run.ReviewerResults, run.RawFindings, run.AggregatedFindings)
	}
	results := make(map[int]domain.ReviewerResult)
	for _, result := range run.ReviewerResults {
		results[result.ReviewerID] = result
	}
	if results[2].Failure == nil || results[2].Failure.Kind != domain.ReviewerFailureInterrupted {
		t.Fatalf("canceled reviewer was not retained as interrupted: %#v", results[2])
	}
	completedReviewers := 0
	reviewerPhaseCompleted := false
	for _, event := range events {
		if event.Kind == EventPhaseCompleted && event.Phase == domain.ReviewPhaseReviewers {
			reviewerPhaseCompleted = true
		}
		if event.Kind == EventReviewerCompleted {
			if reviewerPhaseCompleted {
				t.Fatalf("reviewer completion emitted after reviewer phase completion: %#v", event)
			}
			completedReviewers++
		}
		if event.Kind == EventRunCompleted && completedReviewers != 2 {
			t.Fatalf("run completed after only %d reviewer completions", completedReviewers)
		}
	}
	if completedReviewers != 2 || !reviewerPhaseCompleted {
		t.Fatalf("reviewer event lifecycle incomplete: completions=%d phaseCompleted=%v", completedReviewers, reviewerPhaseCompleted)
	}
}

func TestServiceReturnsPopulatedReviewerFailure(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(context.Context, *agent.ReviewConfig) (string, int, string, error) {
			return codexReviewOutput("partial finding"), 1, "review failed", nil
		},
	}
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("accepted reviewer failure returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseReviewers {
		t.Fatalf("unexpected failed run: %#v", run)
	}
	if len(run.ReviewerResults) != 2 || len(run.RawFindings) != 2 || len(run.AggregatedFindings) != 1 {
		t.Fatalf("failed reviewer evidence was lost: results=%d raw=%d aggregated=%d", len(run.ReviewerResults), len(run.RawFindings), len(run.AggregatedFindings))
	}
	for _, result := range run.ReviewerResults {
		if result.Failure == nil || result.Failure.Kind != domain.ReviewerFailureExit || result.Attempts != 1 {
			t.Fatalf("typed reviewer failure missing: %#v", result)
		}
	}
}

func TestServiceCancelsAndJoinsPriorFeedbackOnEarlyFailure(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(context.Context, *agent.ReviewConfig) (string, int, string, error) {
			return "", 1, "review failed", nil
		},
	}
	var feedbackStopped atomic.Bool
	var completionClock atomic.Int64
	feedbackProvider := func(ctx context.Context, _ domain.ReviewTarget, _, _ string) (string, error) {
		<-ctx.Done()
		feedbackStopped.Store(true)
		completionClock.Store(2)
		return "", ctx.Err()
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	values.PRFeedbackEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create feedback configuration: %v", err)
	}
	request.Configuration = configuration
	request.Target.PullRequest = &domain.PullRequestKey{Host: "github.com", Owner: "owner", Repository: "repo", Number: 42}
	var events []Event
	var completionBeforeFeedbackStopped atomic.Bool
	request.Events = EventSinkFunc(func(event Event) {
		events = append(events, event)
		if event.Kind == EventRunCompleted && !feedbackStopped.Load() {
			completionBeforeFeedbackStopped.Store(true)
		}
	})
	service := serviceForTest(
		t,
		reviewAgent,
		"diff",
		WithPriorFeedbackProvider(feedbackProvider),
		WithClock(func() time.Time { return time.Unix(completionClock.Load(), 0) }),
	)

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed {
		t.Fatalf("unexpected run status %q", run.Status)
	}
	if !feedbackStopped.Load() {
		t.Fatal("prior-feedback worker remained active after service return")
	}
	if completionBeforeFeedbackStopped.Load() {
		t.Fatal("run completion was emitted before prior-feedback worker stopped")
	}
	if run.CompletedAt.Before(time.Unix(2, 0)) {
		t.Fatalf("run completed at %s before feedback joined", run.CompletedAt)
	}
	feedbackStarted := -1
	feedbackCompleted := -1
	runCompleted := -1
	for i, event := range events {
		if event.Kind == EventPhaseStarted && event.Phase == domain.ReviewPhaseFeedback {
			feedbackStarted = i
		}
		if event.Kind == EventPhaseCompleted && event.Phase == domain.ReviewPhaseFeedback {
			feedbackCompleted = i
		}
		if event.Kind == EventRunCompleted {
			runCompleted = i
		}
	}
	if feedbackStarted < 0 || feedbackCompleted <= feedbackStarted || runCompleted <= feedbackCompleted {
		t.Fatalf("feedback phase lifecycle is unbalanced: %#v", events)
	}
}

func TestServiceRetainsPriorFeedbackWhenCleanupWarns(t *testing.T) {
	var feedbackUsed atomic.Bool
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(_ context.Context, call int64, prompt string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			if strings.Contains(prompt, "trusted prior feedback") {
				feedbackUsed.Store(true)
			}
			return codexSummaryOutput(`{"evaluations":[{"id":0,"fp_score":0,"reasoning":"actionable"}]}`), 0, "", nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	values.PRFeedbackEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatal(err)
	}
	request.Configuration = configuration
	request.Target.PullRequest = &domain.PullRequestKey{Host: "github.com", Owner: "owner", Repository: "repo", Number: 42}
	var events []Event
	request.Events = EventSinkFunc(func(event Event) { events = append(events, event) })
	service := serviceForTest(
		t,
		reviewAgent,
		"diff",
		WithPriorFeedbackProvider(func(context.Context, domain.ReviewTarget, string, string) (string, error) {
			return "trusted prior feedback", errors.New("feedback cleanup failed")
		}),
	)

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.ReviewStatusCompleted || !feedbackUsed.Load() {
		t.Fatalf("prior feedback was discarded: status=%q used=%v", run.Status, feedbackUsed.Load())
	}
	found := false
	for _, event := range events {
		if event.Kind == EventWarning && event.Phase == domain.ReviewPhaseFeedback && strings.Contains(event.Message, "feedback cleanup failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("feedback cleanup warning event was not emitted")
	}
}

func TestServiceSummarizerFailureRetainsReviewerEvidence(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(context.Context, int64, string, []byte) (string, int, string, error) {
			return codexSummaryOutput(`{"findings":[],"info":[]}`), 1, "summary failed", nil
		},
	}
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("accepted summarizer failure returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseSummarization {
		t.Fatalf("unexpected summarizer failure: %#v", run)
	}
	if len(run.ReviewerResults) != 2 || len(run.RawFindings) != 2 || len(run.AggregatedFindings) != 1 {
		t.Fatalf("summarizer failure lost reviewer evidence: %#v", run)
	}
	if run.Summarizer.ExitCode != 1 || run.Summarizer.Stderr != "summary failed" || run.Summarizer.DiagnosticOutput == "" {
		t.Fatalf("summarizer diagnostic evidence missing: %#v", run.Summarizer)
	}
}

func TestServiceBoundsOneLineSummarizerFailureEvidence(t *testing.T) {
	rawOutput := strings.Repeat("x", summaryDiagnosticMaxBytes*4)
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(context.Context, int64, string, []byte) (string, int, string, error) {
			return rawOutput, 1, "summary failed", nil
		},
	}
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("accepted summarizer failure returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseSummarization {
		t.Fatalf("unexpected summarizer failure: %#v", run)
	}
	if len(run.Summarizer.DiagnosticOutput) > summaryDiagnosticMaxBytes {
		t.Fatalf("diagnostic output has %d bytes, want at most %d", len(run.Summarizer.DiagnosticOutput), summaryDiagnosticMaxBytes)
	}
	if !strings.HasSuffix(run.Summarizer.DiagnosticOutput, summaryDiagnosticTruncationMarker) {
		t.Fatalf("bounded diagnostic output is not marked as truncated: %q", run.Summarizer.DiagnosticOutput)
	}
	if run.Summarizer.DiagnosticOutput == rawOutput {
		t.Fatal("raw summarizer transcript was retained")
	}
}

func TestServiceBoundsSummarizerStderrAndFailureMessage(t *testing.T) {
	stderr := strings.Repeat("failure detail ", summaryDiagnosticMaxBytes)
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(context.Context, int64, string, []byte) (string, int, string, error) {
			return codexSummaryOutput(`{"findings":[],"info":[]}`), 1, stderr, nil
		},
	}
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), validRequest(t, t.TempDir()))
	if err != nil {
		t.Fatalf("accepted summarizer failure returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusFailed || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseSummarization {
		t.Fatalf("unexpected summarizer failure: %#v", run)
	}
	if len(run.Summarizer.Stderr) > summaryDiagnosticMaxBytes {
		t.Fatalf("summarizer stderr has %d bytes, want at most %d", len(run.Summarizer.Stderr), summaryDiagnosticMaxBytes)
	}
	if !strings.HasSuffix(run.Summarizer.Stderr, summaryDiagnosticTruncationMarker) {
		t.Fatalf("bounded summarizer stderr is not marked as truncated: %q", run.Summarizer.Stderr)
	}
	if run.Failure.Message != strings.TrimSpace(run.Summarizer.Stderr) {
		t.Fatalf("failure message was not derived from bounded stderr: message=%q stderr=%q", run.Failure.Message, run.Summarizer.Stderr)
	}
	if strings.Contains(run.Failure.Message, stderr) {
		t.Fatal("run failure retained full summarizer stderr")
	}
}

func TestServiceInterruptDuringSummarizationRetainsReviewerEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reviewAgent := &mockReviewAgent{name: "codex"}
	request := validRequest(t, t.TempDir())
	request.Events = EventSinkFunc(func(event Event) {
		if event.Kind == EventPhaseStarted && event.Phase == domain.ReviewPhaseSummarization {
			cancel()
		}
	})
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseSummarization {
		t.Fatalf("unexpected interrupted run: %#v", run)
	}
	if len(run.RawFindings) != 2 || len(run.AggregatedFindings) != 1 {
		t.Fatalf("summarization interruption lost reviewer evidence: %#v", run)
	}
}

func TestServiceInterruptsRunCanceledAtSummarizationBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reviewAgent := &mockReviewAgent{name: "codex"}
	request := validRequest(t, t.TempDir())
	request.Events = EventSinkFunc(func(event Event) {
		if event.Kind == EventPhaseCompleted && event.Phase == domain.ReviewPhaseSummarization {
			cancel()
		}
	})
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Conclusion != domain.ReviewConclusionNone {
		t.Fatalf("canceled summarized run completed: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseSummarization {
		t.Fatalf("summarization-boundary interruption evidence missing: %#v", run.Failure)
	}
	if len(run.PreFilterSummary.Findings) != 1 || len(run.FindingRecords) != 1 {
		t.Fatalf("summarization-boundary interruption lost evidence: %#v", run)
	}
}

func TestServicePreservesPreFilterEvidenceAndExcludeDisposition(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		review: func(context.Context, *agent.ReviewConfig) (string, int, string, error) {
			return codexReviewOutput("src/main.go:1: bug", "generated/file.go:2: generated bug"), 0, "", nil
		},
		summary: func(_ context.Context, _ int64, _ string, _ []byte) (string, int, string, error) {
			return codexSummaryOutput(`{"findings":[{"title":"Real bug","summary":"Real.","messages":["src/main.go:1: bug"],"reviewer_count":2,"sources":[0]},{"title":"Generated bug","summary":"Generated.","messages":["generated/file.go:2: generated bug"],"reviewer_count":2,"sources":[1]}],"info":[]}`), 0, "", nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.ExcludePatterns = []string{"generated/"}
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create filtered configuration: %v", err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if len(run.PreFilterSummary.Findings) != 2 || len(run.Findings) != 1 || len(run.ExcludeFilter.Removed) != 1 {
		t.Fatalf("filter evidence incomplete: pre=%d final=%d removed=%d", len(run.PreFilterSummary.Findings), len(run.Findings), len(run.ExcludeFilter.Removed))
	}
	if run.Findings[0].ID != "finding-001" || run.ExcludeFilter.Removed[0].ID != "finding-002" {
		t.Fatalf("finding identities did not follow observed summary order: final=%#v removed=%#v", run.Findings, run.ExcludeFilter.Removed)
	}
	if run.ExcludeFilter.Removed[0].Disposition.Kind != domain.DispositionFilteredExclude {
		t.Fatalf("exclude disposition missing: %#v", run.ExcludeFilter.Removed[0])
	}
}

func TestServiceRecordsFalsePositiveDisposition(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(_ context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			return codexSummaryOutput(`{"evaluations":[{"id":0,"fp_score":95,"reasoning":"not actionable"}]}`), 0, "", nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create FP configuration: %v", err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionClean {
		t.Fatalf("unexpected filtered outcome: status=%q conclusion=%q", run.Status, run.Conclusion)
	}
	if !run.FalsePositiveFilter.Applied || len(run.FalsePositiveFilter.Removed) != 1 {
		t.Fatalf("false-positive outcome missing: %#v", run.FalsePositiveFilter)
	}
	removed := run.FalsePositiveFilter.Removed[0]
	if removed.ID != "finding-001" || removed.Disposition.FPScore != 95 || removed.Disposition.Reasoning != "not actionable" {
		t.Fatalf("false-positive disposition incomplete: %#v", removed)
	}
}

func TestServiceRetainsFalsePositiveCleanupWarningsHeadlessly(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(_ context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			return codexSummaryOutput(`{"evaluations":[{"id":0,"fp_score":0,"reasoning":"actionable"}]}`), 0, "", nil
		},
		summaryCloseForCall: func(call int64) error {
			if call == 2 {
				return errors.New("FP temp cleanup failed")
			}
			return nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatal(err)
	}
	request.Configuration = configuration
	var events []Event
	request.Events = EventSinkFunc(func(event Event) { events = append(events, event) })
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.FalsePositiveFilter.Warnings) != 1 || !strings.Contains(run.FalsePositiveFilter.Warnings[0], "FP temp cleanup failed") {
		t.Fatalf("FP cleanup warnings = %#v", run.FalsePositiveFilter.Warnings)
	}
	found := false
	for _, event := range events {
		if event.Kind == EventWarning && event.Phase == domain.ReviewPhaseFalsePositiveFilter && strings.Contains(event.Message, "FP temp cleanup failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("FP cleanup warning event was not emitted")
	}
}

func TestServiceTracksDuplicateFindingGroupsByFilterIdentity(t *testing.T) {
	duplicate := `{"title":"Duplicate","summary":"Same.","messages":["src/service.go:10: issue"],"reviewer_count":2,"sources":[0]}`
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(_ context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[` + duplicate + `,` + duplicate + `],"info":[]}`), 0, "", nil
			}
			return codexSummaryOutput(`{"evaluations":[{"id":0,"fp_score":95,"reasoning":"first removed"},{"id":1,"fp_score":0,"reasoning":"second kept"}]}`), 0, "", nil
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatal(err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Findings) != 1 || run.Findings[0].ID != "finding-002" {
		t.Fatalf("surviving duplicate identity = %#v", run.Findings)
	}
	if len(run.FalsePositiveFilter.Removed) != 1 || run.FalsePositiveFilter.Removed[0].ID != "finding-001" {
		t.Fatalf("removed duplicate identity = %#v", run.FalsePositiveFilter.Removed)
	}
	if run.Stats.FPFilteredCount != 1 {
		t.Fatalf("FP filtered count = %d", run.Stats.FPFilteredCount)
	}
}

func TestServiceFalsePositiveFailureIsExplicitAndFailOpen(t *testing.T) {
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(_ context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			return "", 0, "", errors.New("FP backend unavailable")
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create FP configuration: %v", err)
	}
	request.Configuration = configuration
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("run review: %v", err)
	}
	if run.Status != domain.ReviewStatusCompleted || run.Conclusion != domain.ReviewConclusionFindings || len(run.Findings) != 1 {
		t.Fatalf("FP failure did not fail open: %#v", run)
	}
	if !run.FalsePositiveFilter.Skipped || !strings.Contains(run.FalsePositiveFilter.SkipReason, "FP backend unavailable") {
		t.Fatalf("FP skip evidence missing: %#v", run.FalsePositiveFilter)
	}
}

func TestServiceInterruptDuringFalsePositiveFilterRetainsIntermediateState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reviewAgent := &mockReviewAgent{
		name: "codex",
		summary: func(summaryCtx context.Context, call int64, _ string, _ []byte) (string, int, string, error) {
			if call == 1 {
				return codexSummaryOutput(`{"findings":[{"title":"Maybe bug","summary":"Maybe.","messages":["src/service.go:10: missing validation"],"reviewer_count":2,"sources":[0]}],"info":[]}`), 0, "", nil
			}
			return "", 0, "", summaryCtx.Err()
		},
	}
	request := validRequest(t, t.TempDir())
	values := request.Configuration.Values()
	values.FPFilterEnabled = true
	configuration, err := domain.NewReviewConfiguration(values)
	if err != nil {
		t.Fatalf("create FP configuration: %v", err)
	}
	request.Configuration = configuration
	request.Events = EventSinkFunc(func(event Event) {
		if event.Kind == EventPhaseStarted && event.Phase == domain.ReviewPhaseFalsePositiveFilter {
			cancel()
		}
	})
	service := serviceForTest(t, reviewAgent, "diff")

	run, err := service.Run(ctx, request)
	if err != nil {
		t.Fatalf("accepted interruption returned Go error: %v", err)
	}
	if run.Status != domain.ReviewStatusInterrupted || run.Failure == nil || run.Failure.Phase != domain.ReviewPhaseFalsePositiveFilter {
		t.Fatalf("unexpected interrupted run: %#v", run)
	}
	if len(run.PreFilterSummary.Findings) != 1 || len(run.FindingRecords) != 1 || run.FindingRecords[0].ID != "finding-001" {
		t.Fatalf("FP interruption lost intermediate finding identity: %#v", run)
	}
}

func TestServiceRejectsInvalidRequestBeforeAcceptance(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "diff")
	request := validRequest(t, t.TempDir())
	request.Target.WorktreeRoot = "relative"

	run, err := service.Run(context.Background(), request)
	if err == nil {
		t.Fatal("expected invalid request error")
	}
	if run != nil {
		t.Fatalf("invalid request was accepted: %#v", run)
	}
}

func TestServiceRejectsMissingConfigurationSourceBeforeAcceptance(t *testing.T) {
	reviewAgent := &mockReviewAgent{name: "codex"}
	service := serviceForTest(t, reviewAgent, "diff")
	request := validRequest(t, t.TempDir())
	request.ConfigurationSource = domain.ConfigurationSourceIdentity{}

	run, err := service.Run(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "configuration source") {
		t.Fatalf("expected configuration source error, got %v", err)
	}
	if run != nil {
		t.Fatalf("request without source evidence was accepted: %#v", run)
	}
}

func assertEmptyFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture %q: %v", path, err)
	}
	if len(data) != 0 {
		t.Fatalf("unexpected process output in %q: %q", path, data)
	}
}

func slicesEqual(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func runGitForServiceTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func assertOrderedEventBoundaries(t *testing.T, events []Event, status domain.ReviewStatus) {
	t.Helper()
	if len(events) < 2 || events[0].Kind != EventRunStarted || events[len(events)-1].Kind != EventRunCompleted {
		t.Fatalf("unexpected event boundaries: %#v", events)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("event %d has sequence %d", i, event.Sequence)
		}
	}
	if events[len(events)-1].Status != status {
		t.Fatalf("last event status = %q, want %q", events[len(events)-1].Status, status)
	}
}
