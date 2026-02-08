package main

import (
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestFilterFindingsByIndices_SelectsCorrectFindings(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 0"},
		{Title: "Finding 1"},
		{Title: "Finding 2"},
		{Title: "Finding 3"},
	}

	result := filterFindingsByIndices(findings, []int{1, 3})

	if len(result) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result))
	}
	if result[0].Title != "Finding 1" {
		t.Errorf("expected 'Finding 1', got %q", result[0].Title)
	}
	if result[1].Title != "Finding 3" {
		t.Errorf("expected 'Finding 3', got %q", result[1].Title)
	}
}

func TestFilterFindingsByIndices_EmptyIndices(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 0"},
	}

	result := filterFindingsByIndices(findings, []int{})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty indices, got %d", len(result))
	}

	result = filterFindingsByIndices(findings, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil indices, got %d", len(result))
	}
}

func TestFilterFindingsByIndices_OutOfBoundsIgnored(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "Finding 0"},
		{Title: "Finding 1"},
	}

	// Index 5 is out of bounds, should be ignored
	result := filterFindingsByIndices(findings, []int{0, 5, 1})

	if len(result) != 2 {
		t.Fatalf("expected 2 findings (out of bounds ignored), got %d", len(result))
	}
}

func TestFilterFindingsByIndices_PreservesOrder(t *testing.T) {
	findings := []domain.FindingGroup{
		{Title: "A"},
		{Title: "B"},
		{Title: "C"},
	}

	// Indices in reverse order - result should follow findings order, not indices order
	result := filterFindingsByIndices(findings, []int{2, 0})

	if len(result) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result))
	}
	// Should be in findings order: A (index 0), then C (index 2)
	if result[0].Title != "A" {
		t.Errorf("expected 'A' first, got %q", result[0].Title)
	}
	if result[1].Title != "C" {
		t.Errorf("expected 'C' second, got %q", result[1].Title)
	}
}

func TestExitCodeError_Error(t *testing.T) {
	tests := []struct {
		code     domain.ExitCode
		contains string
	}{
		{domain.ExitFindings, "findings were reported"},
		{domain.ExitError, "review failed with error"},
		{domain.ExitInterrupted, "review was interrupted"},
		{domain.ExitCode(99), "exit code 99"},
	}

	for _, tt := range tests {
		t.Run(tt.contains, func(t *testing.T) {
			err := exitCodeError{code: tt.code}
			if err.Error() != tt.contains {
				t.Errorf("expected %q, got %q", tt.contains, err.Error())
			}
		})
	}
}

func TestExitCode_ReturnsNilForNoFindings(t *testing.T) {
	err := exitCode(domain.ExitNoFindings)
	if err != nil {
		t.Errorf("expected nil for ExitNoFindings, got %v", err)
	}
}

func TestExitCode_ReturnsErrorForOtherCodes(t *testing.T) {
	codes := []domain.ExitCode{
		domain.ExitFindings,
		domain.ExitError,
		domain.ExitInterrupted,
	}

	for _, code := range codes {
		err := exitCode(code)
		if err == nil {
			t.Errorf("expected error for code %d, got nil", code)
		}
		exitErr, ok := err.(exitCodeError)
		if !ok {
			t.Errorf("expected exitCodeError type, got %T", err)
		}
		if exitErr.code != code {
			t.Errorf("expected code %d, got %d", code, exitErr.code)
		}
	}
}

func TestLogCIChecks_Truncation(t *testing.T) {
	if maxDisplayedCIChecks != 5 {
		t.Errorf("maxDisplayedCIChecks = %d, want 5", maxDisplayedCIChecks)
	}
}

func TestFormatPRRef(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		result := formatPRRef("123")
		if !strings.Contains(result, "#123") {
			t.Errorf("formatPRRef(123) = %q, want to contain '#123'", result)
		}
	})
}

func TestFormatPrompt(t *testing.T) {
	terminal.WithColorsDisabled(func() {
		result := formatPrompt("Post review", "[Y]es / [N]o:")
		if !strings.Contains(result, "Post review") {
			t.Errorf("formatPrompt() = %q, want to contain question", result)
		}
		if !strings.Contains(result, "[Y]es / [N]o:") {
			t.Errorf("formatPrompt() = %q, want to contain options", result)
		}
		if !strings.HasSuffix(result, " ") {
			t.Errorf("formatPrompt() = %q, should end with space", result)
		}
	})
}
