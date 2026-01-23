package agent

import (
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestNewCodexSummaryParser(t *testing.T) {
	parser := NewCodexSummaryParser()
	if parser == nil {
		t.Fatal("NewCodexSummaryParser() returned nil")
	}
}

// wrapInJSONL wraps a JSON string in the JSONL event format that codex --json outputs.
func wrapInJSONL(jsonContent string) string {
	return `{"type":"thread.started","thread_id":"test-thread"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":` + jsonContent + `}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`
}

func TestCodexSummaryParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    *domain.GroupedFindings
		wantErr bool
	}{
		{
			name:  "valid JSONL with findings and info",
			input: []byte(wrapInJSONL(`"{\"findings\": [{\"title\": \"Bug Found\", \"summary\": \"A bug was found\", \"messages\": [\"msg1\"], \"reviewer_count\": 2, \"sources\": [1, 2]}], \"info\": [{\"title\": \"Note\", \"summary\": \"A note\", \"messages\": [\"info1\"], \"reviewer_count\": 1, \"sources\": [3]}]}"`)),
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
			name:  "valid JSONL with only findings",
			input: []byte(wrapInJSONL(`"{\"findings\": [{\"title\": \"Issue\", \"summary\": \"An issue\", \"messages\": [\"m1\", \"m2\"], \"reviewer_count\": 3, \"sources\": [1]}], \"info\": []}"`)),
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
			input: []byte(wrapInJSONL(`"{\"findings\": [], \"info\": []}"`)),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name:    "no agent_message in output",
			input:   []byte(`{"type":"thread.started"}`),
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
			name:  "multiple findings",
			input: []byte(wrapInJSONL(`"{\"findings\": [{\"title\": \"F1\", \"summary\": \"S1\", \"messages\": [], \"reviewer_count\": 1, \"sources\": []}, {\"title\": \"F2\", \"summary\": \"S2\", \"messages\": [\"a\", \"b\"], \"reviewer_count\": 2, \"sources\": [1, 2, 3]}], \"info\": []}"`)),
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
			name:  "agent_message with markdown code fence",
			input: []byte(wrapInJSONL(`"` + "```json\\n{\\\"findings\\\": [], \\\"info\\\": []}\\n```" + `"`)),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{},
				Info:     []domain.FindingGroup{},
			},
			wantErr: false,
		},
		{
			name: "multiple agent_messages uses last one",
			input: []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"First\", \"summary\": \"S1\", \"messages\": [], \"reviewer_count\": 1, \"sources\": []}], \"info\": []}"}}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"Last\", \"summary\": \"S2\", \"messages\": [], \"reviewer_count\": 1, \"sources\": []}], \"info\": []}"}}`),
			want: &domain.GroupedFindings{
				Findings: []domain.FindingGroup{
					{Title: "Last", Summary: "S2", Messages: []string{}, ReviewerCount: 1, Sources: []int{}},
				},
				Info: []domain.FindingGroup{},
			},
			wantErr: false,
		},
	}

	parser := NewCodexSummaryParser()

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

func TestCodexSummaryParser_SummaryParserInterface(t *testing.T) {
	var _ SummaryParser = (*CodexSummaryParser)(nil)
}

func TestCodexSummaryParser_DecodeErrorIncluded(t *testing.T) {
	parser := NewCodexSummaryParser()

	// Malformed JSON that will cause a decode error
	input := []byte(`{"type":"thread.started"}{invalid json here}`)

	_, err := parser.Parse(input)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}

	// Error should mention "decode error" since no agent_message was found
	if !strings.Contains(err.Error(), "decode error") {
		t.Errorf("error should include decode error details, got: %v", err)
	}
}

func TestCodexSummaryParser_TruncateUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{
			name:  "ASCII within limit",
			input: "hello",
			n:     10,
			want:  "hello",
		},
		{
			name:  "ASCII truncated",
			input: "hello world",
			n:     5,
			want:  "hello...",
		},
		{
			name:  "UTF-8 multibyte preserved",
			input: "héllo wörld",
			n:     5,
			want:  "héllo...",
		},
		{
			name:  "CJK characters",
			input: "你好世界",
			n:     2,
			want:  "你好...",
		},
		{
			name:  "emoji preserved",
			input: "hello 🌍🌎🌏",
			n:     7,
			want:  "hello 🌍...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
			}
		})
	}
}
