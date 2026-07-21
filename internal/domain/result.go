package domain

import "time"

type ReviewerResult struct {
	ReviewerID  int
	AgentName   string
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

func (s *ReviewStats) AllFailed() bool {
	totalFailures := len(s.FailedReviewers) + len(s.TimedOutReviewers) + len(s.AuthFailedReviewers)
	return totalFailures >= s.TotalReviewers
}
