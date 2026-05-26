package agent

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestNewAntigravitySummaryParser(t *testing.T) {
	parser := NewAntigravitySummaryParser()
	if parser == nil {
		t.Fatal("NewAntigravitySummaryParser() returned nil")
	}
}

func TestAntigravitySummaryParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    *domain.GroupedFindings
		wantErr bool
	}{
		{
			name:  "valid raw JSON",
			input: []byte(`{"findings":[{"title":"Bug Found","summary":"A bug was found","messages":["msg1"],"reviewer_count":2,"sources":[1,2]}],"info":[{"title":"Note","summary":"A note","messages":["info1"],"reviewer_count":1,"sources":[3]}]}`),
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
			name:  "valid JSON with markdown code fence",
			input: []byte("```json\n{\"findings\":[{\"title\":\"Issue\",\"summary\":\"An issue\",\"messages\":[],\"reviewer_count\":1,\"sources\":[]}],\"info\":[]}\n```"),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Issue", Summary: "An issue", Messages: []string{}, ReviewerCount: 1, Sources: []int{}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{invalid json}`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing findings field",
			input:   []byte(`{"info":[]}`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "missing info field",
			input:   []byte(`{"findings":[]}`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "null findings field",
			input:   []byte(`{"findings":null,"info":[]}`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "whitespace padded null findings field",
			input:   []byte("{\"findings\": \n null \t,\"info\":[]}"),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   []byte(``),
			want:    nil,
			wantErr: true,
		},
	}

	parser := NewAntigravitySummaryParser()

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

func TestAntigravitySummaryParser_SummaryParserInterface(t *testing.T) {
	var _ SummaryParser = (*AntigravitySummaryParser)(nil)
}
