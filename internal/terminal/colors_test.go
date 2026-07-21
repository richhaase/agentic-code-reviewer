package terminal

import (
	"sync"
	"testing"
)

func TestEnableDisableColors(t *testing.T) {

	EnableColors()

	if Color(Cyan) != Cyan {
		t.Error("expected color code when colors enabled")
	}

	DisableColors()

	if Color(Cyan) != "" {
		t.Error("expected empty string when colors disabled")
	}

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

func TestIsTTY(t *testing.T) {

	_ = IsTTY(0)
	_ = IsTTY(1)
	_ = IsTTY(2)
}

func TestIsStdoutTTY(t *testing.T) {

	result := IsStdoutTTY()

	_ = result
}

func TestIsStderrTTY(t *testing.T) {

	result := IsStderrTTY()
	_ = result
}

func TestGetTerminalWidth(t *testing.T) {
	width := GetTerminalWidth()

	if width <= 0 {
		t.Errorf("GetTerminalWidth() = %d, want > 0", width)
	}

	if width < 10 || width > 10000 {
		t.Errorf("GetTerminalWidth() = %d, seems unreasonable", width)
	}
}

func TestColorsEnabled(t *testing.T) {
	EnableColors()
	if !ColorsEnabled() {
		t.Error("ColorsEnabled() should return true after EnableColors()")
	}

	DisableColors()
	if ColorsEnabled() {
		t.Error("ColorsEnabled() should return false after DisableColors()")
	}

	EnableColors()
}

func TestSetColorsEnabled(t *testing.T) {
	SetColorsEnabled(true)
	if !ColorsEnabled() {
		t.Error("ColorsEnabled() should return true after SetColorsEnabled(true)")
	}

	SetColorsEnabled(false)
	if ColorsEnabled() {
		t.Error("ColorsEnabled() should return false after SetColorsEnabled(false)")
	}

	SetColorsEnabled(true)
}

func TestWithColorsDisabled(t *testing.T) {
	EnableColors()

	if !ColorsEnabled() {
		t.Fatal("colors should be enabled before WithColorsDisabled")
	}

	var insideState bool
	WithColorsDisabled(func() {
		insideState = ColorsEnabled()
	})

	if insideState {
		t.Error("colors should be disabled inside WithColorsDisabled")
	}

	if !ColorsEnabled() {
		t.Error("colors should be restored after WithColorsDisabled")
	}
}

func TestWithColorsDisabled_RestoresPreviousState(t *testing.T) {

	DisableColors()
	defer EnableColors()

	WithColorsDisabled(func() {

		if ColorsEnabled() {
			t.Error("colors should be disabled inside WithColorsDisabled")
		}
	})

	if ColorsEnabled() {
		t.Error("WithColorsDisabled should restore previous disabled state")
	}
}

func TestColorFunctions_ThreadSafe(t *testing.T) {

	var wg sync.WaitGroup
	iterations := 100

	for range iterations {
		wg.Add(4)

		go func() {
			defer wg.Done()
			EnableColors()
		}()

		go func() {
			defer wg.Done()
			DisableColors()
		}()

		go func() {
			defer wg.Done()
			_ = ColorsEnabled()
		}()

		go func() {
			defer wg.Done()
			_ = Color(Cyan)
		}()
	}

	wg.Wait()
	EnableColors()
}
