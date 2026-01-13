package terminal

import (
	"testing"
)

func TestEnableDisableColors(t *testing.T) {
	// Ensure we start enabled
	EnableColors()

	if Color(Cyan) != Cyan {
		t.Error("expected color code when colors enabled")
	}

	DisableColors()

	if Color(Cyan) != "" {
		t.Error("expected empty string when colors disabled")
	}

	// Re-enable for other tests
	EnableColors()

	if Color(Cyan) != Cyan {
		t.Error("expected color code after re-enabling colors")
	}
}

func TestColor_AllColors(t *testing.T) {
	EnableColors()

	colors := []struct {
		name     string
		code     string
		expected string
	}{
		{"Reset", Reset, "\033[0m"},
		{"Bold", Bold, "\033[1m"},
		{"Dim", Dim, "\033[2m"},
		{"Cyan", Cyan, "\033[36m"},
		{"Green", Green, "\033[32m"},
		{"Yellow", Yellow, "\033[33m"},
		{"Red", Red, "\033[31m"},
		{"Magenta", Magenta, "\033[35m"},
		{"White", White, "\033[97m"},
		{"Blue", Blue, "\033[34m"},
	}

	for _, tc := range colors {
		t.Run(tc.name, func(t *testing.T) {
			if tc.code != tc.expected {
				t.Errorf("constant %s = %q, want %q", tc.name, tc.code, tc.expected)
			}
			if Color(tc.code) != tc.code {
				t.Errorf("Color(%s) = %q, want %q", tc.name, Color(tc.code), tc.code)
			}
		})
	}
}

func TestColor_DisabledReturnsEmpty(t *testing.T) {
	DisableColors()
	defer EnableColors()

	colors := []string{Reset, Bold, Dim, Cyan, Green, Yellow, Red, Magenta, White, Blue}
	for _, c := range colors {
		if Color(c) != "" {
			t.Errorf("Color(%q) should return empty when disabled, got %q", c, Color(c))
		}
	}
}

func TestNewColors(t *testing.T) {
	c := NewColors()
	if c == nil {
		t.Fatal("NewColors returned nil")
	}

	if c.Reset != Reset {
		t.Errorf("Reset = %q, want %q", c.Reset, Reset)
	}
	if c.Bold != Bold {
		t.Errorf("Bold = %q, want %q", c.Bold, Bold)
	}
	if c.Dim != Dim {
		t.Errorf("Dim = %q, want %q", c.Dim, Dim)
	}
	if c.Cyan != Cyan {
		t.Errorf("Cyan = %q, want %q", c.Cyan, Cyan)
	}
	if c.Green != Green {
		t.Errorf("Green = %q, want %q", c.Green, Green)
	}
	if c.Yellow != Yellow {
		t.Errorf("Yellow = %q, want %q", c.Yellow, Yellow)
	}
	if c.Red != Red {
		t.Errorf("Red = %q, want %q", c.Red, Red)
	}
	if c.Magenta != Magenta {
		t.Errorf("Magenta = %q, want %q", c.Magenta, Magenta)
	}
	if c.White != White {
		t.Errorf("White = %q, want %q", c.White, White)
	}
	if c.Blue != Blue {
		t.Errorf("Blue = %q, want %q", c.Blue, Blue)
	}
}

func TestNewColorsDisabled(t *testing.T) {
	c := NewColorsDisabled()
	if c == nil {
		t.Fatal("NewColorsDisabled returned nil")
	}

	if c.Reset != "" {
		t.Errorf("Reset should be empty, got %q", c.Reset)
	}
	if c.Bold != "" {
		t.Errorf("Bold should be empty, got %q", c.Bold)
	}
	if c.Dim != "" {
		t.Errorf("Dim should be empty, got %q", c.Dim)
	}
	if c.Cyan != "" {
		t.Errorf("Cyan should be empty, got %q", c.Cyan)
	}
	if c.Green != "" {
		t.Errorf("Green should be empty, got %q", c.Green)
	}
	if c.Yellow != "" {
		t.Errorf("Yellow should be empty, got %q", c.Yellow)
	}
	if c.Red != "" {
		t.Errorf("Red should be empty, got %q", c.Red)
	}
	if c.Magenta != "" {
		t.Errorf("Magenta should be empty, got %q", c.Magenta)
	}
	if c.White != "" {
		t.Errorf("White should be empty, got %q", c.White)
	}
	if c.Blue != "" {
		t.Errorf("Blue should be empty, got %q", c.Blue)
	}
}

func TestIsTTY(t *testing.T) {
	// We can't really test if something IS a TTY in a test environment
	// but we can verify the function doesn't panic and returns a bool
	_ = IsTTY(0)
	_ = IsTTY(1)
	_ = IsTTY(2)
}

func TestIsStdoutTTY(t *testing.T) {
	// In test environment, stdout is typically not a TTY
	// Just verify it doesn't panic
	result := IsStdoutTTY()
	// In CI/test environments, this is typically false
	_ = result
}

func TestIsStderrTTY(t *testing.T) {
	// In test environment, stderr is typically not a TTY
	// Just verify it doesn't panic
	result := IsStderrTTY()
	_ = result
}

func TestGetTerminalWidth(t *testing.T) {
	width := GetTerminalWidth()
	// Should return either actual width or default 80
	if width <= 0 {
		t.Errorf("GetTerminalWidth() = %d, want > 0", width)
	}
	// In non-TTY environment, should return 80
	// but we can't guarantee the test environment
	if width < 10 || width > 10000 {
		t.Errorf("GetTerminalWidth() = %d, seems unreasonable", width)
	}
}
