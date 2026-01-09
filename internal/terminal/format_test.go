package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestWrapText_BasicWrapping(t *testing.T) {
	text := "This is a longer sentence that needs to be wrapped at the boundary"
	result := WrapText(text, 30, "")

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) > 30 {
			t.Errorf("line %d exceeds width 30: len=%d, content=%q", i, len(line), line)
		}
	}
}

func TestWrapText_WithIndent(t *testing.T) {
	text := "First Second Third"
	indent := ">>> "
	result := WrapText(text, 15, indent)

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, indent) {
			t.Errorf("line %d missing indent prefix: %q", i, line)
		}
	}
}

func TestWrapText_WordBoundary(t *testing.T) {
	text := "word1 word2 word3"
	result := WrapText(text, 12, "")

	// Should preserve complete words, not split them
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		words := strings.Fields(line)
		for _, word := range words {
			if word != "word1" && word != "word2" && word != "word3" {
				t.Errorf("unexpected partial word in output: %q (full result: %q)", word, result)
			}
		}
	}

	// All three words should be present
	if !strings.Contains(result, "word1") || !strings.Contains(result, "word2") || !strings.Contains(result, "word3") {
		t.Errorf("missing words in result: %q", result)
	}
}

func TestWrapText_EmptyInput(t *testing.T) {
	result := WrapText("", 50, "  ")
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestWrapText_WhitespaceOnlyInput(t *testing.T) {
	result := WrapText("   \t  ", 50, "")
	if result != "" {
		t.Errorf("expected empty string for whitespace-only input, got %q", result)
	}
}

func TestWrapText_SingleLongWord(t *testing.T) {
	// A word longer than width should still appear (no infinite loop)
	longWord := "supercalifragilisticexpialidocious"
	result := WrapText(longWord, 10, "")

	// The word should be present in output
	if !strings.Contains(result, longWord) {
		t.Errorf("long word should be in output: %q", result)
	}
}

func TestWrapText_NarrowWidthWithIndent(t *testing.T) {
	// Width <= indent length: should still produce output
	result := WrapText("hello world", 3, ">>> ")
	if result == "" {
		t.Error("expected non-empty output even with narrow width")
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output: %q", result)
	}
}

func TestWrapText_PreservesAllWords(t *testing.T) {
	words := []string{"The", "quick", "brown", "fox", "jumps"}
	text := strings.Join(words, " ")
	result := WrapText(text, 15, "")

	for _, word := range words {
		if !strings.Contains(result, word) {
			t.Errorf("missing word %q in result: %q", word, result)
		}
	}
}

func TestFormatDuration_UnderOneMinute(t *testing.T) {
	tests := []struct {
		dur      time.Duration
		expected string
	}{
		{500 * time.Millisecond, "0.5s"},
		{5 * time.Second, "5.0s"},
		{45*time.Second + 300*time.Millisecond, "45.3s"},
		{59*time.Second + 999*time.Millisecond, "60.0s"}, // edge: rounds to 60
	}

	for _, tt := range tests {
		t.Run(tt.dur.String(), func(t *testing.T) {
			got := FormatDuration(tt.dur)
			if got != tt.expected {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.dur, got, tt.expected)
			}
		})
	}
}

func TestFormatDuration_OverOneMinute(t *testing.T) {
	tests := []struct {
		dur      time.Duration
		expected string
	}{
		{1 * time.Minute, "1m 0.0s"},
		{1*time.Minute + 30*time.Second, "1m 30.0s"},
		{2*time.Minute + 45*time.Second + 500*time.Millisecond, "2m 45.5s"},
		{10 * time.Minute, "10m 0.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.dur.String(), func(t *testing.T) {
			got := FormatDuration(tt.dur)
			if got != tt.expected {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.dur, got, tt.expected)
			}
		})
	}
}

func TestFormatDuration_Zero(t *testing.T) {
	got := FormatDuration(0)
	if got != "0.0s" {
		t.Errorf("FormatDuration(0) = %q, want %q", got, "0.0s")
	}
}
