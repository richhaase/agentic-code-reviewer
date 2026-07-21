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

type Spinner struct {
	isTTY     bool
	completed *atomic.Int32
	total     int
}

func NewSpinner(total int) *Spinner {
	return &Spinner{
		isTTY:     IsStderrTTY(),
		completed: &atomic.Int32{},
		total:     total,
	}
}

func (s *Spinner) Completed() *atomic.Int32 {
	return s.completed
}

func (s *Spinner) Run(ctx context.Context) {
	if !s.isTTY {
		<-ctx.Done()
		return
	}

	idx := 0
	start := time.Now()
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():

			elapsed := FormatDuration(time.Since(start))
			progress := fmt.Sprintf("%d/%d", s.completed.Load(), s.total)
			tag := fmt.Sprintf("%s[%s%s✓%s%s]%s",
				Color(Dim), Color(Reset), Color(Green), Color(Reset), Color(Dim), Color(Reset))
			final := fmt.Sprintf("\r%s Reviewers complete %s(%s, %s)%s",
				tag, Color(Dim), progress, elapsed, Color(Reset))
			fmt.Fprint(os.Stderr, final+"          \n")
			return

		case <-ticker.C:
			elapsed := FormatDuration(time.Since(start))
			frame := string(spinnerFrames[idx%len(spinnerFrames)])
			progress := fmt.Sprintf("%d/%d", s.completed.Load(), s.total)
			tag := fmt.Sprintf("%s[%s%s%s%s%s]%s",
				Color(Dim), Color(Reset), Color(Cyan), frame, Color(Reset), Color(Dim), Color(Reset))
			line := fmt.Sprintf("\r%s Running reviewers %s(%s, %s)%s",
				tag, Color(Dim), progress, elapsed, Color(Reset))
			fmt.Fprint(os.Stderr, line+"          ")
			idx++
		}
	}
}

type PhaseSpinner struct {
	isTTY bool
	label string
}

func NewPhaseSpinner(label string) *PhaseSpinner {
	return &PhaseSpinner{
		isTTY: IsStderrTTY(),
		label: label,
	}
}

func (s *PhaseSpinner) Run(ctx context.Context) {
	if !s.isTTY {
		<-ctx.Done()
		return
	}

	idx := 0
	start := time.Now()
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():

			elapsed := FormatDuration(time.Since(start))
			tag := fmt.Sprintf("%s[%s%s✓%s%s]%s",
				Color(Dim), Color(Reset), Color(Green), Color(Reset), Color(Dim), Color(Reset))
			final := fmt.Sprintf("\r%s %s %s(%s)%s",
				tag, s.label, Color(Dim), elapsed, Color(Reset))
			fmt.Fprint(os.Stderr, final+"          \n")
			return

		case <-ticker.C:
			elapsed := FormatDuration(time.Since(start))
			frame := string(spinnerFrames[idx%len(spinnerFrames)])
			tag := fmt.Sprintf("%s[%s%s%s%s%s]%s",
				Color(Dim), Color(Reset), Color(Cyan), frame, Color(Reset), Color(Dim), Color(Reset))
			line := fmt.Sprintf("\r%s %s %s(%s)%s",
				tag, s.label, Color(Dim), elapsed, Color(Reset))
			fmt.Fprint(os.Stderr, line+"          ")
			idx++
		}
	}
}
