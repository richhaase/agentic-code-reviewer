package terminal

import (
	"fmt"
	"strings"
	"time"
)

// MaxReportWidth is the maximum width for reports.
const MaxReportWidth = 90

// FormatDuration formats a duration in human-readable form.
func FormatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs / 60)
	remainSecs := secs - float64(mins*60)
	return fmt.Sprintf("%dm %.1fs", mins, remainSecs)
}

// Ruler returns a horizontal rule string.
func Ruler(width int, char string) string {
	return fmt.Sprintf("%s%s%s", Color(Dim), strings.Repeat(char, width), Color(Reset))
}

// WrapText wraps text to width with proper indentation.
func WrapText(text string, width int, indent string) string {
	if width <= len(indent) {
		return indent + text
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	var currentLine strings.Builder
	currentLine.WriteString(indent)
	lineWidth := len(indent)
	maxWidth := width

	for i, word := range words {
		wordLen := len(word)

		if i == 0 {
			currentLine.WriteString(word)
			lineWidth += wordLen
			continue
		}

		if lineWidth+1+wordLen > maxWidth {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentLine.WriteString(indent)
			currentLine.WriteString(word)
			lineWidth = len(indent) + wordLen
		} else {
			currentLine.WriteString(" ")
			currentLine.WriteString(word)
			lineWidth += 1 + wordLen
		}
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return strings.Join(lines, "\n")
}

// ReportWidth returns the report width based on terminal width.
func ReportWidth() int {
	w := GetTerminalWidth()
	if w > MaxReportWidth {
		return MaxReportWidth
	}
	return w
}
