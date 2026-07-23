package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const LockFileName = "desk.lock"

var ErrWriterLocked = errors.New("another acr process already owns the desk write lock")

type WriteLock struct {
	file *os.File
}

func AcquireWriteLock(dataDir string) (*WriteLock, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, LockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open desk lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("acquire desk lock %s: %w", path, ErrWriterLocked)
		}
		return nil, fmt.Errorf("acquire desk lock %s: %w", path, err)
	}
	return &WriteLock{file: file}, nil
}

func (l *WriteLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("release desk lock: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close desk lock file: %w", closeErr)
	}
	return nil
}
