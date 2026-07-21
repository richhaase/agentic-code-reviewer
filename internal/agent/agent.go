package agent

import (
	"context"
)

type Agent interface {
	Name() string

	IsAvailable() error

	ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error)

	ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error)
}
