package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

type stringReadCloser struct {
	*strings.Reader
}

func (s *stringReadCloser) Close() error {
	return nil
}

type errorReadCloser struct {
	*strings.Reader
	err error
}

func (r *errorReadCloser) Close() error {
	return r.err
}

func TestBuildStats_CategorizesResults(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Duration: 10 * time.Second},
		{ReviewerID: 2, ExitCode: 1, Duration: 15 * time.Second},
		{ReviewerID: 3, TimedOut: true, ExitCode: -1, Duration: 30 * time.Second},
		{ReviewerID: 4, ExitCode: 0, Duration: 12 * time.Second, ParseErrors: 2},
	}

	stats := BuildStats(results, 4, 35*time.Second)

	if stats.SuccessfulReviewers != 2 {
		t.Errorf("expected 2 successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.FailedReviewers) != 1 || stats.FailedReviewers[0] != 2 {
		t.Errorf("expected FailedReviewers=[2], got %v", stats.FailedReviewers)
	}
	if len(stats.TimedOutReviewers) != 1 || stats.TimedOutReviewers[0] != 3 {
		t.Errorf("expected TimedOutReviewers=[3], got %v", stats.TimedOutReviewers)
	}
	if stats.ParseErrors != 2 {
		t.Errorf("expected 2 parse errors, got %d", stats.ParseErrors)
	}
}

func TestFinishRunReportsCancellationAfterResultsWereCollected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	want := []domain.ReviewerResult{{ReviewerID: 1, ExitCode: 0}}

	got, _, err := finishRun(ctx, want, time.Now())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("finishRun error = %v, want context cancellation", err)
	}
	if len(got) != 1 || got[0].ReviewerID != 1 {
		t.Fatalf("collected reviewer results were lost: %#v", got)
	}
}

func TestBuildStats_TracksReviewerDurations(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Duration: 10 * time.Second},
		{ReviewerID: 2, ExitCode: 0, Duration: 20 * time.Second},
	}

	stats := BuildStats(results, 2, 25*time.Second)

	if len(stats.ReviewerDurations) != 2 {
		t.Fatalf("expected 2 duration entries, got %d", len(stats.ReviewerDurations))
	}
	if stats.ReviewerDurations[1] != 10*time.Second {
		t.Errorf("reviewer 1 duration: expected 10s, got %v", stats.ReviewerDurations[1])
	}
	if stats.ReviewerDurations[2] != 20*time.Second {
		t.Errorf("reviewer 2 duration: expected 20s, got %v", stats.ReviewerDurations[2])
	}
	if stats.WallClockDuration != 25*time.Second {
		t.Errorf("wall clock: expected 25s, got %v", stats.WallClockDuration)
	}
}

func TestBuildStats_AggregatesParseErrors(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, ParseErrors: 3},
		{ReviewerID: 2, ExitCode: 0, ParseErrors: 5},
		{ReviewerID: 3, ExitCode: 0, ParseErrors: 0},
	}

	stats := BuildStats(results, 3, time.Second)

	if stats.ParseErrors != 8 {
		t.Errorf("expected total 8 parse errors, got %d", stats.ParseErrors)
	}
}

func TestBuildStats_EmptyResults(t *testing.T) {
	stats := BuildStats(nil, 0, 0)

	if stats.SuccessfulReviewers != 0 {
		t.Errorf("expected 0 successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.FailedReviewers) != 0 {
		t.Errorf("expected no failures, got %v", stats.FailedReviewers)
	}
	if len(stats.TimedOutReviewers) != 0 {
		t.Errorf("expected no timeouts, got %v", stats.TimedOutReviewers)
	}
}

func TestBuildStats_TimeoutTakesPrecedenceOverExitCode(t *testing.T) {

	results := []domain.ReviewerResult{
		{ReviewerID: 1, TimedOut: true, ExitCode: 0},
		{ReviewerID: 2, TimedOut: true, ExitCode: 1},
	}

	stats := BuildStats(results, 2, time.Second)

	if stats.SuccessfulReviewers != 0 {
		t.Errorf("timed out reviewers should not count as successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.TimedOutReviewers) != 2 {
		t.Errorf("expected 2 timed out, got %v", stats.TimedOutReviewers)
	}
}

func TestCollectFindings_FlattensFromAllReviewers(t *testing.T) {
	results := []domain.ReviewerResult{
		{
			ReviewerID: 1,
			Findings: []domain.Finding{
				{Text: "Issue A", ReviewerID: 1},
				{Text: "Issue B", ReviewerID: 1},
			},
		},
		{
			ReviewerID: 2,
			Findings: []domain.Finding{
				{Text: "Issue C", ReviewerID: 2},
			},
		},
		{
			ReviewerID: 3,
			Findings:   nil,
		},
	}

	findings := CollectFindings(results)

	if len(findings) != 3 {
		t.Fatalf("expected 3 total findings, got %d", len(findings))
	}

	texts := map[string]bool{}
	for _, f := range findings {
		texts[f.Text] = true
	}
	if !texts["Issue A"] || !texts["Issue B"] || !texts["Issue C"] {
		t.Errorf("missing expected findings, got %v", findings)
	}
}

func TestCollectFindings_EmptyResults(t *testing.T) {
	findings := CollectFindings(nil)
	if len(findings) != 0 {
		t.Errorf("expected empty findings for nil input, got %d", len(findings))
	}

	findings = CollectFindings([]domain.ReviewerResult{})
	if len(findings) != 0 {
		t.Errorf("expected empty findings for empty input, got %d", len(findings))
	}
}

func TestCollectFindings_PreservesReviewerIDs(t *testing.T) {
	results := []domain.ReviewerResult{
		{
			ReviewerID: 5,
			Findings: []domain.Finding{
				{Text: "Finding from 5", ReviewerID: 5},
			},
		},
		{
			ReviewerID: 10,
			Findings: []domain.Finding{
				{Text: "Finding from 10", ReviewerID: 10},
			},
		},
	}

	findings := CollectFindings(results)

	reviewerIDs := map[int]bool{}
	for _, f := range findings {
		reviewerIDs[f.ReviewerID] = true
	}
	if !reviewerIDs[5] || !reviewerIDs[10] {
		t.Errorf("reviewer IDs not preserved, found: %v", reviewerIDs)
	}
}

func TestNew_EmptyAgentsReturnsError(t *testing.T) {
	cfg := Config{Reviewers: 5, Timeout: time.Minute}

	_, err := New(cfg, []agent.Agent{}, nil)

	if err == nil {
		t.Error("expected error for empty agents slice, got nil")
	}
	if !strings.Contains(err.Error(), "at least one agent") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_NilAgentsReturnsError(t *testing.T) {
	cfg := Config{Reviewers: 5, Timeout: time.Minute}

	_, err := New(cfg, nil, nil)

	if err == nil {
		t.Error("expected error for nil agents slice, got nil")
	}
}

func TestNew_ValidAgentsSucceeds(t *testing.T) {
	cfg := Config{Reviewers: 5, Timeout: time.Minute}
	agents := []agent.Agent{&mockAgent{name: "codex"}}

	r, err := New(cfg, agents, nil)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if r == nil {
		t.Error("expected non-nil runner")
	}
}

func TestBuildStats_TracksAgentNames(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AgentName: "codex", ExitCode: 0, Duration: time.Second},
		{ReviewerID: 2, AgentName: "claude", ExitCode: 1, Duration: time.Second},
		{ReviewerID: 3, AgentName: "agy", ExitCode: 0, Duration: time.Second},
	}

	stats := BuildStats(results, 3, time.Second)

	if stats.ReviewerAgentNames[1] != "codex" {
		t.Errorf("expected agent name 'codex' for reviewer 1, got %q", stats.ReviewerAgentNames[1])
	}
	if stats.ReviewerAgentNames[2] != "claude" {
		t.Errorf("expected agent name 'claude' for reviewer 2, got %q", stats.ReviewerAgentNames[2])
	}
	if stats.ReviewerAgentNames[3] != "agy" {
		t.Errorf("expected agent name 'agy' for reviewer 3, got %q", stats.ReviewerAgentNames[3])
	}
}

type mockAgent struct {
	name string
}

func (m *mockAgent) Name() string {
	return m.name
}

func (m *mockAgent) IsAvailable() error {
	return nil
}

func (m *mockAgent) ExecuteReview(_ context.Context, _ *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	return nil, nil
}

func (m *mockAgent) ExecuteSummary(_ context.Context, _ string, _ []byte) (*agent.ExecutionResult, error) {
	return nil, nil
}

type mockStreamingAgent struct {
	name   string
	output string
}

type cleanupWarningAgent struct {
	name     string
	output   string
	closeErr error
}

func (a *cleanupWarningAgent) Name() string {
	return a.name
}

func (a *cleanupWarningAgent) IsAvailable() error {
	return nil
}

func (a *cleanupWarningAgent) ExecuteReview(context.Context, *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	reader := &errorReadCloser{Reader: strings.NewReader(a.output), err: a.closeErr}
	return agent.NewExecutionResult(reader, func() int { return 0 }, func() string { return "" }), nil
}

func (a *cleanupWarningAgent) ExecuteSummary(context.Context, string, []byte) (*agent.ExecutionResult, error) {
	return nil, nil
}

func (m *mockStreamingAgent) Name() string {
	if m.name == "" {
		return "codex"
	}
	return m.name
}

func (m *mockStreamingAgent) IsAvailable() error {
	return nil
}

func (m *mockStreamingAgent) ExecuteReview(_ context.Context, _ *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	reader := &stringReadCloser{strings.NewReader(m.output)}
	return agent.NewExecutionResult(reader, func() int { return 0 }, func() string { return "" }), nil
}

func (m *mockStreamingAgent) ExecuteSummary(_ context.Context, _ string, _ []byte) (*agent.ExecutionResult, error) {
	return nil, nil
}

func TestRunReviewer_ParserErrorRecovery(t *testing.T) {

	mockAgent := &mockStreamingAgent{
		output: `{"item":{"type":"agent_message","text":"finding 1"}}
invalid json line here
{"item":{"type":"agent_message","text":"finding 2"}}`,
	}

	r := &Runner{
		config:    Config{Reviewers: 1, Timeout: 10 * time.Second},
		agents:    []agent.Agent{mockAgent},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewer(context.Background(), 1)

	if len(result.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Findings))
	}
	if result.ParseErrors != 1 {
		t.Errorf("expected 1 parse error, got %d", result.ParseErrors)
	}
}

func TestRunReviewerCleanupFailureIsNonfatalWarning(t *testing.T) {
	mock := &cleanupWarningAgent{
		name:     "codex",
		output:   `{"item":{"type":"agent_message","text":"finding"}}`,
		closeErr: errors.New("temporary file cleanup failed"),
	}
	r := &Runner{
		config:    Config{Reviewers: 1, Timeout: time.Second},
		agents:    []agent.Agent{mock},
		completed: new(atomic.Int32),
	}

	result := r.runReviewer(context.Background(), 1)

	if result.ExitCode != 0 || result.Failure != nil || len(result.Findings) != 1 {
		t.Fatalf("cleanup failure changed reviewer outcome: %#v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Kind != domain.ReviewerWarningCleanup {
		t.Fatalf("cleanup warning missing: %#v", result.Warnings)
	}
}

func TestHeadlessRunnerEmitsEarlyReturnCleanupWarningWithoutStderr(t *testing.T) {
	mock := &cleanupWarningAgent{
		name:     "unsupported",
		output:   "ignored",
		closeErr: errors.New("temporary file cleanup failed"),
	}
	var completed domain.ReviewerResult
	r, err := NewHeadless(Config{
		Reviewers:   1,
		Concurrency: 1,
		Timeout:     time.Second,
		Events: Events{
			ReviewerCompleted: func(result domain.ReviewerResult) {
				completed = result
			},
		},
	}, []agent.Agent{mock})
	if err != nil {
		t.Fatalf("create headless runner: %v", err)
	}
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() { os.Stderr = originalStderr })

	results, _, runErr := r.Run(context.Background())
	os.Stderr = originalStderr
	if err := stderr.Close(); err != nil {
		t.Fatalf("close stderr capture: %v", err)
	}

	if runErr != nil {
		t.Fatalf("run headless reviewer: %v", runErr)
	}
	if len(results) != 1 || results[0].Failure == nil || results[0].Failure.Kind != domain.ReviewerFailureParser {
		t.Fatalf("early return outcome missing: %#v", results)
	}
	if len(results[0].Warnings) != 1 || results[0].Warnings[0].Kind != domain.ReviewerWarningCleanup {
		t.Fatalf("early-return cleanup warning missing: %#v", results[0])
	}
	if len(completed.Warnings) != 1 || completed.Warnings[0] != results[0].Warnings[0] {
		t.Fatalf("ReviewerCompleted did not receive cleanup warning: completed=%#v result=%#v", completed, results[0])
	}
	captured, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	if len(captured) != 0 {
		t.Fatalf("headless cleanup failure wrote stderr: %q", captured)
	}
}

func TestVerboseRunnerLogsCleanupWarningOnce(t *testing.T) {
	mock := &cleanupWarningAgent{
		name:     "codex",
		output:   `{"item":{"type":"agent_message","text":"finding"}}`,
		closeErr: errors.New("temporary file cleanup failed"),
	}
	stderrPath := filepath.Join(t.TempDir(), "stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() { os.Stderr = originalStderr })
	r := &Runner{
		config:    Config{Reviewers: 1, Timeout: time.Second, Verbose: true},
		agents:    []agent.Agent{mock},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewer(context.Background(), 1)
	os.Stderr = originalStderr
	if err := stderr.Close(); err != nil {
		t.Fatalf("close stderr capture: %v", err)
	}

	if len(result.Warnings) != 1 || result.Warnings[0].Kind != domain.ReviewerWarningCleanup {
		t.Fatalf("typed cleanup warning missing: %#v", result.Warnings)
	}
	captured, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	if strings.Count(string(captured), "close error (non-fatal)") != 1 {
		t.Fatalf("verbose cleanup warning output = %q", captured)
	}
}

func TestRunReviewerParentCancellationTakesPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockStreamingAgent{
		output: `{"item":{"type":"agent_message","text":"finding"}}`,
	}
	r := &Runner{
		config: Config{
			Reviewers: 1,
			Timeout:   time.Minute,
			Events: Events{
				ReviewerStarted: func(int, string) { cancel() },
			},
		},
		agents:    []agent.Agent{mock},
		completed: new(atomic.Int32),
	}

	result := r.runReviewer(ctx, 1)

	if result.Failure == nil || result.Failure.Kind != domain.ReviewerFailureInterrupted {
		t.Fatalf("reviewer cancellation was not classified as interrupted: %#v", result)
	}
	if result.TimedOut || result.AuthFailed || result.ExitCode != -1 {
		t.Fatalf("reviewer cancellation was misclassified: %#v", result)
	}
}

func TestRunReviewer_RecoverableParseError(t *testing.T) {

	mockAgent := &mockStreamingAgent{
		name:   "codex",
		output: "line1\nline2\nline3\n",
	}

	r := &Runner{
		config:    Config{Reviewers: 1, Timeout: 10 * time.Second},
		agents:    []agent.Agent{mockAgent},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewer(context.Background(), 1)

	if result.ParseErrors != 3 {
		t.Errorf("expected 3 parse errors for non-JSON lines, got %d", result.ParseErrors)
	}
}

func TestBuildStats_CategorizesAuthFailedReviewers(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, ExitCode: 0, Duration: time.Second},
		{ReviewerID: 2, ExitCode: 1, AuthFailed: true, AgentName: "agy", Duration: time.Second},
		{ReviewerID: 3, ExitCode: 1, Duration: time.Second},
	}

	stats := BuildStats(results, 3, time.Second)

	if stats.SuccessfulReviewers != 1 {
		t.Errorf("expected 1 successful, got %d", stats.SuccessfulReviewers)
	}
	if len(stats.AuthFailedReviewers) != 1 || stats.AuthFailedReviewers[0] != 2 {
		t.Errorf("expected AuthFailedReviewers=[2], got %v", stats.AuthFailedReviewers)
	}
	if len(stats.FailedReviewers) != 1 || stats.FailedReviewers[0] != 3 {
		t.Errorf("expected FailedReviewers=[3], got %v", stats.FailedReviewers)
	}
}

func TestBuildStats_AllFailedIncludesAuthFailures(t *testing.T) {
	results := []domain.ReviewerResult{
		{ReviewerID: 1, AuthFailed: true, ExitCode: 1},
		{ReviewerID: 2, AuthFailed: true, ExitCode: 1},
	}

	stats := BuildStats(results, 2, time.Second)

	if !stats.AllFailed() {
		t.Error("expected AllFailed() to return true when all reviewers are auth-failed")
	}
}

type mockAuthFailAgent struct {
	name      string
	exitCode  int
	stdout    string
	stderr    string
	callCount atomic.Int32
}

func (m *mockAuthFailAgent) Name() string       { return m.name }
func (m *mockAuthFailAgent) IsAvailable() error { return nil }

func (m *mockAuthFailAgent) ExecuteReview(_ context.Context, _ *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	m.callCount.Add(1)
	reader := &stringReadCloser{strings.NewReader(m.stdout)}
	exitCode := m.exitCode
	stderr := m.stderr
	return agent.NewExecutionResult(reader, func() int { return exitCode }, func() string { return stderr }), nil
}

func (m *mockAuthFailAgent) ExecuteSummary(_ context.Context, _ string, _ []byte) (*agent.ExecutionResult, error) {
	return nil, nil
}

func TestRunReviewerWithRetry_SkipsRetryOnAuthFailure(t *testing.T) {
	mock := &mockAuthFailAgent{name: "agy", exitCode: 1, stderr: "authentication required"}

	r := &Runner{
		config:    Config{Reviewers: 1, Retries: 2, Timeout: 10 * time.Second},
		agents:    []agent.Agent{mock},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewerWithRetry(context.Background(), 1)

	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 call (no retries), got %d", mock.callCount.Load())
	}
	if !result.AuthFailed {
		t.Error("expected AuthFailed to be true")
	}
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
}

func TestRunReviewerWithRetry_SkipsRetryOnStdoutAuthFailure(t *testing.T) {
	mock := &mockAuthFailAgent{
		name:     "claude",
		exitCode: 1,
		stdout:   "Failed to authenticate. API Error: 401 Invalid authentication credentials\n",
	}

	r := &Runner{
		config:    Config{Reviewers: 1, Retries: 2, Timeout: 10 * time.Second},
		agents:    []agent.Agent{mock},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewerWithRetry(context.Background(), 1)

	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 call (no retries), got %d", mock.callCount.Load())
	}
	if !result.AuthFailed {
		t.Error("expected AuthFailed to be true")
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected auth stdout to be discarded, got findings: %v", result.Findings)
	}
}

func TestRunReviewerWithRetry_RetriesNonAuthFailure(t *testing.T) {
	mock := &mockAuthFailAgent{name: "codex", exitCode: 1, stderr: "some error"}

	r := &Runner{
		config:    Config{Reviewers: 1, Retries: 1, Timeout: 10 * time.Second},
		agents:    []agent.Agent{mock},
		logger:    terminal.NewLogger(),
		completed: new(atomic.Int32),
	}

	result := r.runReviewerWithRetry(context.Background(), 1)

	if mock.callCount.Load() != 2 {
		t.Errorf("expected 2 calls (initial + 1 retry), got %d", mock.callCount.Load())
	}
	if result.AuthFailed {
		t.Error("expected AuthFailed to be false for non-auth failure")
	}
}
