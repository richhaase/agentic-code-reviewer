package agent

import (
	"context"
	"io"
)

// Agent represents a code review backend that can execute reviews.
// Implementations include CodexAgent, ClaudeAgent, GeminiAgent, etc.
type Agent interface {
	// Name returns the agent's identifier (e.g., "codex", "claude", "gemini").
	Name() string

	// IsAvailable checks if the agent's backend CLI is installed and accessible.
	// Returns an error if the agent cannot be used.
	IsAvailable() error

	// Execute runs a code review with the given configuration.
	// Returns an io.Reader for streaming output and an error if execution fails.
	// The caller is responsible for closing the reader if it implements io.Closer.
	// If the reader implements ExitCoder, the caller can retrieve the process exit code.
	Execute(ctx context.Context, config *AgentConfig) (io.Reader, error)
}

// ExitCoder is an optional interface for readers that can report process exit codes.
// Readers returned by Agent.Execute may implement this interface.
// The exit code is only valid after Close() has been called.
type ExitCoder interface {
	ExitCode() int
}
