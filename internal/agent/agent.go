package agent

import (
	"context"
)

// Agent represents a backend that can execute code reviews and summarizations.
// Implementations include CodexAgent, ClaudeAgent, GeminiAgent.
type Agent interface {
	// Name returns the agent's identifier (e.g., "codex", "claude", "gemini").
	Name() string

	// IsAvailable checks if the agent's backend CLI is installed and accessible.
	// Returns an error if the agent cannot be used.
	IsAvailable() error

	// ExecuteReview runs a code review with the given configuration.
	// Returns an ExecutionResult for streaming output and an error if execution fails.
	// The caller MUST call Close() on the result to ensure proper resource cleanup.
	// After Close(), ExitCode() and Stderr() return valid values.
	ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error)

	// ExecuteSummary runs a summarization task with the given prompt and input data.
	// The prompt contains the summarization instructions.
	// The input contains the data to summarize (typically JSON-encoded aggregated findings).
	// Returns an ExecutionResult for streaming output.
	// The caller MUST call Close() on the result to ensure proper resource cleanup.
	// After Close(), ExitCode() and Stderr() return valid values.
	ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error)
}

// ExitCoder is an optional interface for readers that can report process exit codes.
// Readers returned by Agent.ExecuteReview may implement this interface.
// The exit code is only valid after Close() has been called.
type ExitCoder interface {
	ExitCode() int
}

// StderrProvider is an optional interface for readers that capture subprocess stderr.
// Readers returned by Agent.ExecuteReview may implement this interface.
// The stderr output is only valid after Close() has been called.
type StderrProvider interface {
	Stderr() string
}
