package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

const maxStderrSize = 1 << 20

type stderrBuffer interface {
	io.Writer
	String() string
}

type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {

		return len(p), nil
	}
	if len(p) > remaining {

		c.buf.Write(p[:remaining])
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) Bytes() []byte {
	return c.buf.Bytes()
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}

type executeOptions struct {
	Command string

	Args []string

	Stdin io.Reader

	WorkDir string

	TempFilePath string
}

func executeCommand(ctx context.Context, opts executeOptions) (*ExecutionResult, error) {

	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}

	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return terminateProcessGroup(cmd) }

	stderr := newCappedBuffer(maxStderrSize)
	cmd.Stderr = stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		CleanupTempFile(opts.TempFilePath)
		return nil, fmt.Errorf("failed to start %s: %w", opts.Command, err)
	}

	reader := &cmdReader{
		Reader:       stdout,
		cmd:          cmd,
		ctx:          ctx,
		stderr:       stderr,
		tempFilePath: opts.TempFilePath,
	}

	return reader.ToExecutionResult(), nil
}
