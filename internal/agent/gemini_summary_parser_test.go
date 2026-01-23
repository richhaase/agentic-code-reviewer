package agent

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestNewGeminiSummaryParser(t *testing.T) {
	parser := NewGeminiSummaryParser()
	if parser == nil {
		t.Fatal("NewGeminiSummaryParser() returned nil")
	}
}

func TestGeminiSummaryParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    *domain.GroupedFindings
		wantErr bool
	}{
		{
			name: "valid JSON with response wrapper",
			input: []byte(`{
				"response": "{\"findings\": [{\"title\": \"Bug Found\", \"summary\": \"A bug was found\", \"messages\": [\"msg1\"], \"reviewer_count\": 2, \"sources\": [1, 2]}], \"info\": [{\"title\": \"Note\", \"summary\": \"A note\", \"messages\": [\"info1\"], \"reviewer_count\": 1, \"sources\": [3]}]}"
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
			name: "valid JSON with markdown code fence",
			input: []byte(`{
				"response": "` + "```json\\n{\\\"findings\\\": [{\\\"title\\\": \\\"Issue\\\", \\\"summary\\\": \\\"An issue\\\", \\\"messages\\\": [], \\\"reviewer_count\\\": 1, \\\"sources\\\": []}], \\\"info\\\": []}\\n```" + `"
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Issue", Summary: "An issue", Messages: []string{}, ReviewerCount: 1, Sources: []int{}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name: "valid JSON with plain code fence",
			input: []byte(`{
				"response": "` + "```\\n{\\\"findings\\\": [], \\\"info\\\": []}\\n```" + `"
			}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:  "empty findings and info",
			input: []byte(`{"response": "{\"findings\": [], \"info\": []}"}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:    "invalid outer JSON",
			input:   []byte(`{invalid json`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid inner JSON in response",
			input:   []byte(`{"response": "{invalid json}"}`),
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
			name: "multiple findings",
			input: []byte(`{
				"response": "{\"findings\": [{\"title\": \"F1\", \"summary\": \"S1\", \"messages\": [], \"reviewer_count\": 1, \"sources\": []}, {\"title\": \"F2\", \"summary\": \"S2\", \"messages\": [\"a\", \"b\"], \"reviewer_count\": 2, \"sources\": [1, 2, 3]}], \"info\": []}"
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
	}

	parser := NewGeminiSummaryParser()

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

func TestStripMarkdownCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no code fence",
			input: `{"findings": []}`,
			want:  `{"findings": []}`,
		},
		{
			name:  "json code fence",
			input: "```json\n{\"findings\": []}\n```",
			want:  `{"findings": []}`,
		},
		{
			name:  "plain code fence",
			input: "```\n{\"findings\": []}\n```",
			want:  `{"findings": []}`,
		},
		{
			name:  "code fence with extra whitespace",
			input: "  ```json\n{\"findings\": []}\n```  ",
			want:  `{"findings": []}`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace",
			input: "   \n\t\n   ",
			want:  "",
		},
		{
			name:  "unclosed code fence",
			input: "```json\n{\"findings\": []}",
			want:  `{"findings": []}`,
		},
		{
			name:  "single-line code fence with language",
			input: "```json{\"findings\": []}```",
			want:  `{"findings": []}`,
		},
		{
			name:  "single-line code fence without language",
			input: "```{\"findings\": []}```",
			want:  `{"findings": []}`,
		},
		{
			name:  "single-line code fence with array",
			input: "```json[1, 2, 3]```",
			want:  `[1, 2, 3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdownCodeFence(tt.input)
			if got != tt.want {
				t.Errorf("StripMarkdownCodeFence() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeminiSummaryParser_SummaryParserInterface(t *testing.T) {
	var _ SummaryParser = (*GeminiSummaryParser)(nil)
}
