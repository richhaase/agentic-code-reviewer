package feedback

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFetchPRContext_NoPRNumber(t *testing.T) {
	ctx := context.Background()
	_, err := FetchPRContext(ctx, "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}

func TestPRContext_HasContent(t *testing.T) {
	tests := []struct {
		name     string
		ctx      PRContext
		expected bool
	}{
		{
			name:     "empty context",
			ctx:      PRContext{},
			expected: false,
		},
		{
			name:     "description only",
			ctx:      PRContext{Description: "Fix bug"},
			expected: true,
		},
		{
			name:     "comments only",
			ctx:      PRContext{Comments: []Comment{{Body: "LGTM"}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.HasContent(); got != tt.expected {
				t.Errorf("HasContent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseNDJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty input",
			input:     []byte{},
			wantCount: 0,
		},
		{
			name:      "single object",
			input:     []byte(`{"user":{"login":"alice"},"body":"looks good"}`),
			wantCount: 1,
		},
		{
			name:      "multiple objects newline separated",
			input:     []byte("{\"user\":{\"login\":\"alice\"},\"body\":\"comment 1\"}\n{\"user\":{\"login\":\"bob\"},\"body\":\"comment 2\"}\n"),
			wantCount: 2,
		},
		{
			name:      "objects with extra whitespace between",
			input:     []byte("{\"user\":{\"login\":\"alice\"},\"body\":\"c1\"}\n\n{\"user\":{\"login\":\"bob\"},\"body\":\"c2\"}\n"),
			wantCount: 2,
		},
		{
			name:      "invalid json",
			input:     []byte(`{not valid json}`),
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "valid then invalid",
			input:     []byte("{\"user\":{\"login\":\"alice\"},\"body\":\"ok\"}\n{broken}"),
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "empty body filtered by caller not parser",
			input:     []byte(`{"user":{"login":"alice"},"body":""}`),
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := parseNDJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("got %d results, want %d", len(results), tt.wantCount)
			}
		})
	}
}

func TestParseNDJSON_FieldExtraction(t *testing.T) {
	input := []byte(`{"user":{"login":"alice"},"body":"review comment"}`)
	results, err := parseNDJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].User.Login != "alice" {
		t.Errorf("User.Login = %q, want %q", results[0].User.Login, "alice")
	}
	if results[0].Body != "review comment" {
		t.Errorf("Body = %q, want %q", results[0].Body, "review comment")
	}
}

func TestParseNDJSON_LargePayload(t *testing.T) {
	// Build 150 NDJSON objects
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, fmt.Sprintf(`{"user":{"login":"user%d"},"body":"comment %d"}`, i, i))
	}
	input := []byte(strings.Join(lines, "\n"))

	results, err := parseNDJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 150 {
		t.Errorf("expected 150 results, got %d", len(results))
	}
	// Verify first and last
	if results[0].User.Login != "user0" {
		t.Errorf("first login = %q, want %q", results[0].User.Login, "user0")
	}
	if results[149].User.Login != "user149" {
		t.Errorf("last login = %q, want %q", results[149].User.Login, "user149")
	}
}

func TestParseNDJSON_UnicodeContent(t *testing.T) {
	input := []byte(`{"user":{"login":"日本語ユーザー"},"body":"这是一个评论 🚀 émojis and àccents"}`)

	results, err := parseNDJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].User.Login != "日本語ユーザー" {
		t.Errorf("User.Login = %q, want unicode username", results[0].User.Login)
	}
	if !strings.Contains(results[0].Body, "🚀") {
		t.Error("Body should contain emoji")
	}
	if !strings.Contains(results[0].Body, "àccents") {
		t.Error("Body should contain accented characters")
	}
}
