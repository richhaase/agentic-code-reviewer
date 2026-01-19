package agent

import (
	"bufio"
	"strings"
	"testing"
)

func TestClaudeOutputParser_ReadFinding(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		reviewerID int
		want       []string // Expected finding texts in order
	}{
		{
			name:       "single plain text finding",
			reviewerID: 1,
			input:      "auth/login.go:45: SQL injection vulnerability",
			want:       []string{"auth/login.go:45: SQL injection vulnerability"},
		},
		{
			name:       "multiple plain text findings",
			reviewerID: 2,
			input: `auth/login.go:45: SQL injection vulnerability
api/handler.go:123: Resource leak detected
utils/parser.go:67: Potential panic`,
			want: []string{
				"auth/login.go:45: SQL injection vulnerability",
				"api/handler.go:123: Resource leak detected",
				"utils/parser.go:67: Potential panic",
			},
		},
		{
			name:       "findings with empty lines",
			reviewerID: 1,
			input: `Finding 1

Finding 2
`,
			want: []string{"Finding 1", "Finding 2"},
		},
		{
			name:       "comments filtered",
			reviewerID: 1,
			input: `# This is a comment
Valid finding
## Another comment`,
			want: []string{"Valid finding"},
		},
		{
			name:       "markdown dividers filtered",
			reviewerID: 1,
			input: `---
Valid finding
---`,
			want: []string{"Valid finding"},
		},
		{
			name:       "code blocks filtered",
			reviewerID: 1,
			input:      "```\nValid finding\n```go",
			want:       []string{"Valid finding"},
		},
		{
			name:       "no issues messages filtered",
			reviewerID: 1,
			input: `No issues found
Valid finding
no issues in this file
There are no issues`,
			want: []string{"Valid finding"},
		},
		{
			name:       "no findings messages filtered",
			reviewerID: 1,
			input: `No findings detected
Valid finding
no findings here`,
			want: []string{"Valid finding"},
		},
		{
			name:       "looks good messages filtered",
			reviewerID: 1,
			input: `Looks good
Valid finding
looks good to me
LOOKS GOOD`,
			want: []string{"Valid finding"},
		},
		{
			name:       "code looks clean filtered",
			reviewerID: 1,
			input: `The code looks clean
Valid finding`,
			want: []string{"Valid finding"},
		},
		{
			name:       "no problems filtered",
			reviewerID: 1,
			input: `No problems found
Valid finding`,
			want: []string{"Valid finding"},
		},
		{
			name:       "review complete filtered",
			reviewerID: 1,
			input: `Valid finding
Review complete`,
			want: []string{"Valid finding"},
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
			name:       "whitespace trimmed",
			reviewerID: 1,
			input:      "   Finding with surrounding whitespace   ",
			want:       []string{"Finding with surrounding whitespace"},
		},
		{
			name:       "case insensitive filtering",
			reviewerID: 1,
			input: `NO ISSUES FOUND
Valid finding
No Issues Found
LOOKS GOOD
looks GOOD`,
			want: []string{"Valid finding"},
		},
		{
			name:       "findings with special characters",
			reviewerID: 3,
			input:      `Finding with "quotes" and special chars: <>!@#$%`,
			want:       []string{`Finding with "quotes" and special chars: <>!@#$%`},
		},
		{
			name:       "multiline context",
			reviewerID: 1,
			input: `security/auth.go:100: Missing input validation - user input passed directly to database query without sanitization
performance/cache.go:50: Inefficient loop - O(n^2) complexity could be reduced to O(n)`,
			want: []string{
				"security/auth.go:100: Missing input validation - user input passed directly to database query without sanitization",
				"performance/cache.go:50: Inefficient loop - O(n^2) complexity could be reduced to O(n)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewClaudeOutputParser(tt.reviewerID)
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

func TestClaudeOutputParser_ParseErrors(t *testing.T) {
	// ClaudeOutputParser parses plain text, so it doesn't have parse errors
	tests := []struct {
		name            string
		input           string
		wantParseErrors int
	}{
		{
			name:            "valid plain text",
			input:           "Some finding text",
			wantParseErrors: 0,
		},
		{
			name:            "empty input",
			input:           "",
			wantParseErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewClaudeOutputParser(1)
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

func TestNewClaudeOutputParser(t *testing.T) {
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
			parser := NewClaudeOutputParser(tt.reviewerID)
			if parser == nil {
				t.Fatal("NewClaudeOutputParser() returned nil")
			}
			if parser.reviewerID != tt.reviewerID {
				t.Errorf("reviewerID = %d, want %d", parser.reviewerID, tt.reviewerID)
			}
		})
	}
}

func TestClaudeOutputParserInterface(t *testing.T) {
	// Verify that ClaudeOutputParser implements the ReviewParser interface
	var _ ReviewParser = (*ClaudeOutputParser)(nil)
}

func BenchmarkClaudeOutputParser_ReadFinding(b *testing.B) {
	input := `auth/login.go:45: SQL injection vulnerability
api/handler.go:123: Resource leak detected
utils/parser.go:67: Potential panic
`
	parser := NewClaudeOutputParser(1)

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

func BenchmarkClaudeOutputParser_WithFiltering(b *testing.B) {
	input := `# Comment line
auth/login.go:45: SQL injection vulnerability
No issues found
api/handler.go:123: Resource leak detected
Looks good
utils/parser.go:67: Potential panic
---
`
	parser := NewClaudeOutputParser(1)

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
