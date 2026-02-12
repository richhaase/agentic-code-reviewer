package terminal

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpinner_NonTTY(t *testing.T) {
	s := &Spinner{
		isTTY:     false,
		completed: &atomic.Int32{},
		total:     5,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("spinner did not exit")
	}
}

func TestPhaseSpinner_NonTTY(t *testing.T) {
	s := &PhaseSpinner{
		isTTY: false,
		label: "Testing",
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("phase spinner did not exit")
	}
}

func TestNewSpinner(t *testing.T) {
	s := NewSpinner(10)
	if s.total != 10 {
		t.Errorf("total = %d, want 10", s.total)
	}
	if s.completed == nil {
		t.Error("completed counter should not be nil")
	}
}

func TestNewPhaseSpinner(t *testing.T) {
	s := NewPhaseSpinner("Summarizing")
	if s.label != "Summarizing" {
		t.Errorf("label = %q, want %q", s.label, "Summarizing")
	}
}
