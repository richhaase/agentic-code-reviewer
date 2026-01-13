package filter

import (
	"testing"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantErr  bool
	}{
		{
			name:     "empty patterns",
			patterns: []string{},
			wantErr:  false,
		},
		{
			name:     "valid pattern",
			patterns: []string{"Next\\.js forbids"},
			wantErr:  false,
		},
		{
			name:     "multiple valid patterns",
			patterns: []string{"pattern1", "pattern2", ".*test.*"},
			wantErr:  false,
		},
		{
			name:     "invalid regex",
			patterns: []string{"[invalid"},
			wantErr:  true,
		},
		{
			name:     "one invalid among valid",
			patterns: []string{"valid", "[invalid", "also-valid"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := New(tt.patterns)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if f == nil {
				t.Error("expected filter, got nil")
			}
		})
	}
}

func TestFilter_Apply(t *testing.T) {
	tests := []struct {
		name          string
		patterns      []string
		input         domain.GroupedFindings
		wantFindings  int
		wantInfoCount int
		wantTitles    []string
	}{
		{
			name:     "no patterns - no filtering",
			patterns: []string{},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Finding 1", Messages: []string{"some message"}},
					{Title: "Finding 2", Messages: []string{"another message"}},
				},
				Info: []domain.FindingGroup{
					{Title: "Info 1", Messages: []string{"info message"}},
				},
			},
			wantFindings:  2,
			wantInfoCount: 1,
			wantTitles:    []string{"Finding 1", "Finding 2"},
		},
		{
			name:     "single pattern excludes matching finding",
			patterns: []string{"Next\\.js forbids"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Valid Finding", Messages: []string{"This is fine"}},
					{Title: "False Positive", Messages: []string{"Next.js forbids this in client bundles"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"Valid Finding"},
		},
		{
			name:     "pattern matches any message in array",
			patterns: []string{"deprecated"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Has deprecated", Messages: []string{"first message", "this uses deprecated API", "third"}},
					{Title: "Clean", Messages: []string{"no issues here"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"Clean"},
		},
		{
			name:     "multiple patterns - excludes if any match",
			patterns: []string{"deprecated", "Next\\.js"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Deprecated Issue", Messages: []string{"deprecated API usage"}},
					{Title: "Next Issue", Messages: []string{"Next.js forbids this"}},
					{Title: "Real Issue", Messages: []string{"SQL injection vulnerability"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"Real Issue"},
		},
		{
			name:     "info is never filtered",
			patterns: []string{".*"}, // matches everything
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Finding", Messages: []string{"will be filtered"}},
				},
				Info: []domain.FindingGroup{
					{Title: "Info 1", Messages: []string{"should remain"}},
					{Title: "Info 2", Messages: []string{"also remains"}},
				},
			},
			wantFindings:  0,
			wantInfoCount: 2,
			wantTitles:    []string{},
		},
		{
			name:     "empty messages - not excluded",
			patterns: []string{"pattern"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "No Messages", Messages: []string{}},
					{Title: "Nil Messages", Messages: nil},
				},
			},
			wantFindings: 2,
			wantTitles:   []string{"No Messages", "Nil Messages"},
		},
		{
			name:     "case sensitive matching",
			patterns: []string{"ERROR"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Lowercase", Messages: []string{"error message"}},
					{Title: "Uppercase", Messages: []string{"ERROR message"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"Lowercase"},
		},
		{
			name:     "case insensitive pattern",
			patterns: []string{"(?i)error"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Lowercase", Messages: []string{"error message"}},
					{Title: "Uppercase", Messages: []string{"ERROR message"}},
					{Title: "Mixed", Messages: []string{"Error message"}},
					{Title: "No match", Messages: []string{"warning message"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"No match"},
		},
		{
			name:     "regex special characters",
			patterns: []string{"\\[warning\\]"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Has bracket", Messages: []string{"[warning] something"}},
					{Title: "No bracket", Messages: []string{"warning something"}},
				},
			},
			wantFindings: 1,
			wantTitles:   []string{"No bracket"},
		},
		{
			name:     "empty input",
			patterns: []string{"pattern"},
			input: domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantFindings:  0,
			wantInfoCount: 0,
			wantTitles:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := New(tt.patterns)
			if err != nil {
				t.Fatalf("failed to create filter: %v", err)
			}

			result := f.Apply(tt.input)

			if len(result.Findings) != tt.wantFindings {
				t.Errorf("got %d findings, want %d", len(result.Findings), tt.wantFindings)
			}

			if tt.wantInfoCount > 0 && len(result.Info) != tt.wantInfoCount {
				t.Errorf("got %d info, want %d", len(result.Info), tt.wantInfoCount)
			}

			gotTitles := make([]string, len(result.Findings))
			for i, f := range result.Findings {
				gotTitles[i] = f.Title
			}

			if len(gotTitles) != len(tt.wantTitles) {
				t.Errorf("got titles %v, want %v", gotTitles, tt.wantTitles)
				return
			}

			for i, title := range tt.wantTitles {
				if gotTitles[i] != title {
					t.Errorf("title[%d] = %q, want %q", i, gotTitles[i], title)
				}
			}
		})
	}
}

func TestFilter_Apply_DoesNotMutateOriginal(t *testing.T) {
	original := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "To Remove", Messages: []string{"exclude me"}},
			{Title: "To Keep", Messages: []string{"keep me"}},
		},
		Info: []domain.FindingGroup{
			{Title: "Info", Messages: []string{"exclude me"}},
		},
	}

	f, _ := New([]string{"exclude"})
	result := f.Apply(original)

	// Original should be unchanged
	if len(original.Findings) != 2 {
		t.Errorf("original was mutated: got %d findings, want 2", len(original.Findings))
	}
	if len(original.Info) != 1 {
		t.Errorf("original was mutated: got %d info, want 1", len(original.Info))
	}

	// Result should be filtered
	if len(result.Findings) != 1 {
		t.Errorf("result not filtered: got %d findings, want 1", len(result.Findings))
	}
	if len(result.Info) != 1 {
		t.Errorf("result info changed: got %d info, want 1", len(result.Info))
	}
}
