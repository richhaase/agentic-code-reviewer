package agent

import "testing"

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
