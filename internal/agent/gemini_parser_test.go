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
			name:       "single JSON finding with text field",
			reviewerID: 1,
			input:      `{"text": "Missing error handling"}`,
			want:       []string{"Missing error handling"},
		},
		{
			name:       "single JSON finding with message field",
			reviewerID: 1,
			input:      `{"message": "Security vulnerability found"}`,
			want:       []string{"Security vulnerability found"},
		},
		{
			name:       "single JSON finding with content field",
			reviewerID: 1,
			input:      `{"content": "Performance issue detected"}`,
			want:       []string{"Performance issue detected"},
		},
		{
			name:       "JSON finding with response field (gemini CLI format)",
			reviewerID: 1,
			input:      `{"session_id": "abc123", "response": "internal/agent/claude.go:61: ARG_MAX risk", "stats": {"models": {}}}`,
			want:       []string{"internal/agent/claude.go:61: ARG_MAX risk"},
		},
		{
			name:       "multiple JSON findings",
			reviewerID: 2,
			input: `{"text": "Finding 1"}
{"message": "Finding 2"}
{"content": "Finding 3"}`,
			want: []string{"Finding 1", "Finding 2", "Finding 3"},
		},
		{
			name:       "plain text findings",
			reviewerID: 1,
			input: `auth/login.go:45: SQL injection vulnerability
api/handler.go:123: Resource leak detected`,
			want: []string{
				"auth/login.go:45: SQL injection vulnerability",
				"api/handler.go:123: Resource leak detected",
			},
		},
		{
			name:       "mixed JSON and plain text",
			reviewerID: 1,
			input: `{"text": "JSON finding"}
Plain text finding
{"message": "Another JSON finding"}`,
			want: []string{"JSON finding", "Plain text finding", "Another JSON finding"},
		},
		{
			name:       "findings with empty lines",
			reviewerID: 1,
			input: `{"text": "Finding 1"}

{"text": "Finding 2"}
`,
			want: []string{"Finding 1", "Finding 2"},
		},
		{
			name:       "empty text fields ignored",
			reviewerID: 1,
			input: `{"text": ""}
{"text": "Valid finding"}
{"message": ""}`,
			want: []string{"Valid finding"},
		},
		{
			name:       "JSON without recognized fields skipped",
			reviewerID: 1,
			input: `{"status": "processing"}
{"text": "Valid finding"}
{"debug": "some debug info"}`,
			want: []string{"Valid finding"},
		},
		{
			name:       "comments and non-findings filtered",
			reviewerID: 1,
			input: `# This is a comment
{"text": "Valid finding"}
No issues found
Looks good
{"message": "Another finding"}`,
			want: []string{"Valid finding", "Another finding"},
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
			name:       "findings with special characters",
			reviewerID: 3,
			input:      `{"text": "Finding with \"quotes\" and\nnewlines"}`,
			want:       []string{"Finding with \"quotes\" and\nnewlines"},
		},
		{
			name:       "nested JSON objects",
			reviewerID: 1,
			input:      `{"text": "Valid finding", "metadata": {"severity": "high"}}`,
			want:       []string{"Valid finding"},
		},
		{
			name:       "case insensitive no issues filtering",
			reviewerID: 1,
			input: `NO ISSUES FOUND
{"text": "Valid finding"}
No Issues found
Looks Good
LOOKS GOOD`,
			want: []string{"Valid finding"},
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

func TestGeminiOutputParser_Close(t *testing.T) {
	parser := NewGeminiOutputParser(1)
	err := parser.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
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
	// Verify that GeminiOutputParser implements the OutputParser interface
	var _ OutputParser = (*GeminiOutputParser)(nil)
}

func BenchmarkGeminiOutputParser_ReadFinding(b *testing.B) {
	input := `{"text": "Performance issue detected"}
{"message": "Memory leak found"}
{"content": "Security vulnerability"}
`
	parser := NewGeminiOutputParser(1)

	b.ResetTimer()
	for b.Loop() {
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

func BenchmarkGeminiOutputParser_PlainText(b *testing.B) {
	input := `auth/login.go:45: SQL injection vulnerability
api/handler.go:123: Resource leak detected
utils/parser.go:67: Potential panic
`
	parser := NewGeminiOutputParser(1)

	b.ResetTimer()
	for b.Loop() {
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
