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
		outcome CycleOutcome
		want    watch.CycleResult
	}{
		{"no changes", CycleOutcome{Kind: OutcomeNoChanges}, watch.CycleNoChanges},
		{"findings", CycleOutcome{Kind: OutcomeFindings}, watch.CycleFindings},
		{"approved", CycleOutcome{Kind: OutcomeLGTMApproved}, watch.CycleLGTMApproved},
		{"comment", CycleOutcome{Kind: OutcomeLGTMComment}, watch.CycleLGTMComment},
		{"comment via CI downgrade", CycleOutcome{Kind: OutcomeLGTMComment, CIDowngraded: true}, watch.CycleLGTMCommentCIPending},
		{"declined", CycleOutcome{Kind: OutcomeLGTMDeclined}, watch.CycleLGTMDeclined},
		{"skipped", CycleOutcome{Kind: OutcomeLGTMSkipped}, watch.CycleLGTMSkipped},
		{"stale head", CycleOutcome{Kind: OutcomeStaleHead}, watch.CycleStaleHead},
		{"unrecorded is an error", CycleOutcome{}, watch.CycleError},
	}
	for _, tt := range tests {
		if got := mapCycleOutcome(&tt.outcome); got != tt.want {
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
