//go:build !darwin && !linux && !windows

package main

import "os"

func watchInputSupported() bool {
	return false
}

func activateWatchInput(_ *os.File) (func() error, error) {
	return nil, errWatchInputNotForeground
}

func suspendWatchInput() error {
	return nil
}
