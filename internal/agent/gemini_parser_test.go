package agent

import (
	"bufio"
	"strings"
	"testing"
)

func TestGeminiOutputParser_ReadFinding(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		reviewerID int
		want       []string // Expected finding texts in order
	}{
		{
			name:       "single-line JSON with response field (gemini CLI format)",
			reviewerID: 1,
			input:      `{"session_id": "abc123", "response": "internal/agent/claude.go:61: ARG_MAX risk", "stats": {"models": {}}}`,
			want:       []string{"internal/agent/claude.go:61: ARG_MAX risk"},
		},
		{
			name:       "multi-line pretty-printed JSON (real gemini output)",
			reviewerID: 1,
			input: `{
  "session_id": "abc123",
  "response": "1. Missing error handling\n2. Security vulnerability",
  "stats": {
    "models": {}
  }
}`,
			want: []string{"1. Missing error handling\n2. Security vulnerability"},
		},
		{
			name:       "JSON with text field",
			reviewerID: 1,
			input:      `{"text": "Missing error handling"}`,
			want:       []string{"Missing error handling"},
		},
		{
			name:       "JSON with message field",
			reviewerID: 1,
			input:      `{"message": "Security vulnerability found"}`,
			want:       []string{"Security vulnerability found"},
		},
		{
			name:       "JSON with content field",
			reviewerID: 1,
			input:      `{"content": "Performance issue detected"}`,
			want:       []string{"Performance issue detected"},
		},
		{
			name:       "plain text finding (fallback)",
			reviewerID: 1,
			input:      `auth/login.go:45: SQL injection vulnerability`,
			want:       []string{"auth/login.go:45: SQL injection vulnerability"},
		},
		{
			name:       "multi-line plain text treated as single finding",
			reviewerID: 1,
			input: `auth/login.go:45: SQL injection vulnerability
api/handler.go:123: Resource leak detected`,
			want: []string{"auth/login.go:45: SQL injection vulnerability\napi/handler.go:123: Resource leak detected"},
		},
		{
			name:       "empty input",
			reviewerID: 1,
			input:      "",
			want:       []string{},
		},
		{
			name:       "only whitespace",
			reviewerID: 1,
			input:      "\n\n\n",
			want:       []string{},
		},
		{
			name:       "JSON with special characters in response",
			reviewerID: 3,
			input:      `{"response": "Finding with \"quotes\" and\nnewlines"}`,
			want:       []string{"Finding with \"quotes\" and\nnewlines"},
		},
		{
			name:       "nested JSON objects with text field",
			reviewerID: 1,
			input:      `{"text": "Valid finding", "metadata": {"severity": "high"}}`,
			want:       []string{"Valid finding"},
		},
		{
			name:       "no issues response filtered",
			reviewerID: 1,
			input:      `{"response": "No issues found in the code."}`,
			want:       []string{},
		},
		{
			name:       "looks good response filtered",
			reviewerID: 1,
			input:      `{"response": "The code looks good, no problems detected."}`,
			want:       []string{},
		},
		{
			name:       "JSON without recognized fields returns nothing",
			reviewerID: 1,
			input:      `{"status": "processing", "debug": "info"}`,
			want:       []string{},
		},
		{
			name:       "empty response field",
			reviewerID: 1,
			input:      `{"response": ""}`,
			want:       []string{},
		},
		{
			name:       "response field takes priority over others",
			reviewerID: 1,
			input:      `{"response": "Primary finding", "text": "Secondary", "message": "Tertiary"}`,
			want:       []string{"Primary finding"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewGeminiOutputParser(tt.reviewerID)
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			ConfigureScanner(scanner)

			var got []string
			for {
				finding, err := parser.ReadFinding(scanner)
				if err != nil {
					t.Fatalf("ReadFinding() error = %v", err)
				}
				if finding == nil {
					break
				}
				got = append(got, finding.Text)
				if finding.ReviewerID != tt.reviewerID {
					t.Errorf("ReviewerID = %d, want %d", finding.ReviewerID, tt.reviewerID)
				}
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d findings, want %d\nGot: %v\nWant: %v", len(got), len(tt.want), got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("finding[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGeminiOutputParser_ParseErrors(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantParseErrors int
	}{
		{
			name:            "valid JSON",
			input:           `{"response": "Valid finding"}`,
			wantParseErrors: 0,
		},
		{
			name:            "invalid JSON treated as plain text (counts as parse error)",
			input:           "not valid json",
			wantParseErrors: 1,
		},
		{
			name:            "empty input",
			input:           "",
			wantParseErrors: 0,
		},
		{
			name:            "multi-line pretty JSON is valid",
			input:           "{\n  \"response\": \"finding\"\n}",
			wantParseErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewGeminiOutputParser(1)
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			ConfigureScanner(scanner)

			for {
				finding, err := parser.ReadFinding(scanner)
				if err != nil {
					t.Fatalf("ReadFinding() error = %v", err)
				}
				if finding == nil {
					break
				}
			}

			if parser.ParseErrors() != tt.wantParseErrors {
				t.Errorf("ParseErrors() = %d, want %d", parser.ParseErrors(), tt.wantParseErrors)
			}
		})
	}
}

func TestNewGeminiOutputParser(t *testing.T) {
	tests := []struct {
		name       string
		reviewerID int
	}{
		{"reviewer 0", 0},
		{"reviewer 1", 1},
		{"reviewer 100", 100},
		{"negative reviewer", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewGeminiOutputParser(tt.reviewerID)
			if parser == nil {
				t.Fatal("NewGeminiOutputParser() returned nil")
			}
			if parser.reviewerID != tt.reviewerID {
				t.Errorf("reviewerID = %d, want %d", parser.reviewerID, tt.reviewerID)
			}
		})
	}
}

func TestGeminiOutputParserInterface(t *testing.T) {
	// Verify that GeminiOutputParser implements the ReviewParser interface
	var _ ReviewParser = (*GeminiOutputParser)(nil)
}

func TestGeminiOutputParser_MultipleCallsReturnNothing(t *testing.T) {
	// After parsing once, subsequent calls should return nil
	parser := NewGeminiOutputParser(1)
	input := `{"response": "Single finding"}`
	scanner := bufio.NewScanner(strings.NewReader(input))
	ConfigureScanner(scanner)

	// First call should return the finding
	finding, err := parser.ReadFinding(scanner)
	if err != nil {
		t.Fatalf("First ReadFinding() error = %v", err)
	}
	if finding == nil {
		t.Fatal("First ReadFinding() returned nil, expected finding")
	}
	if finding.Text != "Single finding" {
		t.Errorf("finding.Text = %q, want %q", finding.Text, "Single finding")
	}

	// Second call should return nil (no more findings)
	finding, err = parser.ReadFinding(scanner)
	if err != nil {
		t.Fatalf("Second ReadFinding() error = %v", err)
	}
	if finding != nil {
		t.Errorf("Second ReadFinding() = %v, want nil", finding)
	}
}

func BenchmarkGeminiOutputParser_ReadFinding(b *testing.B) {
	input := `{"session_id": "bench", "response": "Performance issue detected\nMemory leak found", "stats": {}}`

	b.ResetTimer()
	for b.Loop() {
		parser := NewGeminiOutputParser(1)
		scanner := bufio.NewScanner(strings.NewReader(input))
		ConfigureScanner(scanner)

		for {
			finding, err := parser.ReadFinding(scanner)
			if err != nil {
				b.Fatal(err)
			}
			if finding == nil {
				break
			}
		}
	}
}

func BenchmarkGeminiOutputParser_PrettyPrintedJSON(b *testing.B) {
	input := `{
  "session_id": "bench",
  "response": "auth/login.go:45: SQL injection vulnerability\napi/handler.go:123: Resource leak detected",
  "stats": {
    "models": {}
  }
}`

	b.ResetTimer()
	for b.Loop() {
		parser := NewGeminiOutputParser(1)
		scanner := bufio.NewScanner(strings.NewReader(input))
		ConfigureScanner(scanner)

		for {
			finding, err := parser.ReadFinding(scanner)
			if err != nil {
				b.Fatal(err)
			}
			if finding == nil {
				break
			}
		}
	}
}
