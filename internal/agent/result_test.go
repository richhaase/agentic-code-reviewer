package agent

import (
	"bytes"
	"io"
	"testing"
)

// mockReadCloser is a simple ReadCloser for testing.
type mockReadCloser struct {
	*bytes.Reader
	closed bool
}

func newMockReadCloser(data []byte) *mockReadCloser {
	return &mockReadCloser{
		Reader: bytes.NewReader(data),
	}
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return nil
}

func TestExecutionResult_Read(t *testing.T) {
	data := []byte("test output")
	mock := newMockReadCloser(data)

	result := NewExecutionResult(mock, nil, nil)

	buf := make([]byte, len(data))
	n, err := result.Read(buf)

	if err != nil {
		t.Errorf("Read() error = %v, want nil", err)
	}
	if n != len(data) {
		t.Errorf("Read() n = %d, want %d", n, len(data))
	}
	if string(buf) != string(data) {
		t.Errorf("Read() data = %q, want %q", buf, data)
	}
}

func TestExecutionResult_Close(t *testing.T) {
	mock := newMockReadCloser([]byte("test"))
	exitCode := 42
	stderr := "error output"

	result := NewExecutionResult(
		mock,
		func() int { return exitCode },
		func() string { return stderr },
	)

	// Before close
	if result.IsClosed() {
		t.Error("IsClosed() = true before Close(), want false")
	}
	if result.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d before Close(), want 0", result.ExitCode())
	}
	if result.Stderr() != "" {
		t.Errorf("Stderr() = %q before Close(), want empty", result.Stderr())
	}

	// Close
	err := result.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	// After close
	if !result.IsClosed() {
		t.Error("IsClosed() = false after Close(), want true")
	}
	if !mock.closed {
		t.Error("underlying reader not closed")
	}
	if result.ExitCode() != exitCode {
		t.Errorf("ExitCode() = %d after Close(), want %d", result.ExitCode(), exitCode)
	}
	if result.Stderr() != stderr {
		t.Errorf("Stderr() = %q after Close(), want %q", result.Stderr(), stderr)
	}
}

func TestExecutionResult_CloseOnce(t *testing.T) {
	mock := newMockReadCloser([]byte("test"))
	callCount := 0

	result := NewExecutionResult(
		mock,
		func() int {
			callCount++
			return 0
		},
		nil,
	)

	// Close multiple times
	_ = result.Close()
	_ = result.Close()
	_ = result.Close()

	// exitCodeFunc should only be called once
	if callCount != 1 {
		t.Errorf("exitCodeFunc called %d times, want 1", callCount)
	}
}

func TestExecutionResult_NilFuncs(t *testing.T) {
	mock := newMockReadCloser([]byte("test"))

	// Create with nil funcs
	result := NewExecutionResult(mock, nil, nil)

	err := result.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	// Should return default values
	if result.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d with nil func, want 0", result.ExitCode())
	}
	if result.Stderr() != "" {
		t.Errorf("Stderr() = %q with nil func, want empty", result.Stderr())
	}
}

func TestExecutionResult_ImplementsReadCloser(t *testing.T) {
	// Compile-time check that ExecutionResult implements io.ReadCloser
	var _ io.ReadCloser = (*ExecutionResult)(nil)
}

func TestExecutionResult_ReadAll(t *testing.T) {
	data := []byte("multi-line\ntest\noutput")
	mock := newMockReadCloser(data)

	result := NewExecutionResult(mock, nil, nil)

	got, err := io.ReadAll(result)
	if err != nil {
		t.Errorf("io.ReadAll() error = %v, want nil", err)
	}
	if string(got) != string(data) {
		t.Errorf("io.ReadAll() = %q, want %q", got, data)
	}
}
