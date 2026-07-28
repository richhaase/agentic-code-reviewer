package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

const manualRequestCoalesceWindow = 30 * time.Millisecond

type watchInputAdapter struct {
	input    io.Reader
	activate func() (func() error, error)
}

type watchInputRead struct {
	key byte
	err error
}

func newWatchInputAdapter(input *os.File) *watchInputAdapter {
	return &watchInputAdapter{
		input: input,
		activate: func() (func() error, error) {
			fd := int(input.Fd())
			state, err := term.MakeRaw(fd)
			if err != nil {
				return nil, err
			}
			return func() error {
				return term.Restore(fd, state)
			}, nil
		},
	}
}

func (a *watchInputAdapter) Wait(ctx context.Context, duration time.Duration) (result watch.WaitResult, resultErr error) {
	restore, err := a.activate()
	if err != nil {
		return watch.WaitResult{}, fmt.Errorf("enable manual input: %w", err)
	}
	defer func() {
		if err := restore(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("restore terminal input: %w", err)
		}
	}()

	reader, err := cancelreader.NewReader(a.input)
	if err != nil {
		return watch.WaitResult{}, fmt.Errorf("prepare manual input: %w", err)
	}
	defer reader.Close()

	reads := make(chan watchInputRead, 1)
	done := make(chan struct{})
	stop := make(chan struct{})
	go readWatchInput(reader, reads, stop, done)
	defer func() {
		close(stop)
		reader.Cancel()
		<-done
	}()

	timer := time.NewTimer(duration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return watch.WaitResult{}, ctx.Err()
		case <-timer.C:
			return watch.WaitResult{}, nil
		case read := <-reads:
			if read.err != nil {
				return watch.WaitResult{}, fmt.Errorf("read manual input: %w", read.err)
			}
			switch read.key {
			case 3:
				return watch.WaitResult{Interrupted: true}, nil
			case 'r', 'R':
				return a.coalesce(ctx, reads)
			}
		}
	}
}

func (a *watchInputAdapter) coalesce(ctx context.Context, reads <-chan watchInputRead) (watch.WaitResult, error) {
	count := 1
	timer := time.NewTimer(manualRequestCoalesceWindow)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return watch.WaitResult{}, ctx.Err()
		case <-timer.C:
			return watch.WaitResult{ManualRequests: count}, nil
		case read := <-reads:
			if read.err != nil {
				return watch.WaitResult{}, fmt.Errorf("read manual input: %w", read.err)
			}
			switch read.key {
			case 3:
				return watch.WaitResult{Interrupted: true}, nil
			case 'r', 'R':
				count++
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(manualRequestCoalesceWindow)
			}
		}
	}
}

func readWatchInput(reader io.Reader, reads chan<- watchInputRead, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	var buffer [1]byte
	for {
		n, err := reader.Read(buffer[:])
		if err != nil {
			if err != cancelreader.ErrCanceled {
				select {
				case reads <- watchInputRead{err: err}:
				case <-stop:
				}
			}
			return
		}
		if n == 1 {
			for _, key := range buffer {
				select {
				case reads <- watchInputRead{key: key}:
				case <-stop:
					return
				}
			}
		}
	}
}
