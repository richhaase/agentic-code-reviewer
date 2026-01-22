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

	// UseRefFile writes the diff to a temp file and instructs the agent to read it,
	// instead of embedding the diff in the prompt. This avoids "prompt too long" errors.
	// When false (default), ref-file mode is still used automatically if diff exceeds RefFileSizeThreshold.
	UseRefFile bool
}
