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

const lineClearWidth = 100

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
	symbol := "I"
	switch style {
	case StyleInfo:
		styleColor = Cyan
		symbol = "I"
	case StyleSuccess:
		styleColor = Green
		symbol = "✓"
	case StyleWarning:
		styleColor = Yellow
		symbol = "W"
	case StyleError:
		styleColor = Red
		symbol = "!"
	case StyleDim:
		styleColor = Dim
		symbol = "·"
	case StylePhase:
		styleColor = Magenta + Bold
		symbol = "▸"
	}

	// Clear line if TTY
	if l.isTTY {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", lineClearWidth)+"\r")
	}

	tag := fmt.Sprintf("%s[%s%s%s%s%s]%s",
		Color(Dim), Color(Reset), Color(styleColor), symbol, Color(Reset), Color(Dim), Color(Reset))
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
