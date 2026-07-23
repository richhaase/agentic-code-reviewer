package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

type watchConfigSource struct {
	result *config.LoadResult
	err    error
}

func (s watchConfigSource) LoadWithWarnings(context.Context) (*config.LoadResult, error) {
	return s.result, s.err
}

func TestWatchRejectsOneShotOnlyFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--yes"},
		{"-y"},
		{"--local"},
		{"-l"},
		{"--worktree-branch", "feature"},
		{"-B", "feature"},
	} {
		cmd := newWatchCmd()
		if err := cmd.ParseFlags(args); err == nil {
			t.Errorf("watch must reject %v with a usage error", args)
		}
	}
}

func TestWatchAcceptsSharedAndWatchFlags(t *testing.T) {
	cmd := newWatchCmd()
	err := cmd.ParseFlags([]string{
		"--pr", "42",
		"--reviewers", "3",
		"--reviewer-agent", "codex",
		"--post-mode", "comment",
		"--poll-interval", "30s",
		"--settle-time", "5m",
		"--max-reviews", "4",
		"--max-duration", "2h",
	})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

func TestMapCycleOutcome(t *testing.T) {
	tests := []struct {
		name    string
		run     *domain.ReviewRun
		outcome CycleOutcome
		want    watch.CycleResult
	}{
		{"no changes", nil, CycleOutcome{Kind: OutcomeNoChanges}, watch.CycleNoChanges},
		{"findings", nil, CycleOutcome{Kind: OutcomeFindings}, watch.CycleFindings},
		{"approved", nil, CycleOutcome{Kind: OutcomeLGTMApproved}, watch.CycleLGTMApproved},
		{"comment", nil, CycleOutcome{Kind: OutcomeLGTMComment}, watch.CycleLGTMComment},
		{"comment via CI downgrade", nil, CycleOutcome{Kind: OutcomeLGTMComment, CIDowngraded: true}, watch.CycleLGTMCommentCIPending},
		{"declined", nil, CycleOutcome{Kind: OutcomeLGTMDeclined}, watch.CycleLGTMDeclined},
		{"skipped", nil, CycleOutcome{Kind: OutcomeLGTMSkipped}, watch.CycleLGTMSkipped},
		{"stale head", nil, CycleOutcome{Kind: OutcomeStaleHead}, watch.CycleStaleHead},
		{"unrecorded is an error with nil run", nil, CycleOutcome{}, watch.CycleError},
		{
			"run conclusion no changes drives result over a mismatched outcome",
			&domain.ReviewRun{Conclusion: domain.ReviewConclusionNoChanges},
			CycleOutcome{Kind: OutcomeLGTMApproved},
			watch.CycleNoChanges,
		},
		{
			"run conclusion findings drives result over a mismatched outcome",
			&domain.ReviewRun{Conclusion: domain.ReviewConclusionFindings},
			CycleOutcome{Kind: OutcomeLGTMApproved},
			watch.CycleFindings,
		},
		{
			"run conclusion clean falls back to the outcome kind",
			&domain.ReviewRun{Conclusion: domain.ReviewConclusionClean},
			CycleOutcome{Kind: OutcomeLGTMApproved},
			watch.CycleLGTMApproved,
		},
	}
	for _, tt := range tests {
		if got := mapCycleOutcome(tt.run, &tt.outcome); got != tt.want {
			t.Errorf("%s: mapCycleOutcome = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWatchRejectsPositionalArgs(t *testing.T) {
	cmd := newWatchCmd()
	if err := cmd.ValidateArgs([]string{"123"}); err == nil {
		t.Error("watch must reject positional args; a bare PR number would be silently ignored")
	}
}

func TestInitialTrustedConfigurationRetries(t *testing.T) {
	attempts := 0
	sleeps := 0
	pollInterval := 30 * time.Second

	result, err := resolveInitialTrustedReviewConfiguration(
		context.Background(),
		pollInterval,
		func(context.Context) (configResult, error) {
			attempts++
			if attempts < 3 {
				return configResult{}, errors.New("remote unavailable")
			}
			return configResult{resolved: config.ResolvedConfig{WatchPollInterval: pollInterval}}, nil
		},
		func(_ context.Context, duration time.Duration) error {
			if duration != pollInterval {
				t.Fatalf("sleep duration = %s", duration)
			}
			sleeps++
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || sleeps != 2 {
		t.Fatalf("attempts = %d, sleeps = %d", attempts, sleeps)
	}
	if result.resolved.WatchPollInterval != pollInterval {
		t.Fatalf("poll interval = %s", result.resolved.WatchPollInterval)
	}
}

func TestInitialTrustedConfigurationStopsAfterLimit(t *testing.T) {
	attempts := 0
	sleeps := 0

	_, err := resolveInitialTrustedReviewConfiguration(
		context.Background(),
		time.Second,
		func(context.Context) (configResult, error) {
			attempts++
			return configResult{}, errors.New("remote unavailable")
		},
		func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
		nil,
	)
	if err == nil {
		t.Fatal("resolveInitialTrustedReviewConfiguration succeeded")
	}
	if attempts != maxInitialTrustedConfigAttempts || sleeps != maxInitialTrustedConfigAttempts-1 {
		t.Fatalf("attempts = %d, sleeps = %d", attempts, sleeps)
	}
}

func TestInitialTrustedConfigurationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	_, err := resolveInitialTrustedReviewConfiguration(
		ctx,
		time.Second,
		func(context.Context) (configResult, error) {
			attempts++
			return configResult{}, errors.New("remote unavailable")
		},
		func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestInitialTrustedConfigurationUsesTrustedConfigRetryInterval(t *testing.T) {
	attempts := 0
	var sleeps []time.Duration
	trustedInterval := 17 * time.Second

	_, err := resolveInitialTrustedReviewConfiguration(
		context.Background(),
		time.Minute,
		func(context.Context) (configResult, error) {
			attempts++
			result := configResult{resolved: config.ResolvedConfig{WatchPollInterval: trustedInterval}}
			if attempts == 1 {
				return result, errors.New("guidance unavailable")
			}
			return result, nil
		},
		func(_ context.Context, duration time.Duration) error {
			sleeps = append(sleeps, duration)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 1 || sleeps[0] != trustedInterval {
		t.Fatalf("sleeps = %v", sleeps)
	}
}

func TestResolveWatchPollIntervalUsesEnvironmentAndFlagPrecedence(t *testing.T) {
	originalPollInterval := watchPollInterval
	t.Cleanup(func() { watchPollInterval = originalPollInterval })
	t.Setenv("ACR_WATCH_POLL_INTERVAL", "23s")
	cmd := newWatchCmd()
	if got := resolveWatchPollInterval(cmd, nil); got != 23*time.Second {
		t.Fatalf("environment poll interval = %s", got)
	}
	if err := cmd.ParseFlags([]string{"--poll-interval", "7s"}); err != nil {
		t.Fatal(err)
	}
	if got := resolveWatchPollInterval(cmd, nil); got != 7*time.Second {
		t.Fatalf("flag poll interval = %s", got)
	}
}

func TestFailedTrustedConfigLoadRetainsSafeRetryInterval(t *testing.T) {
	originalPollInterval := watchPollInterval
	t.Cleanup(func() { watchPollInterval = originalPollInterval })
	interval := config.Duration(19 * time.Second)
	source := watchConfigSource{
		result: &config.LoadResult{Config: &config.Config{Watch: config.WatchConfig{PollInterval: &interval}}},
		err:    errors.New("trusted config validation failed"),
	}
	cmd := newWatchCmd()
	result, err := loadAndResolveConfig(context.Background(), cmd, worktreeResult{}, source, terminal.NewLogger())
	if err == nil {
		t.Fatal("loadAndResolveConfig succeeded")
	}
	if result.resolved.WatchPollInterval != 19*time.Second {
		t.Fatalf("retry interval = %s", result.resolved.WatchPollInterval)
	}
}

func TestFailedTrustedConfigLoadEmitsWarnings(t *testing.T) {
	source := watchConfigSource{
		result: &config.LoadResult{
			Config:   &config.Config{},
			Warnings: []string{"unknown trusted configuration key"},
		},
		err: errors.New("trusted config validation failed"),
	}
	cmd := newWatchCmd()

	output := captureWatchStderr(t, func() {
		_, _ = loadAndResolveConfig(context.Background(), cmd, worktreeResult{}, source, terminal.NewLogger())
	})
	if !strings.Contains(output, "Warning: unknown trusted configuration key") {
		t.Fatalf("stderr = %q", output)
	}
}

func TestTrustedConfigSourceReturningNoResultFailsClosed(t *testing.T) {
	source := watchConfigSource{}
	cmd := newWatchCmd()
	var loadErr error

	output := captureWatchStderr(t, func() {
		_, loadErr = loadAndResolveConfig(context.Background(), cmd, worktreeResult{}, source, terminal.NewLogger())
	})
	if loadErr == nil {
		t.Fatal("loadAndResolveConfig succeeded")
	}
	if !strings.Contains(output, "trusted review configuration source returned no result") {
		t.Fatalf("stderr = %q", output)
	}
}

func TestBuildWatchReviewOptsProducesRequestScopedToWatchTrigger(t *testing.T) {
	cfgResult := configResult{
		resolved: config.ResolvedConfig{
			Reviewers:         2,
			Concurrency:       2,
			Base:              "main",
			Timeout:           2 * time.Minute,
			ReviewerAgents:    []string{"codex"},
			SummarizerAgent:   "codex",
			SummarizerTimeout: 3 * time.Minute,
			FPFilterTimeout:   3 * time.Minute,
			FPThreshold:       75,
		},
		source: config.SourceIdentity{
			Kind:          config.SourceKindRepositoryRevision,
			Locator:       "/canonical/repo",
			Ref:           "refs/acr/trusted-config/origin/main",
			Revision:      "canonical-revision",
			ConfigPresent: true,
			ConfigDigest:  "canonical-digest",
		},
	}
	wt := worktreeResult{
		repositoryRoot: "/canonical/repo",
		workDir:        "/canonical/repo/.worktrees/pr-42",
	}
	outcome := &CycleOutcome{}
	opts := buildWatchReviewOpts(cfgResult, wt, "42", watch.PostModeComment, "deadbeef", outcome)

	if opts.Trigger != domain.ReviewTriggerWatch {
		t.Fatalf("opts.Trigger = %q, want %q", opts.Trigger, domain.ReviewTriggerWatch)
	}
	if opts.RepositoryRoot != wt.repositoryRoot {
		t.Fatalf("opts.RepositoryRoot = %q, want %q", opts.RepositoryRoot, wt.repositoryRoot)
	}
	if opts.ConfigSource != cfgResult.source {
		t.Fatalf("opts.ConfigSource = %#v, want %#v", opts.ConfigSource, cfgResult.source)
	}

	sink := &noopReviewEventSink{}
	request, err := newReviewRequest(opts, cfgResult.resolved.Base, sink)
	if err != nil {
		t.Fatal(err)
	}
	service := &capturedReviewService{run: &domain.ReviewRun{
		Status:     domain.ReviewStatusCompleted,
		Conclusion: domain.ReviewConclusionNoChanges,
	}}
	if _, err := service.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	if service.request.Trigger != domain.ReviewTriggerWatch {
		t.Fatalf("request.Trigger = %q, want %q", service.request.Trigger, domain.ReviewTriggerWatch)
	}
	if service.request.Target.RepositoryRoot != wt.repositoryRoot {
		t.Fatalf("request.Target.RepositoryRoot = %q, want %q", service.request.Target.RepositoryRoot, wt.repositoryRoot)
	}
	wantConfigurationSource := configurationSourceIdentity(cfgResult.source)
	if service.request.ConfigurationSource != wantConfigurationSource {
		t.Fatalf("request.ConfigurationSource = %#v, want %#v", service.request.ConfigurationSource, wantConfigurationSource)
	}
}

func captureWatchStderr(t *testing.T, run func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	func() {
		os.Stderr = writer
		defer func() { os.Stderr = original }()
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
