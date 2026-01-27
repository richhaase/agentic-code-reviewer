package fpcache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestLoadIgnoreFile_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".acr", "ignore")

	patterns, err := LoadIgnoreFile(path)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(patterns) != 0 {
		t.Errorf("expected empty patterns, got %d", len(patterns))
	}

	// Verify file was created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected empty file to be created")
	}
}

func TestLoadIgnoreFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ignore")

	// Create empty file
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	patterns, err := LoadIgnoreFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 0 {
		t.Errorf("expected empty patterns for empty file, got %d", len(patterns))
	}
}

func TestLoadIgnoreFile_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ignore")

	content := "pattern1\npattern2\npattern3\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	patterns, err := LoadIgnoreFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pattern1", "pattern2", "pattern3"}
	if len(patterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d", len(expected), len(patterns))
	}

	for i, want := range expected {
		if patterns[i] != want {
			t.Errorf("pattern[%d] = %q, want %q", i, patterns[i], want)
		}
	}
}

func TestLoadIgnoreFile_TrimsWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ignore")

	content := "  pattern1  \n\npattern2\n  \n  pattern3\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	patterns, err := LoadIgnoreFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pattern1", "pattern2", "pattern3"}
	if len(patterns) != len(expected) {
		t.Fatalf("expected %d patterns, got %d", len(expected), len(patterns))
	}

	for i, want := range expected {
		if patterns[i] != want {
			t.Errorf("pattern[%d] = %q, want %q", i, patterns[i], want)
		}
	}
}

func TestSaveIgnoreFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "ignore")

	patterns := []string{"pattern1", "pattern2", "pattern3"}
	if err := SaveIgnoreFile(path, patterns); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	expected := "pattern1\npattern2\npattern3\n"
	if string(data) != expected {
		t.Errorf("saved content = %q, want %q", string(data), expected)
	}
}

func TestSaveIgnoreFile_EmptyPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ignore")

	if err := SaveIgnoreFile(path, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestMatchesIgnore_ExactMatch(t *testing.T) {
	patterns := []string{"Unused variable"}
	title := "Unused variable"

	if !MatchesIgnore(title, patterns) {
		t.Error("expected exact match to return true")
	}
}

func TestMatchesIgnore_SubstringMatch(t *testing.T) {
	patterns := []string{"variable"}
	title := "Unused variable in function"

	if !MatchesIgnore(title, patterns) {
		t.Error("expected substring match to return true")
	}
}

func TestMatchesIgnore_NoMatch(t *testing.T) {
	patterns := []string{"error", "warning"}
	title := "Unused variable"

	if MatchesIgnore(title, patterns) {
		t.Error("expected no match to return false")
	}
}

func TestMatchesIgnore_EmptyPatterns(t *testing.T) {
	patterns := []string{}
	title := "Any title"

	if MatchesIgnore(title, patterns) {
		t.Error("expected empty patterns to return false")
	}
}

func TestMatchesIgnore_MultiplePatterns(t *testing.T) {
	patterns := []string{"Error", "Warning", "Unused"}
	tests := []struct {
		title string
		want  bool
	}{
		{"Unused variable", true},
		{"Error in function", true},
		{"Warning: deprecated", true},
		{"Info: completed", false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := MatchesIgnore(tt.title, patterns); got != tt.want {
				t.Errorf("MatchesIgnore(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestMatchesIgnore_CaseSensitive(t *testing.T) {
	patterns := []string{"Error"}
	tests := []struct {
		title string
		want  bool
	}{
		{"Error in function", true},
		{"error in function", false}, // lowercase should not match
		{"ERROR in function", false}, // uppercase should not match
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := MatchesIgnore(tt.title, patterns); got != tt.want {
				t.Errorf("MatchesIgnore(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestApplyIgnoreFilter_FiltersFindings(t *testing.T) {
	patterns := []string{"Unused variable"}
	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Unused variable x", Summary: "x is never used"},
			{Title: "SQL injection risk", Summary: "Unsafe query"},
		},
		Info: []domain.FindingGroup{
			{Title: "Info item", Summary: "Some info"},
		},
	}

	result, count := ApplyIgnoreFilter(input, patterns)

	if count != 1 {
		t.Errorf("expected 1 filtered item, got %d", count)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Title != "SQL injection risk" {
		t.Errorf("expected 'SQL injection risk', got %q", result.Findings[0].Title)
	}
	if len(result.Info) != 1 {
		t.Errorf("expected info to be unchanged, got %d items", len(result.Info))
	}
}

func TestApplyIgnoreFilter_FiltersInfo(t *testing.T) {
	patterns := []string{"Deprecated"}
	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Finding 1", Summary: "Some finding"},
		},
		Info: []domain.FindingGroup{
			{Title: "Deprecated API usage", Summary: "Use new API"},
			{Title: "Other info", Summary: "Other"},
		},
	}

	result, count := ApplyIgnoreFilter(input, patterns)

	if count != 1 {
		t.Errorf("expected 1 filtered item, got %d", count)
	}
	if len(result.Info) != 1 {
		t.Fatalf("expected 1 info item, got %d", len(result.Info))
	}
	if result.Info[0].Title != "Other info" {
		t.Errorf("expected 'Other info', got %q", result.Info[0].Title)
	}
	if len(result.Findings) != 1 {
		t.Errorf("expected findings to be unchanged, got %d items", len(result.Findings))
	}
}

func TestApplyIgnoreFilter_EmptyPatterns(t *testing.T) {
	patterns := []string{}
	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Finding 1", Summary: "Some finding"},
			{Title: "Finding 2", Summary: "Another finding"},
		},
		Info: []domain.FindingGroup{
			{Title: "Info 1", Summary: "Some info"},
		},
	}

	result, count := ApplyIgnoreFilter(input, patterns)

	if count != 0 {
		t.Errorf("expected 0 filtered items, got %d", count)
	}
	if len(result.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Findings))
	}
	if len(result.Info) != 1 {
		t.Errorf("expected 1 info item, got %d", len(result.Info))
	}
}

func TestApplyIgnoreFilter_PreservesNonMatching(t *testing.T) {
	patterns := []string{"filter-me"}
	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Keep this", Summary: "Summary 1"},
			{Title: "filter-me out", Summary: "Summary 2"},
			{Title: "Also keep", Summary: "Summary 3"},
		},
	}

	result, count := ApplyIgnoreFilter(input, patterns)

	if count != 1 {
		t.Errorf("expected 1 filtered item, got %d", count)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result.Findings))
	}

	expectedTitles := []string{"Keep this", "Also keep"}
	for i, want := range expectedTitles {
		if result.Findings[i].Title != want {
			t.Errorf("finding[%d].Title = %q, want %q", i, result.Findings[i].Title, want)
		}
	}
}

func TestApplyIgnoreFilter_DoesNotMutateOriginal(t *testing.T) {
	patterns := []string{"remove"}
	original := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "remove this", Summary: "Summary 1"},
			{Title: "keep this", Summary: "Summary 2"},
		},
		Info: []domain.FindingGroup{
			{Title: "remove info", Summary: "Summary 3"},
		},
	}

	result, _ := ApplyIgnoreFilter(original, patterns)

	// Original should be unchanged
	if len(original.Findings) != 2 {
		t.Errorf("original findings mutated: got %d, want 2", len(original.Findings))
	}
	if len(original.Info) != 1 {
		t.Errorf("original info mutated: got %d, want 1", len(original.Info))
	}

	// Result should be filtered
	if len(result.Findings) != 1 {
		t.Errorf("result findings: got %d, want 1", len(result.Findings))
	}
	if len(result.Info) != 0 {
		t.Errorf("result info: got %d, want 0", len(result.Info))
	}
}
