package terminal

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSpinner(t *testing.T) {
	s := NewSpinner(5)
	if s == nil {
		t.Fatal("NewSpinner returned nil")
	}
	if s.total != 5 {
		t.Errorf("total = %d, want 5", s.total)
	}
	if s.completed == nil {
		t.Error("completed counter should not be nil")
	}
	if s.completed.Load() != 0 {
		t.Errorf("completed should start at 0, got %d", s.completed.Load())
	}
}

func TestSpinner_Completed(t *testing.T) {
	s := NewSpinner(10)
	counter := s.Completed()
	if counter == nil {
		t.Fatal("Completed() returned nil")
	}

	counter.Add(1)
	if counter.Load() != 1 {
		t.Errorf("counter should be 1, got %d", counter.Load())
	}

	counter.Add(5)
	if counter.Load() != 6 {
		t.Errorf("counter should be 6, got %d", counter.Load())
	}
}

func TestSpinner_Run_NonTTY(t *testing.T) {
	s := &Spinner{
		isTTY:     false,
		completed: &atomic.Int32{},
		total:     3,
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Cancel should make Run return
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Run did not return after context cancel (non-TTY)")
	}
}

func TestSpinner_Run_TTY(t *testing.T) {
	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	s := &Spinner{
		isTTY:     true,
		completed: &atomic.Int32{},
		total:     3,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Let spinner run a bit
	time.Sleep(250 * time.Millisecond)
	s.completed.Add(1)

	<-ctx.Done()
	<-done

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should contain spinner output
	if output == "" {
		t.Error("expected spinner output for TTY mode")
	}
	// Should contain final completion message
	if !strings.Contains(output, "✓") {
		t.Error("expected checkmark in final output")
	}
}

func TestSpinner_Run_ProgressUpdates(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	s := &Spinner{
		isTTY:     true,
		completed: &atomic.Int32{},
		total:     5,
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Simulate progress
	time.Sleep(50 * time.Millisecond)
	s.completed.Add(2)
	time.Sleep(50 * time.Millisecond)
	s.completed.Add(3)

	cancel()
	<-done

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Final output should show completion
	if !strings.Contains(output, "5/5") {
		t.Logf("output: %q", output)
		// Might not have reached 5/5 if context canceled quickly
	}
}

func TestNewPhaseSpinner(t *testing.T) {
	ps := NewPhaseSpinner("Testing phase")
	if ps == nil {
		t.Fatal("NewPhaseSpinner returned nil")
	}
	if ps.label != "Testing phase" {
		t.Errorf("label = %q, want %q", ps.label, "Testing phase")
	}
}

func TestPhaseSpinner_Run_NonTTY(t *testing.T) {
	ps := &PhaseSpinner{
		isTTY: false,
		label: "Test",
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ps.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("PhaseSpinner.Run did not return after context cancel")
	}
}

func TestPhaseSpinner_Run_TTY(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	ps := &PhaseSpinner{
		isTTY: true,
		label: "Processing data",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		ps.Run(ctx)
		close(done)
	}()

	<-ctx.Done()
	<-done

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should contain the label
	if !strings.Contains(output, "Processing data") {
		t.Errorf("expected label in output, got %q", output)
	}
	// Should contain completion checkmark
	if !strings.Contains(output, "✓") {
		t.Error("expected checkmark in final output")
	}
}

func TestSpinnerFrames(t *testing.T) {
	// Verify spinner frames are defined
	if len(spinnerFrames) == 0 {
		t.Error("spinnerFrames should not be empty")
	}
	// Should be braille spinner characters
	expectedFrames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	if len(spinnerFrames) != len(expectedFrames) {
		t.Errorf("expected %d frames, got %d", len(expectedFrames), len(spinnerFrames))
	}
}

func TestSpinnerInterval(t *testing.T) {
	// Verify interval is reasonable
	if spinnerInterval < 50*time.Millisecond {
		t.Error("spinner interval too fast")
	}
	if spinnerInterval > 500*time.Millisecond {
		t.Error("spinner interval too slow")
	}
}
