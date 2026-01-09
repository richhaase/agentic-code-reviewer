package domain

import "time"

// ReviewerResult holds the result from a single reviewer run.
type ReviewerResult struct {
	ReviewerID  int
	Findings    []Finding
	ExitCode    int
	ParseErrors int
	TimedOut    bool
	Duration    time.Duration
}

// ReviewStats holds statistics about the review run.
type ReviewStats struct {
	TotalReviewers      int
	SuccessfulReviewers int
	FailedReviewers     []int
	TimedOutReviewers   []int
	ParseErrors         int
	ReviewerDurations   map[int]time.Duration
	WallClockDuration   time.Duration
	SummarizerDuration  time.Duration
}

// AllFailed returns true if all reviewers failed.
func (s *ReviewStats) AllFailed() bool {
	totalFailures := len(s.FailedReviewers) + len(s.TimedOutReviewers)
	return totalFailures >= s.TotalReviewers
}
