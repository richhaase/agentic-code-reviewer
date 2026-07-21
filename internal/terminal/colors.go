package terminal

import (
	"os"
	"sync"

	"golang.org/x/term"
)

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

var colorMu sync.RWMutex

var colorsEnabled = true

func DisableColors() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = false
}

func EnableColors() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = true
}

func ColorsEnabled() bool {
	colorMu.RLock()
	defer colorMu.RUnlock()
	return colorsEnabled
}

func SetColorsEnabled(enabled bool) {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsEnabled = enabled
}

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

func Color(c string) string {
	colorMu.RLock()
	defer colorMu.RUnlock()
	if colorsEnabled {
		return c
	}
	return ""
}

func IsTTY(fd int) bool {
	return term.IsTerminal(fd)
}

func IsStdoutTTY() bool {
	return IsTTY(int(os.Stdout.Fd()))
}

func IsStderrTTY() bool {
	return IsTTY(int(os.Stderr.Fd()))
}

func IsStdinTTY() bool {
	return IsTTY(int(os.Stdin.Fd()))
}

func GetTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}
