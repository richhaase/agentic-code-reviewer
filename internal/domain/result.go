package domain

import "time"

type ReviewerFailureKind string

const (
	ReviewerFailureExecution   ReviewerFailureKind = "execution"
	ReviewerFailureExit        ReviewerFailureKind = "exit"
	ReviewerFailureTimeout     ReviewerFailureKind = "timeout"
	ReviewerFailureAuth        ReviewerFailureKind = "authentication"
	ReviewerFailureInterrupted ReviewerFailureKind = "interrupted"
	ReviewerFailureParser      ReviewerFailureKind = "parser"
)

type ReviewerFailure struct {
	Kind    ReviewerFailureKind
	Message string
}

type ReviewerWarningKind string

const ReviewerWarningCleanup ReviewerWarningKind = "cleanup"

type ReviewerWarning struct {
	Kind    ReviewerWarningKind
	Message string
}

type ReviewerResult struct {
	ReviewerID  int
	AgentName   string
	Findings    []Finding
	ExitCode    int
	Attempts    int
	ParseErrors int
	TimedOut    bool
	AuthFailed  bool
	Duration    time.Duration
	Failure     *ReviewerFailure
	Warnings    []ReviewerWarning
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
