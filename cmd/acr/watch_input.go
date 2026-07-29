package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/muesli/cancelreader"

	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

const manualRequestCoalesceWindow = 30 * time.Millisecond

var errWatchInputNotForeground = errors.New("watcher does not own the foreground terminal")

type watchInputAdapter struct {
	input     io.Reader
	activate  func() (func() error, error)
	suspend   func() error
	newReader func(io.Reader) (cancelreader.CancelReader, error)
}

type watchInputRead struct {
	key byte
	err error
}

type watchInputSession struct {
	reader cancelreader.CancelReader
	reads  <-chan watchInputRead
	stop   chan<- struct{}
	done   <-chan struct{}
}

func newWatchInputAdapter(input *os.File) *watchInputAdapter {
	return &watchInputAdapter{
		input: input,
		activate: func() (func() error, error) {
			return activateWatchInput(input)
		},
		suspend: suspendWatchInput,
	}
}

func (a *watchInputAdapter) Wait(ctx context.Context, duration time.Duration) (result watch.WaitResult, resultErr error) {
	restore, err := a.activate()
	if err != nil {
		if errors.Is(err, errWatchInputNotForeground) {
			return waitWithoutWatchInput(ctx, duration)
		}
		return watch.WaitResult{}, fmt.Errorf("enable manual input: %w", err)
	}

	var session *watchInputSession
	handedOff := false
	defer func() {
		if handedOff {
			return
		}
		if err := releaseWatchInput(session, restore); err != nil {
			if resultErr != nil {
				resultErr = fmt.Errorf("%v; restore terminal input: %w", resultErr, err)
				return
			}
			resultErr = fmt.Errorf("restore terminal input: %w", err)
		}
	}()
	session, err = a.startSession()
	if err != nil {
		return watch.WaitResult{}, fmt.Errorf("prepare manual input: %w", err)
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return watch.WaitResult{}, ctx.Err()
		case <-timer.C:
			return watch.WaitResult{}, nil
		case read := <-sessionReads(session):
			if read.err != nil {
				return watch.WaitResult{}, fmt.Errorf("read manual input: %w", read.err)
			}
			switch read.key {
			case 3:
				return watch.WaitResult{Interrupted: true}, nil
			case 26:
				if a.suspend == nil {
					continue
				}
				if err := a.suspendAndResume(&session, &restore); err != nil {
					return watch.WaitResult{}, err
				}
			case 'r', 'R':
				result, err := a.coalesce(ctx, &session, &restore, 1)
				if err != nil {
					return watch.WaitResult{}, err
				}
				if result.Interrupted {
					return result, nil
				}
				result.Finalize = a.handoff(&session, &restore)
				handedOff = true
				return result, nil
			}
		}
	}
}

func (a *watchInputAdapter) startSession() (*watchInputSession, error) {
	newReader := a.newReader
	if newReader == nil {
		newReader = cancelreader.NewReader
	}
	reader, err := newReader(a.input)
	if err != nil {
		return nil, err
	}
	reads := make(chan watchInputRead, 1)
	done := make(chan struct{})
	stop := make(chan struct{})
	go readWatchInput(reader, reads, stop, done)
	return &watchInputSession{reader: reader, reads: reads, stop: stop, done: done}, nil
}

func (s *watchInputSession) close() {
	close(s.stop)
	if s.reader.Cancel() {
		<-s.done
	}
	s.reader.Close()
}

func releaseWatchInput(session *watchInputSession, restore func() error) error {
	if session != nil {
		session.close()
	}
	if restore == nil {
		return nil
	}
	return restore()
}

func sessionReads(session *watchInputSession) <-chan watchInputRead {
	if session == nil {
		return nil
	}
	return session.reads
}

func (a *watchInputAdapter) suspendAndResume(session **watchInputSession, restore *func() error) error {
	(*session).close()
	*session = nil
	if err := (*restore)(); err != nil {
		return fmt.Errorf("restore terminal input before suspend: %w", err)
	}
	*restore = func() error { return nil }
	if err := a.suspend(); err != nil {
		return fmt.Errorf("suspend watcher: %w", err)
	}
	nextRestore, err := a.activate()
	if errors.Is(err, errWatchInputNotForeground) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore manual input after resume: %w", err)
	}
	nextSession, err := a.startSession()
	if err != nil {
		if restoreErr := nextRestore(); restoreErr != nil {
			return fmt.Errorf(
				"restore terminal after input reader failure: %v; prepare input reader: %w",
				restoreErr,
				err,
			)
		}
		return fmt.Errorf("restore manual input reader after resume: %w", err)
	}
	*restore = nextRestore
	*session = nextSession
	return nil
}

func waitWithoutWatchInput(ctx context.Context, duration time.Duration) (watch.WaitResult, error) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return watch.WaitResult{}, ctx.Err()
	case <-timer.C:
		return watch.WaitResult{}, nil
	}
}

func (a *watchInputAdapter) coalesce(
	ctx context.Context,
	session **watchInputSession,
	restore *func() error,
	count int,
) (watch.WaitResult, error) {
	timer := time.NewTimer(manualRequestCoalesceWindow)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return watch.WaitResult{}, ctx.Err()
		case <-timer.C:
			return watch.WaitResult{ManualRequests: count}, nil
		case read := <-sessionReads(*session):
			if read.err != nil {
				return watch.WaitResult{}, fmt.Errorf("read manual input: %w", read.err)
			}
			switch read.key {
			case 3:
				return watch.WaitResult{Interrupted: true}, nil
			case 26:
				if a.suspend != nil {
					if err := a.suspendAndResume(session, restore); err != nil {
						return watch.WaitResult{}, err
					}
				}
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

func (a *watchInputAdapter) handoff(
	session **watchInputSession,
	restore *func() error,
) func(context.Context, <-chan struct{}) (watch.WaitResult, error) {
	finalized := false
	return func(ctx context.Context, stateReady <-chan struct{}) (watch.WaitResult, error) {
		if finalized {
			return watch.WaitResult{}, nil
		}
		finalized = true
		result, collectErr := a.coalesceThroughState(ctx, session, restore, stateReady)
		releaseErr := releaseWatchInput(*session, *restore)
		*session = nil
		*restore = nil
		if collectErr != nil {
			if releaseErr != nil {
				return result, fmt.Errorf("%v; restore terminal input: %w", collectErr, releaseErr)
			}
			return result, collectErr
		}
		if releaseErr != nil {
			return result, fmt.Errorf("restore terminal input: %w", releaseErr)
		}
		return result, nil
	}
}

func (a *watchInputAdapter) coalesceThroughState(
	ctx context.Context,
	session **watchInputSession,
	restore *func() error,
	stateReady <-chan struct{},
) (watch.WaitResult, error) {
	var timer *time.Timer
	var quiet <-chan time.Time
	if stateReady == nil {
		timer = time.NewTimer(manualRequestCoalesceWindow)
		quiet = timer.C
		stateReady = nil
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	count := 0

	for {
		select {
		case <-ctx.Done():
			return watch.WaitResult{}, ctx.Err()
		case <-stateReady:
			stateReady = nil
			timer = time.NewTimer(manualRequestCoalesceWindow)
			quiet = timer.C
		case <-quiet:
			return watch.WaitResult{ManualRequests: count}, nil
		case read := <-sessionReads(*session):
			if read.err != nil {
				return watch.WaitResult{}, fmt.Errorf("read manual input: %w", read.err)
			}
			switch read.key {
			case 3:
				return watch.WaitResult{Interrupted: true}, nil
			case 26:
				if a.suspend != nil {
					if err := a.suspendAndResume(session, restore); err != nil {
						return watch.WaitResult{}, err
					}
				}
			case 'r', 'R':
				count++
				if timer == nil {
					continue
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(manualRequestCoalesceWindow)
				quiet = timer.C
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
