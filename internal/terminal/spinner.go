package terminal

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

const spinnerInterval = 200 * time.Millisecond

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// Spinner displays an animated spinner with progress information.
type Spinner struct {
	isTTY     bool
	completed *atomic.Int32
	total     int
}

// NewSpinner creates a new spinner.
func NewSpinner(total int) *Spinner {
	return &Spinner{
		isTTY:     IsStderrTTY(),
		completed: &atomic.Int32{},
		total:     total,
	}
}

// Completed returns a pointer to the atomic counter for completed items.
func (s *Spinner) Completed() *atomic.Int32 {
	return s.completed
}

// Run runs the spinner until the context is cancelled.
func (s *Spinner) Run(ctx context.Context) {
	if !s.isTTY {
		<-ctx.Done()
		return
	}

	idx := 0
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final state
			progress := fmt.Sprintf("%d/%d", s.completed.Load(), s.total)
			tag := fmt.Sprintf("%s[%s%sreview%s%s]%s",
				Color(Dim), Color(Reset), Color(Green), Color(Reset), Color(Dim), Color(Reset))
			final := fmt.Sprintf("\r%s %s✓%s Reviewers complete %s(%s)%s",
				tag, Color(Green), Color(Reset), Color(Dim), progress, Color(Reset))
			fmt.Fprint(os.Stderr, final+"          \n")
			return

		case <-ticker.C:
			frame := string(spinnerFrames[idx%len(spinnerFrames)])
			progress := fmt.Sprintf("%d/%d", s.completed.Load(), s.total)
			tag := fmt.Sprintf("%s[%s%sreview%s%s]%s",
				Color(Dim), Color(Reset), Color(Cyan), Color(Reset), Color(Dim), Color(Reset))
			line := fmt.Sprintf("\r%s %s%s%s Running reviewers %s(%s)%s",
				tag, Color(Cyan), frame, Color(Reset), Color(Dim), progress, Color(Reset))
			fmt.Fprint(os.Stderr, line+"          ")
			idx++
		}
	}
}

// PhaseSpinner displays a simple spinner for a single phase.
type PhaseSpinner struct {
	isTTY bool
	label string
}

// NewPhaseSpinner creates a new phase spinner.
func NewPhaseSpinner(label string) *PhaseSpinner {
	return &PhaseSpinner{
		isTTY: IsStderrTTY(),
		label: label,
	}
}

// Run runs the phase spinner until the context is cancelled.
func (s *PhaseSpinner) Run(ctx context.Context) {
	if !s.isTTY {
		<-ctx.Done()
		return
	}

	idx := 0
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final state
			tag := fmt.Sprintf("%s[%s%sreview%s%s]%s",
				Color(Dim), Color(Reset), Color(Green), Color(Reset), Color(Dim), Color(Reset))
			final := fmt.Sprintf("\r%s %s✓%s %s",
				tag, Color(Green), Color(Reset), s.label)
			fmt.Fprint(os.Stderr, final+"          \n")
			return

		case <-ticker.C:
			frame := string(spinnerFrames[idx%len(spinnerFrames)])
			tag := fmt.Sprintf("%s[%s%sreview%s%s]%s",
				Color(Dim), Color(Reset), Color(Cyan), Color(Reset), Color(Dim), Color(Reset))
			line := fmt.Sprintf("\r%s %s%s%s %s",
				tag, Color(Cyan), frame, Color(Reset), s.label)
			fmt.Fprint(os.Stderr, line+"          ")
			idx++
		}
	}
}
