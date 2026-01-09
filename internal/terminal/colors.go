// Package terminal provides terminal output formatting and TTY detection.
package terminal

import (
	"os"

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

// colorsEnabled tracks whether color output is enabled globally.
var colorsEnabled = true

// DisableColors turns off color output globally.
func DisableColors() {
	colorsEnabled = false
}

// EnableColors turns on color output globally.
func EnableColors() {
	colorsEnabled = true
}

// Color returns the color code if colors are enabled, otherwise empty string.
// This provides a cleaner API: Color(Cyan) instead of colors.Cyan
func Color(c string) string {
	if colorsEnabled {
		return c
	}
	return ""
}

// Colors holds color codes that can be disabled for non-TTY output.
// Use this struct when you need to pass colors to functions or for testing.
type Colors struct {
	Reset   string
	Bold    string
	Dim     string
	Cyan    string
	Green   string
	Yellow  string
	Red     string
	Magenta string
	White   string
	Blue    string
}

// NewColors creates a Colors instance with colors enabled.
func NewColors() *Colors {
	return &Colors{
		Reset:   Reset,
		Bold:    Bold,
		Dim:     Dim,
		Cyan:    Cyan,
		Green:   Green,
		Yellow:  Yellow,
		Red:     Red,
		Magenta: Magenta,
		White:   White,
		Blue:    Blue,
	}
}

// NewColorsDisabled creates a Colors instance with colors disabled.
func NewColorsDisabled() *Colors {
	return &Colors{}
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
