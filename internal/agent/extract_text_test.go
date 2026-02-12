package agent

import (
	"strings"
	"testing"
)

func TestCodexSummaryParser_ExtractText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "valid JSONL with completed message",
			input: "{\"type\":\"item.created\",\"item\":{\"type\":\"agent_message\",\"text\":\"\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{\\\"evaluations\\\":[]}\"}}",
			want:  `{"evaluations":[]}`,
		},
		{
			name:  "text with markdown code fence",
			input: "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"```json\\n{\\\"evaluations\\\":[]}\\n```\"}}",
			want:  `{"evaluations":[]}`,
		},
		{
			name:    "no agent_message",
			input:   "{\"type\":\"item.created\",\"item\":{\"type\":\"agent_message\",\"text\":\"\"}}",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
	}

	p := NewCodexSummaryParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ExtractText([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ExtractText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaudeSummaryParser_ExtractText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSub string // substring expected in output
		wantErr bool
	}{
		{
			name:    "result preferred over structured_output",
			input:   `{"result":"{\"evaluations\":[{\"id\":0,\"fp_score\":90}]}","structured_output":{"findings":[],"info":[]}}`,
			wantSub: "evaluations",
		},
		{
			name:    "result only",
			input:   `{"result":"{\"evaluations\":[]}"}`,
			wantSub: "evaluations",
		},
		{
			name:    "structured_output fallback when result empty",
			input:   `{"result":"","structured_output":{"findings":[],"info":[]}}`,
			wantSub: "findings",
		},
		{
			name:    "both empty",
			input:   `{"result":"","structured_output":null}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
	}

	p := NewClaudeSummaryParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ExtractText([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("ExtractText() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestGeminiSummaryParser_ExtractText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSub string
		wantErr bool
	}{
		{
			name:    "valid response",
			input:   `{"response":"{\"evaluations\":[]}"}`,
			wantSub: "evaluations",
		},
		{
			name:    "response with code fence",
			input:   "{\"response\":\"```json\\n{\\\"evaluations\\\":[]}\\n```\"}",
			wantSub: "evaluations",
		},
		{
			name:    "empty response",
			input:   `{"response":""}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
	}

	p := NewGeminiSummaryParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.ExtractText([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("ExtractText() = %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

// Verify Parse still works after refactor (regression test)
func TestCodexSummaryParser_Parse_StillWorks(t *testing.T) {
	input := `{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\":[{\"title\":\"test\",\"summary\":\"s\",\"messages\":[\"m\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}"}}`

	p := NewCodexSummaryParser()
	grouped, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grouped.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(grouped.Findings))
	}
}

func TestClaudeSummaryParser_Parse_StillWorks(t *testing.T) {
	input := `{"result":"{\"findings\":[{\"title\":\"test\",\"summary\":\"s\",\"messages\":[\"m\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}","structured_output":{"findings":[],"info":[]}}`

	p := NewClaudeSummaryParser()
	grouped, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grouped.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(grouped.Findings))
	}
}

func TestGeminiSummaryParser_Parse_StillWorks(t *testing.T) {
	input := `{"response":"{\"findings\":[{\"title\":\"test\",\"summary\":\"s\",\"messages\":[\"m\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}"}`

	p := NewGeminiSummaryParser()
	grouped, err := p.Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(grouped.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(grouped.Findings))
	}
}
