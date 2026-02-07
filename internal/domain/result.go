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
	AuthFailed  bool
	Duration    time.Duration
}

type ReviewStats struct {
	TotalReviewers      int
	SuccessfulReviewers int
	FailedReviewers     []int
	TimedOutReviewers   []int
	AuthFailedReviewers []int
	ParseErrors         int
	ReviewerDurations   map[int]time.Duration
	ReviewerAgentNames  map[int]string
	WallClockDuration   time.Duration
	SummarizerDuration  time.Duration
	FPFilterDuration    time.Duration
	FPFilteredCount     int
}

// AllFailed returns true if all reviewers failed.
func (s *ReviewStats) AllFailed() bool {
	totalFailures := len(s.FailedReviewers) + len(s.TimedOutReviewers) + len(s.AuthFailedReviewers)
	return totalFailures >= s.TotalReviewers
}
