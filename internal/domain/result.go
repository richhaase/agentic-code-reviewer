package domain

import "time"

// ReviewerResult holds the result from a single reviewer run.
type ReviewerResult struct {
	ReviewerID  int
	AgentName   string // Which agent type was used (codex, claude, gemini)
	Findings    []Finding
	SkillsUsed  string // Skills used by this reviewer (if reported)
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
	ReviewerAgentNames  map[int]string // Reviewer ID → agent name
	SkillsUsed          []string       // Skills used across all reviewers
	WallClockDuration   time.Duration
	SummarizerDuration  time.Duration
}

// AllFailed returns true if all reviewers failed.
func (s *ReviewStats) AllFailed() bool {
	totalFailures := len(s.FailedReviewers) + len(s.TimedOutReviewers)
	return totalFailures >= s.TotalReviewers
}
