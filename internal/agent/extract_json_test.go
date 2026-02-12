package agent

import (
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "clean JSON object",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "clean JSON array",
			input: `[{"id": 1}, {"id": 2}]`,
			want:  `[{"id": 1}, {"id": 2}]`,
		},
		{
			name:  "JSON object with trailing prose",
			input: `{"key": "value"} and some extra text`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "JSON array with trailing prose",
			input: `[{"id": 1}] that was the result`,
			want:  `[{"id": 1}]`,
		},
		{
			name:  "prose with embedded JSON object",
			input: `Here are the results: {"evaluations": [{"id": 0, "fp_score": 85}]}`,
			want:  `{"evaluations": [{"id": 0, "fp_score": 85}]}`,
		},
		{
			name:  "prose with embedded JSON array",
			input: `The findings are: [{"title": "bug", "severity": "high"}]`,
			want:  `[{"title": "bug", "severity": "high"}]`,
		},
		{
			name:  "markdown code fence with JSON object",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "markdown code fence with JSON array",
			input: "```json\n[{\"id\": 1}]\n```",
			want:  `[{"id": 1}]`,
		},
		{
			name:  "nested braces",
			input: `{"outer": {"inner": {"deep": true}}}`,
			want:  `{"outer": {"inner": {"deep": true}}}`,
		},
		{
			name:  "nested brackets",
			input: `[[1, 2], [3, 4]]`,
			want:  `[[1, 2], [3, 4]]`,
		},
		{
			name:  "strings with braces inside",
			input: `{"msg": "use {braces} here"}`,
			want:  `{"msg": "use {braces} here"}`,
		},
		{
			name:  "strings with escaped quotes",
			input: `{"msg": "say \"hello\""}`,
			want:  `{"msg": "say \"hello\""}`,
		},
		{
			name:  "whitespace padding",
			input: "  \n  {\"key\": \"value\"}  \n  ",
			want:  `{"key": "value"}`,
		},
		{
			name:    "no JSON at all",
			input:   "This is just plain text with no JSON.",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only whitespace",
			input:   "   \n\t  ",
			wantErr: true,
		},
		{
			name:  "array before object in prose",
			input: `Results: [{"id": 1}] or {"alt": true}`,
			want:  `[{"id": 1}]`,
		},
		{
			name:  "object before array in prose",
			input: `Results: {"evaluations": []} or [1,2]`,
			want:  `{"evaluations": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ExtractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}
