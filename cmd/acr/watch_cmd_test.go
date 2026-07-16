package main

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

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
