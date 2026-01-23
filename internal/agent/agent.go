package agent

import (
	"context"
	"io"
)

// RefFileSizeThreshold is the diff size (in bytes) above which ref-file mode
// is automatically used to avoid "prompt too long" errors.
const RefFileSizeThreshold = 100 * 1024 // 100KB

// Agent represents a backend that can execute code reviews and summarizations.
// Implementations include CodexAgent, ClaudeAgent, GeminiAgent.
type Agent interface {
	// Name returns the agent's identifier (e.g., "codex", "claude", "gemini").
	Name() string

	// IsAvailable checks if the agent's backend CLI is installed and accessible.
	// Returns an error if the agent cannot be used.
	IsAvailable() error

	// ExecuteReview runs a code review with the given configuration.
	// Returns an io.Reader for streaming output and an error if execution fails.
	// The caller is responsible for closing the reader if it implements io.Closer.
	// If the reader implements ExitCoder, the caller can retrieve the process exit code.
	ExecuteReview(ctx context.Context, config *ReviewConfig) (io.Reader, error)

	// ExecuteSummary runs a summarization task with the given prompt and input data.
	// The prompt contains the summarization instructions.
	// The input contains the data to summarize (typically JSON-encoded aggregated findings).
	// Returns an io.Reader for the output.
	// The caller is responsible for closing the reader if it implements io.Closer.
	ExecuteSummary(ctx context.Context, prompt string, input []byte) (io.Reader, error)
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
