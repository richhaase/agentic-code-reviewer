package agent

import (
	"io"
	"sync"
	"sync/atomic"
)

type ExecutionResult struct {
	reader       io.ReadCloser
	exitCode     int
	exitCodeFunc func() int
	stderr       string
	stderrFunc   func() string
	closeOnce    sync.Once
	closed       atomic.Bool
}

func NewExecutionResult(reader io.ReadCloser, exitCodeFunc func() int, stderrFunc func() string) *ExecutionResult {
	return &ExecutionResult{
		reader:       reader,
		exitCodeFunc: exitCodeFunc,
		stderrFunc:   stderrFunc,
	}
}

func (r *ExecutionResult) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

func (r *ExecutionResult) Close() error {
	var closeErr error
	r.closeOnce.Do(func() {
		closeErr = r.reader.Close()
		if r.exitCodeFunc != nil {
			r.exitCode = r.exitCodeFunc()
		}
		if r.stderrFunc != nil {
			r.stderr = r.stderrFunc()
		}
		r.closed.Store(true)
	})
	return closeErr
}

func (r *ExecutionResult) ExitCode() int {
	return r.exitCode
}

func (r *ExecutionResult) Stderr() string {
	return r.stderr
}

func (r *ExecutionResult) IsClosed() bool {
	return r.closed.Load()
}
