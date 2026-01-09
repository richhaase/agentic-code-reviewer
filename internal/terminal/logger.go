package terminal

import (
	"fmt"
	"os"
	"strings"
)

// Style represents a log message style.
type Style string

const (
	StyleInfo    Style = "info"
	StyleSuccess Style = "success"
	StyleWarning Style = "warning"
	StyleError   Style = "error"
	StyleDim     Style = "dim"
	StylePhase   Style = "phase"
)

// Logger provides styled logging to stderr.
type Logger struct {
	isTTY bool
}

// NewLogger creates a new logger.
func NewLogger() *Logger {
	return &Logger{
		isTTY: IsStderrTTY(),
	}
}

// Log prints a styled log message to stderr.
func (l *Logger) Log(msg string, style Style) {
	styleColor := Cyan
	switch style {
	case StyleInfo:
		styleColor = Cyan
	case StyleSuccess:
		styleColor = Green
	case StyleWarning:
		styleColor = Yellow
	case StyleError:
		styleColor = Red
	case StyleDim:
		styleColor = Dim
	case StylePhase:
		styleColor = Magenta + Bold
	}

	// Clear line if TTY
	if l.isTTY {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 100)+"\r")
	}

	tag := fmt.Sprintf("%s[%s%sreview%s%s]%s",
		Color(Dim), Color(Reset), Color(styleColor), Color(Reset), Color(Dim), Color(Reset))
	fmt.Fprintf(os.Stderr, "%s %s\n", tag, msg)
}

// Logf prints a formatted styled log message to stderr.
func (l *Logger) Logf(style Style, format string, args ...any) {
	l.Log(fmt.Sprintf(format, args...), style)
}

// Log prints a styled log message to stderr (package-level function).
func Log(msg string, style Style) {
	logger := NewLogger()
	logger.Log(msg, style)
}

// Logf prints a formatted styled log message to stderr (package-level function).
func Logf(style Style, format string, args ...any) {
	Log(fmt.Sprintf(format, args...), style)
}
