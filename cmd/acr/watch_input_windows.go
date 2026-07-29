package main

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

func watchInputSupported() bool {
	return true
}

func activateWatchInput(input *os.File) (func() error, error) {
	fd := int(input.Fd())
	if err := windows.FlushConsoleInputBuffer(windows.Handle(input.Fd())); err != nil {
		return nil, err
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error {
		if err := term.Restore(fd, state); err != nil {
			return err
		}
		return windows.FlushConsoleInputBuffer(windows.Handle(input.Fd()))
	}, nil
}

func suspendWatchInput() error {
	return nil
}
