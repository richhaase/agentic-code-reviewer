// Package terminal provides terminal output formatting and TTY detection.
package terminal

import (
	"os"
	"sync"

	"golang.org/x/term"
)

// ANSI color codes.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Cyan    = "\033[36m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Red     = "\033[31m"
	Magenta = "\033[35m"
	White   = "\033[97m"
	Blue    = "\033[34m"
)

// colorMu protects access to colorsEnabled for thread safety.
var colorMu sync.RWMutex

// colorsEnabled tracks whether color output is enabled globally.
// Access is protected by colorMu for thread safety.
var colorsEnabled = true

// DisableColors turns off color output globally.
// This function is thread-safe.
func DisableColors() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = false
}

// EnableColors turns on color output globally.
// This function is thread-safe.
func EnableColors() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = true
}

// ColorsEnabled returns whether colors are currently enabled.
// This function is thread-safe.
func ColorsEnabled() bool {
	colorMu.RLock()
	defer colorMu.RUnlock()
	return colorsEnabled
}

// SetColorsEnabled sets the color output state.
// This function is thread-safe.
func SetColorsEnabled(enabled bool) {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = enabled
}

// WithColorsDisabled runs a function with colors disabled, then restores the previous state.
// This is useful for tests that need to disable colors without affecting other tests.
// This function is thread-safe.
func WithColorsDisabled(fn func()) {
	colorMu.Lock()
	prev := colorsEnabled
	colorsEnabled = false
	colorMu.Unlock()

	defer func() {
		colorMu.Lock()
		colorsEnabled = prev
		colorMu.Unlock()
	}()

	fn()
}

// Color returns the color code if colors are enabled, otherwise empty string.
// This provides a cleaner API: Color(Cyan) instead of colors.Cyan
// This function is thread-safe.
func Color(c string) string {
	colorMu.RLock()
	defer colorMu.RUnlock()
	if colorsEnabled {
		return c
	}
	return ""
}

// IsTTY returns true if the given file descriptor is a TTY.
func IsTTY(fd int) bool {
	return term.IsTerminal(fd)
}

// IsStdoutTTY returns true if stdout is a TTY.
func IsStdoutTTY() bool {
	return IsTTY(int(os.Stdout.Fd()))
}

// IsStderrTTY returns true if stderr is a TTY.
func IsStderrTTY() bool {
	return IsTTY(int(os.Stderr.Fd()))
}

// GetTerminalWidth returns the terminal width, or 80 if detection fails.
func GetTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}
