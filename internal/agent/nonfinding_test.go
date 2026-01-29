package agent

import "testing"

func TestIsNonFindingText(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		// Positive cases - should be detected as non-findings
		{"No issues found", true},
		{"No issues found in this code", true},
		{"no findings to report", true},
		{"No bugs detected", true},
		{"There are no problems here", true},
		{"Looks good to me!", true},
		{"LOOKS GOOD", true},
		{"Code looks clean", true},
		{"The code looks correct", true},
		{"Review complete", true},
		{"Review complete - no issues", true},

		// Negative cases - should NOT be detected as non-findings
		{"Bug: missing null check", false},
		{"Error handling needed", false},
		{"Consider adding validation", false},
		{"This function is too complex", false},
		{"Memory leak in line 42", false},
		{"", false},
		{"Some random text", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := IsNonFindingText(tt.text); got != tt.expected {
				t.Errorf("IsNonFindingText(%q) = %v, want %v", tt.text, got, tt.expected)
			}
		})
	}
}
