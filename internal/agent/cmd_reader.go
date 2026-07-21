package agent

import (
	"context"
	"io"
	"os/exec"
	"sync"
)

var _ io.Closer = (*cmdReader)(nil)

type cmdReader struct {
	io.Reader
	cmd          *exec.Cmd
	ctx          context.Context
	stderr       stderrBuffer
	exitCode     int
	closeOnce    sync.Once
	tempFilePath string
}

func (r *cmdReader) Close() error {
	r.closeOnce.Do(func() {

		if closer, ok := r.Reader.(io.Closer); ok {
			_ = closer.Close()
		}

		if r.cmd != nil && r.cmd.Process != nil {
			if r.ctx != nil && r.ctx.Err() != nil {
				_ = terminateProcessGroup(r.cmd)
			}

			err := r.cmd.Wait()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					r.exitCode = exitErr.ExitCode()
				} else {
					r.exitCode = -1
				}
			}
		}

		CleanupTempFile(r.tempFilePath)
	})

	return nil
}

func (r *cmdReader) ExitCode() int {
	return r.exitCode
}

func (r *cmdReader) Stderr() string {
	if r.stderr == nil {
		return ""
	}
	return r.stderr.String()
}

func (r *cmdReader) ToExecutionResult() *ExecutionResult {
	return NewExecutionResult(
		r,
		func() int { return r.ExitCode() },
		func() string { return r.Stderr() },
	)
}
