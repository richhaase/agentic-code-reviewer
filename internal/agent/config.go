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

	// Guidance is optional steering context appended to the agent's default prompt.
	// If empty, the agent uses its default prompt as-is.
	Guidance string

	// ReviewerID is a unique identifier for this reviewer instance (e.g., "reviewer-1").
	ReviewerID string

	// UseRefFile writes the diff to a temp file and instructs the agent to read it,
	// instead of embedding the diff in the prompt. This avoids "prompt too long" errors.
	// When false (default), ref-file mode is still used automatically if diff exceeds RefFileSizeThreshold.
	UseRefFile bool

	// Diff is the pre-computed git diff content. When DiffPrecomputed is true, agents
	// use this instead of calling git diff themselves. This avoids running the same
	// diff N times across parallel reviewers. Codex ignores this (built-in diff via --base).
	Diff string

	// DiffPrecomputed indicates that Diff was pre-computed and should be used as-is,
	// even if empty. When false, agents that need a diff will fetch it themselves.
	DiffPrecomputed bool
}
