package agent

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestNewClaudeSummaryParser(t *testing.T) {
	parser := NewClaudeSummaryParser()
	if parser == nil {
		t.Fatal("NewClaudeSummaryParser() returned nil")
	}
}

func TestClaudeSummaryParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    *domain.GroupedFindings
		wantErr bool
	}{
		{
			name: "valid JSON with structured_output wrapper",
			input: []byte(`{
				"structured_output": {
					"findings": [
						{"title": "Bug Found", "summary": "A bug was found", "messages": ["msg1"], "reviewer_count": 2, "sources": [1, 2]}
					],
					"info": [
						{"title": "Note", "summary": "A note", "messages": ["info1"], "reviewer_count": 1, "sources": [3]}
					]
				}
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Bug Found", Summary: "A bug was found", Messages: []string{"msg1"}, ReviewerCount: 2, Sources: []int{1, 2}},
				},
				Info: []domain.FindingGroup{
					{Title: "Note", Summary: "A note", Messages: []string{"info1"}, ReviewerCount: 1, Sources: []int{3}},
				},
			},
			wantErr: false,
		},
		{
			name: "valid JSON with only findings",
			input: []byte(`{
				"structured_output": {
					"findings": [
						{"title": "Issue", "summary": "An issue", "messages": ["m1", "m2"], "reviewer_count": 3, "sources": [1]}
					],
					"info": []
				}
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Issue", Summary: "An issue", Messages: []string{"m1", "m2"}, ReviewerCount: 3, Sources: []int{1}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:  "empty findings and info",
			input: []byte(`{"structured_output": {"findings": [], "info": []}}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{invalid json`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(``),
			want:    nil,
			wantErr: true,
		},
		{
			name:  "missing structured_output field",
			input: []byte(`{"findings": [], "info": []}`),
			want: &domain.GroupedFindings{
				Findings: nil,
				Info:     nil,
			},
			wantErr: false,
		},
		{
			name: "multiple findings",
			input: []byte(`{
				"structured_output": {
					"findings": [
						{"title": "F1", "summary": "S1", "messages": [], "reviewer_count": 1, "sources": []},
						{"title": "F2", "summary": "S2", "messages": ["a", "b"], "reviewer_count": 2, "sources": [1, 2, 3]}
					],
					"info": []
				}
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "F1", Summary: "S1", Messages: []string{}, ReviewerCount: 1, Sources: []int{}},
					{Title: "F2", Summary: "S2", Messages: []string{"a", "b"}, ReviewerCount: 2, Sources: []int{1, 2, 3}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name: "with additional metadata fields",
			input: []byte(`{
				"model": "claude-3",
				"usage": {"input_tokens": 100},
				"structured_output": {
					"findings": [
						{"title": "Issue", "summary": "Test", "messages": [], "reviewer_count": 1, "sources": []}
					],
					"info": []
				}
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Issue", Summary: "Test", Messages: []string{}, ReviewerCount: 1, Sources: []int{}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
	}

	parser := NewClaudeSummaryParser()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.Parse(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got.Findings) != len(tt.want.Findings) {
				t.Errorf("Parse() got %d findings, want %d", len(got.Findings), len(tt.want.Findings))
			}

			if len(got.Info) != len(tt.want.Info) {
				t.Errorf("Parse() got %d info, want %d", len(got.Info), len(tt.want.Info))
			}

			for i := range got.Findings {
				if got.Findings[i].Title != tt.want.Findings[i].Title {
					t.Errorf("Finding[%d].Title = %q, want %q", i, got.Findings[i].Title, tt.want.Findings[i].Title)
				}
				if got.Findings[i].Summary != tt.want.Findings[i].Summary {
					t.Errorf("Finding[%d].Summary = %q, want %q", i, got.Findings[i].Summary, tt.want.Findings[i].Summary)
				}
				if got.Findings[i].ReviewerCount != tt.want.Findings[i].ReviewerCount {
					t.Errorf("Finding[%d].ReviewerCount = %d, want %d", i, got.Findings[i].ReviewerCount, tt.want.Findings[i].ReviewerCount)
				}
			}
		})
	}
}

func TestClaudeSummaryParser_SummaryParserInterface(t *testing.T) {
	var _ SummaryParser = (*ClaudeSummaryParser)(nil)
}
