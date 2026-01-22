package agent

import "time"

// ReviewConfig contains the configuration for a code review execution.
type ReviewConfig struct {
	// BaseRef is the git reference to compare against (e.g., "main", "HEAD~1").
	BaseRef string

	// Timeout is the maximum duration for the review execution.
	Timeout time.Duration

	// WorkDir is the working directory for the review (defaults to current directory).
	WorkDir string

	// Verbose enables verbose output from the agent.
	Verbose bool

	// CustomPrompt is an optional custom prompt to pass to the agent.
	// If empty, the agent uses its default review prompt.
	CustomPrompt string

	// ReviewerID is a unique identifier for this reviewer instance (e.g., "reviewer-1").
	ReviewerID string

	// DiffOverride, if non-empty, is used instead of fetching the diff from git.
	// This allows the runner to distribute different portions of a large diff
	// to different reviewers for parallel processing.
	DiffOverride string
}
