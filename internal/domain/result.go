package domain

import "time"

// ReviewerResult holds the result from a single reviewer run.
type ReviewerResult struct {
	ReviewerID  int
	AgentName   string // Which agent type was used (codex, claude, gemini)
	Findings    []Finding
	ExitCode    int
	ParseErrors int
	TimedOut    bool
	Duration    time.Duration
}

type ReviewStats struct {
	TotalReviewers      int
	SuccessfulReviewers int
	FailedReviewers     []int
	TimedOutReviewers   []int
	ParseErrors         int
	ReviewerDurations   map[int]time.Duration
	ReviewerAgentNames  map[int]string
	WallClockDuration   time.Duration
	SummarizerDuration  time.Duration
	FPFilterDuration    time.Duration
	FPFilteredCount     int
	IgnoredCount        int
}

// AllFailed returns true if all reviewers failed.
func (s *ReviewStats) AllFailed() bool {
	totalFailures := len(s.FailedReviewers) + len(s.TimedOutReviewers)
	return totalFailures >= s.TotalReviewers
}
