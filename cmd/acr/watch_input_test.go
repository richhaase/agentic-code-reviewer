package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/muesli/cancelreader"

	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

type waitOutcome struct {
	result watch.WaitResult
	err    error
}

func TestWatchInputAdapterReadsAndCoalescesManualRequests(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{}, 1)
	released := make(chan struct{}, 1)
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			activated <- struct{}{}
			return func() error {
				released <- struct{}{}
				return nil
			}, nil
		},
	}
	outcome := make(chan waitOutcome, 1)
	go func() {
		result, err := adapter.Wait(context.Background(), time.Hour)
		outcome <- waitOutcome{result: result, err: err}
	}()

	<-activated
	if _, err := writer.Write([]byte("rrr")); err != nil {
		t.Fatal(err)
	}
	got := <-outcome
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.ManualRequests != 3 {
		t.Fatalf("manual requests = %d, want 3", got.result.ManualRequests)
	}
	select {
	case <-released:
		t.Fatal("manual input released before the pre-review handoff")
	default:
	}
	stateReady := make(chan struct{})
	finalized := make(chan waitOutcome, 1)
	go func() {
		result, err := got.result.Finalize(context.Background(), stateReady)
		finalized <- waitOutcome{result: result, err: err}
	}()
	if _, err := writer.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	close(stateReady)
	final := <-finalized
	additional, err := final.result, final.err
	if err != nil {
		t.Fatal(err)
	}
	if additional.ManualRequests != 1 {
		t.Fatalf("additional manual requests = %d, want 1", additional.ManualRequests)
	}
	<-released

	nextRead := make(chan byte, 1)
	go func() {
		buffer := make([]byte, 1)
		if _, err := reader.Read(buffer); err == nil {
			nextRead <- buffer[0]
		}
	}()
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-nextRead:
		if key != 'x' {
			t.Fatalf("key = %q, want x", key)
		}
	case <-time.After(time.Second):
		t.Fatal("manual input reader retained ownership after Wait returned")
	}
}

func TestWatchInputAdapterReleasesInputAfterScheduledWait(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	released := false
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			return func() error {
				released = true
				return nil
			}, nil
		},
	}

	result, err := adapter.Wait(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManualRequests != 0 {
		t.Fatalf("manual requests = %d", result.ManualRequests)
	}
	if !released {
		t.Fatal("input was not released after scheduled wait")
	}
}

func TestWatchInputAdapterTreatsControlCAsInterrupt(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{})
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			close(activated)
			return func() error { return nil }, nil
		},
	}
	outcome := make(chan bool, 1)
	go func() {
		result, _ := adapter.Wait(context.Background(), time.Hour)
		outcome <- result.Interrupted
	}()

	<-activated
	if _, err := writer.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	if !<-outcome {
		t.Fatal("control-C did not interrupt the wait")
	}
}

func TestWatchInputAdapterRestoresAroundSuspend(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{}, 2)
	released := make(chan struct{}, 2)
	suspended := make(chan struct{}, 1)
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			activated <- struct{}{}
			return func() error {
				released <- struct{}{}
				return nil
			}, nil
		},
		suspend: func() error {
			select {
			case <-released:
			default:
				t.Fatal("terminal input was not restored before suspension")
			}
			suspended <- struct{}{}
			return nil
		},
	}
	outcome := make(chan bool, 1)
	go func() {
		result, _ := adapter.Wait(context.Background(), time.Hour)
		outcome <- result.Interrupted
	}()

	<-activated
	if _, err := writer.Write([]byte{26}); err != nil {
		t.Fatal(err)
	}
	<-suspended
	<-activated
	if _, err := writer.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	if !<-outcome {
		t.Fatal("control-C did not interrupt the resumed wait")
	}
	<-released
}

func TestWatchInputAdapterSuspendsDuringCoalescing(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{}, 2)
	released := make(chan struct{}, 2)
	suspended := make(chan struct{}, 1)
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			activated <- struct{}{}
			return func() error {
				released <- struct{}{}
				return nil
			}, nil
		},
		suspend: func() error {
			<-released
			suspended <- struct{}{}
			return nil
		},
	}
	type waitOutcome struct {
		result watch.WaitResult
		err    error
	}
	outcome := make(chan waitOutcome, 1)
	go func() {
		result, err := adapter.Wait(context.Background(), time.Hour)
		outcome <- waitOutcome{result: result, err: err}
	}()

	<-activated
	if _, err := writer.Write([]byte{'r', 26}); err != nil {
		t.Fatal(err)
	}
	<-suspended
	<-activated
	got := <-outcome
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.ManualRequests != 1 {
		t.Fatalf("manual requests = %d, want 1", got.result.ManualRequests)
	}
	stateReady := make(chan struct{})
	close(stateReady)
	if _, err := got.result.Finalize(context.Background(), stateReady); err != nil {
		t.Fatal(err)
	}
	<-released
}

func TestWatchInputAdapterFallsBackWhenResumedInBackground(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{})
	activation := 0
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			activation++
			if activation > 1 {
				return nil, errWatchInputNotForeground
			}
			close(activated)
			return func() error { return nil }, nil
		},
		suspend: func() error { return nil },
	}
	outcome := make(chan error, 1)
	go func() {
		_, err := adapter.Wait(context.Background(), 10*time.Millisecond)
		outcome <- err
	}()

	<-activated
	if _, err := writer.Write([]byte{26}); err != nil {
		t.Fatal(err)
	}
	if err := <-outcome; err != nil {
		t.Fatal(err)
	}
}

func TestWatchInputAdapterFallsBackWhenBackgrounded(t *testing.T) {
	adapter := &watchInputAdapter{
		activate: func() (func() error, error) {
			return nil, errWatchInputNotForeground
		},
	}
	start := time.Now()
	result, err := adapter.Wait(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManualRequests != 0 || result.Interrupted {
		t.Fatalf("result = %#v", result)
	}
	if time.Since(start) < time.Millisecond {
		t.Fatal("background fallback did not wait")
	}
}

func TestWatchInputAdapterDoesNotWaitWhenCancellationFails(t *testing.T) {
	reader := &failedCancellationReader{closed: make(chan struct{})}
	adapter := &watchInputAdapter{
		input: io.Reader(reader),
		activate: func() (func() error, error) {
			return func() error { return nil }, nil
		},
		newReader: func(io.Reader) (cancelreader.CancelReader, error) {
			return reader, nil
		},
	}

	_, err := adapter.Wait(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("uncancelable reader was not closed")
	}
}

func TestWatchInputAdapterReportsTerminalRestoreFailure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			return func() error { return errors.New("restore failed") }, nil
		},
	}

	_, err = adapter.Wait(context.Background(), time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "restore terminal input: restore failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestWatchInputAdapterReportsHandoffRestoreFailure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{})
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			close(activated)
			return func() error { return errors.New("restore failed") }, nil
		},
	}
	outcome := make(chan watch.WaitResult, 1)
	go func() {
		result, _ := adapter.Wait(context.Background(), time.Hour)
		outcome <- result
	}()

	<-activated
	if _, err := writer.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	result := <-outcome
	if result.Finalize == nil {
		t.Fatal("manual input handoff is missing")
	}
	stateReady := make(chan struct{})
	close(stateReady)
	if _, err := result.Finalize(context.Background(), stateReady); err == nil || !strings.Contains(err.Error(), "restore terminal input: restore failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestWatchInputAdapterRestoresWhenReaderSetupFails(t *testing.T) {
	released := false
	adapter := &watchInputAdapter{
		input: strings.NewReader(""),
		activate: func() (func() error, error) {
			return func() error {
				released = true
				return nil
			}, nil
		},
		newReader: func(io.Reader) (cancelreader.CancelReader, error) {
			return nil, errors.New("reader failed")
		},
	}

	_, err := adapter.Wait(context.Background(), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "prepare manual input: reader failed") {
		t.Fatalf("error = %v", err)
	}
	if !released {
		t.Fatal("terminal input was not restored after reader setup failed")
	}
}

func TestWatchInputAdapterReleasesInterruptedCoalescing(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{})
	released := make(chan struct{})
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			close(activated)
			return func() error {
				close(released)
				return nil
			}, nil
		},
	}
	outcome := make(chan waitOutcome, 1)
	go func() {
		result, err := adapter.Wait(context.Background(), time.Hour)
		outcome <- waitOutcome{result: result, err: err}
	}()

	<-activated
	if _, err := writer.Write([]byte{'r', 3}); err != nil {
		t.Fatal(err)
	}
	got := <-outcome
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !got.result.Interrupted {
		t.Fatal("control-C during coalescing did not interrupt the wait")
	}
	if got.result.Finalize != nil {
		t.Fatal("interrupted coalescing retained a finalizer")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("terminal input was not restored after interrupted coalescing")
	}
}

func TestWatchInputAdapterHandlesControlsDuringStateFetch(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
	})
	activated := make(chan struct{}, 2)
	released := make(chan struct{}, 2)
	suspended := make(chan struct{}, 1)
	adapter := &watchInputAdapter{
		input: reader,
		activate: func() (func() error, error) {
			activated <- struct{}{}
			return func() error {
				released <- struct{}{}
				return nil
			}, nil
		},
		suspend: func() error {
			<-released
			suspended <- struct{}{}
			return nil
		},
	}
	outcome := make(chan waitOutcome, 1)
	go func() {
		result, err := adapter.Wait(context.Background(), time.Hour)
		outcome <- waitOutcome{result: result, err: err}
	}()

	<-activated
	if _, err := writer.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	waited := <-outcome
	if waited.err != nil {
		t.Fatal(waited.err)
	}
	stateReady := make(chan struct{})
	finalized := make(chan waitOutcome, 1)
	go func() {
		result, err := waited.result.Finalize(context.Background(), stateReady)
		finalized <- waitOutcome{result: result, err: err}
	}()
	if _, err := writer.Write([]byte{26}); err != nil {
		t.Fatal(err)
	}
	<-suspended
	<-activated
	if _, err := writer.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	got := <-finalized
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !got.result.Interrupted {
		t.Fatal("control-C during state fetch did not interrupt the handoff")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("terminal input was not restored after state-fetch interruption")
	}
}

type failedCancellationReader struct {
	closed chan struct{}
}

func (r *failedCancellationReader) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}

func (r *failedCancellationReader) Cancel() bool {
	return false
}

func (r *failedCancellationReader) Close() error {
	close(r.closed)
	return nil
}
