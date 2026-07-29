package main

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func watchInputSupported() bool {
	return true
}

func activateWatchInput(input *os.File) (func() error, error) {
	fd := int(input.Fd())
	foregroundGroup, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return nil, fmt.Errorf("read foreground process group: %w", err)
	}
	if foregroundGroup != unix.Getpgrp() {
		return nil, errWatchInputNotForeground
	}
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETAF, state); err != nil {
		return nil, fmt.Errorf("discard pending terminal input: %w", err)
	}
	rawState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error {
		return term.Restore(fd, rawState)
	}, nil
}

func suspendWatchInput() error {
	return syscall.Kill(-unix.Getpgrp(), syscall.SIGTSTP)
}
