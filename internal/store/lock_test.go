package store

import (
	"errors"
	"testing"
)

func TestAcquireWriteLock_SecondAcquireRefusesCleanly(t *testing.T) {
	dir := t.TempDir()

	first, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	_, err = AcquireWriteLock(dir)
	if err == nil {
		t.Fatal("expected second acquire to fail while the first holds the lock")
	}
	if !errors.Is(err, ErrWriterLocked) {
		t.Fatalf("expected ErrWriterLocked, got %v", err)
	}
}

func TestAcquireWriteLock_ReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()

	first, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("second acquire after release: %v", err)
	}
	defer second.Release()
}

func TestWriteLock_ReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}

func TestWriteLock_ReleaseOnNilIsNoop(t *testing.T) {
	var lock *WriteLock
	if err := lock.Release(); err != nil {
		t.Fatalf("expected nil release to be a no-op, got %v", err)
	}
}
