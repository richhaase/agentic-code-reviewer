package agent

import (
	"errors"
	"testing"
)

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

func TestRecoverableParseError(t *testing.T) {
	err := &RecoverableParseError{
		Line:    42,
		Message: "invalid JSON: unexpected token",
	}

	want := "parse error at line 42: invalid JSON: unexpected token"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "recoverable parse error",
			err:  &RecoverableParseError{Line: 1, Message: "test"},
			want: true,
		},
		{
			name: "wrapped recoverable error",
			err:  errors.Join(errors.New("context"), &RecoverableParseError{Line: 1, Message: "test"}),
			want: true,
		},
		{
			name: "regular error",
			err:  errors.New("some error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRecoverable(tt.err); got != tt.want {
				t.Errorf("IsRecoverable() = %v, want %v", got, tt.want)
			}
		})
	}
}
