package watch

import (
	"context"
	"time"
)

// Clock abstracts wall-clock time so the watch loop can be tested
// deterministically.
type Clock interface {
	Now() time.Time
	// Sleep blocks for d or until ctx is canceled, returning ctx.Err() in the
	// latter case.
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock implements Clock with the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// Sleep waits for d or context cancellation.
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
