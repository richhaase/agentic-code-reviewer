// Package backend defines the interface for AI provider backends.
package backend

import (
	"context"
	"time"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

// Result contains the output from a backend summarization request.
type Result struct {
	Grouped  domain.GroupedFindings
	ExitCode int
	Stderr   string
	RawOut   string
	Duration time.Duration
}

// Backend defines the interface that all AI provider backends must implement.
type Backend interface {
	// Summarize takes aggregated findings and returns grouped/clustered findings
	// using the backend's AI provider.
	Summarize(ctx context.Context, aggregated []domain.AggregatedFinding) (*Result, error)
}
