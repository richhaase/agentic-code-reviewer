package terminal

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr captures stderr output during the execution of f.
func captureStderr(f func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	f()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestLogger_Log_AllStyles(t *testing.T) {
	// Disable colors for predictable output
	DisableColors()
	defer EnableColors()

	tests := []struct {
		style          Style
		expectedSymbol string
	}{
		{StyleInfo, "I"},
		{StyleSuccess, "✓"},
		{StyleWarning, "W"},
		{StyleError, "!"},
		{StyleDim, "·"},
		{StylePhase, "▸"},
	}

	for _, tc := range tests {
		t.Run(string(tc.style), func(t *testing.T) {
			logger := &Logger{isTTY: false} // Non-TTY to avoid line clearing

			output := captureStderr(func() {
				logger.Log("test message", tc.style)
			})

			if !strings.Contains(output, tc.expectedSymbol) {
				t.Errorf("expected symbol %q in output, got %q", tc.expectedSymbol, output)
			}
			if !strings.Contains(output, "test message") {
				t.Errorf("expected message in output, got %q", output)
			}
			if !strings.HasSuffix(output, "\n") {
				t.Error("expected newline at end of output")
			}
		})
	}
}

func TestLogger_Logf(t *testing.T) {
	DisableColors()
	defer EnableColors()

	logger := &Logger{isTTY: false}

	output := captureStderr(func() {
		logger.Logf(StyleInfo, "formatted %s %d", "test", 42)
	})

	if !strings.Contains(output, "formatted test 42") {
		t.Errorf("expected formatted message, got %q", output)
	}
}

func TestLog_PackageLevel(t *testing.T) {
	DisableColors()
	defer EnableColors()

	output := captureStderr(func() {
		Log("package level message", StyleWarning)
	})

	if !strings.Contains(output, "package level message") {
		t.Errorf("expected message in output, got %q", output)
	}
	if !strings.Contains(output, "W") {
		t.Errorf("expected warning symbol in output, got %q", output)
	}
}

func TestLogf_PackageLevel(t *testing.T) {
	DisableColors()
	defer EnableColors()

	output := captureStderr(func() {
		Logf(StyleError, "error: %v", "something went wrong")
	})

	if !strings.Contains(output, "error: something went wrong") {
		t.Errorf("expected formatted message, got %q", output)
	}
	if !strings.Contains(output, "!") {
		t.Errorf("expected error symbol in output, got %q", output)
	}
}

func TestLogger_Log_WithColors(t *testing.T) {
	EnableColors()

	logger := &Logger{isTTY: false}

	output := captureStderr(func() {
		logger.Log("colored message", StyleSuccess)
	})

	// Should contain ANSI escape codes
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI codes in colored output, got %q", output)
	}
	if !strings.Contains(output, "colored message") {
		t.Errorf("expected message in output, got %q", output)
	}
}

func TestLogger_Log_TTYClearsLine(t *testing.T) {
	DisableColors()
	defer EnableColors()

	// With TTY mode, should clear line first
	logger := &Logger{isTTY: true}

	output := captureStderr(func() {
		logger.Log("tty message", StyleInfo)
	})

	// Should contain carriage return for line clearing
	if !strings.Contains(output, "\r") {
		t.Errorf("expected carriage return in TTY output, got %q", output)
	}
}

func TestStyle_Constants(t *testing.T) {
	// Verify style constants have expected values
	if StyleInfo != "info" {
		t.Errorf("StyleInfo = %q, want %q", StyleInfo, "info")
	}
	if StyleSuccess != "success" {
		t.Errorf("StyleSuccess = %q, want %q", StyleSuccess, "success")
	}
	if StyleWarning != "warning" {
		t.Errorf("StyleWarning = %q, want %q", StyleWarning, "warning")
	}
	if StyleError != "error" {
		t.Errorf("StyleError = %q, want %q", StyleError, "error")
	}
	if StyleDim != "dim" {
		t.Errorf("StyleDim = %q, want %q", StyleDim, "dim")
	}
	if StylePhase != "phase" {
		t.Errorf("StylePhase = %q, want %q", StylePhase, "phase")
	}
}

func TestLogger_EmptyMessage(t *testing.T) {
	DisableColors()
	defer EnableColors()

	logger := &Logger{isTTY: false}

	output := captureStderr(func() {
		logger.Log("", StyleInfo)
	})

	// Should still output the tag with empty message
	if !strings.Contains(output, "[") {
		t.Errorf("expected tag brackets in output even for empty message, got %q", output)
	}
}
