package agent

import (
	"bufio"
	"strings"
	"testing"
)

func TestCodexOutputParser_ReadFinding(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		reviewerID int
		want       []string // Expected finding texts in order
	}{
		{
			name:       "single valid finding",
			reviewerID: 1,
			input:      `{"item": {"type": "agent_message", "text": "Missing error handling"}}`,
			want:       []string{"Missing error handling"},
		},
		{
			name:       "multiple valid findings",
			reviewerID: 2,
			input: `{"item": {"type": "agent_message", "text": "Finding 1"}}
{"item": {"type": "agent_message", "text": "Finding 2"}}
{"item": {"type": "agent_message", "text": "Finding 3"}}`,
			want: []string{"Finding 1", "Finding 2", "Finding 3"},
		},
		{
			name:       "findings with empty lines",
			reviewerID: 1,
			input: `{"item": {"type": "agent_message", "text": "Finding 1"}}

{"item": {"type": "agent_message", "text": "Finding 2"}}
`,
			want: []string{"Finding 1", "Finding 2"},
		},
		{
			name:       "mixed message types",
			reviewerID: 1,
			input: `{"item": {"type": "status", "text": "Starting review..."}}
{"item": {"type": "agent_message", "text": "Valid finding"}}
{"item": {"type": "debug", "text": "Debug info"}}
{"item": {"type": "agent_message", "text": "Another finding"}}`,
			want: []string{"Valid finding", "Another finding"},
		},
		{
			name:       "empty text ignored",
			reviewerID: 1,
			input: `{"item": {"type": "agent_message", "text": ""}}
{"item": {"type": "agent_message", "text": "Valid finding"}}
{"item": {"type": "agent_message", "text": ""}}`,
			want: []string{"Valid finding"},
		},
		{
			name:       "invalid JSON lines skipped",
			reviewerID: 1,
			input: `not valid json
{"item": {"type": "agent_message", "text": "Valid finding"}}
{malformed json
{"item": {"type": "agent_message", "text": "Another finding"}}`,
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
			input:      `{"item": {"type": "agent_message", "text": "Finding with \"quotes\" and\nnewlines"}}`,
			want:       []string{"Finding with \"quotes\" and\nnewlines"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewCodexOutputParser(tt.reviewerID)
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

func TestCodexOutputParser_ParseErrors(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantFindings    int
		wantParseErrors int
	}{
		{
			name:            "no parse errors",
			input:           `{"item": {"type": "agent_message", "text": "Valid finding"}}`,
			wantFindings:    1,
			wantParseErrors: 0,
		},
		{
			name: "single parse error",
			input: `not valid json
{"item": {"type": "agent_message", "text": "Valid finding"}}`,
			wantFindings:    1,
			wantParseErrors: 1,
		},
		{
			name: "multiple parse errors",
			input: `not valid json
{"item": {"type": "agent_message", "text": "Finding 1"}}
{malformed json
also invalid
{"item": {"type": "agent_message", "text": "Finding 2"}}`,
			wantFindings:    2,
			wantParseErrors: 3,
		},
		{
			name:            "all lines invalid",
			input:           "invalid\nalso invalid\nstill invalid",
			wantFindings:    0,
			wantParseErrors: 3,
		},
		{
			name:            "empty input no errors",
			input:           "",
			wantFindings:    0,
			wantParseErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewCodexOutputParser(1)
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			ConfigureScanner(scanner)

			findingCount := 0
			for {
				finding, err := parser.ReadFinding(scanner)
				if err != nil {
					t.Fatalf("ReadFinding() error = %v", err)
				}
				if finding == nil {
					break
				}
				findingCount++
			}

			if findingCount != tt.wantFindings {
				t.Errorf("got %d findings, want %d", findingCount, tt.wantFindings)
			}

			if parser.ParseErrors() != tt.wantParseErrors {
				t.Errorf("ParseErrors() = %d, want %d", parser.ParseErrors(), tt.wantParseErrors)
			}
		})
	}
}

func TestNewCodexOutputParser(t *testing.T) {
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
			parser := NewCodexOutputParser(tt.reviewerID)
			if parser == nil {
				t.Fatal("NewCodexOutputParser() returned nil")
			}
			if parser.reviewerID != tt.reviewerID {
				t.Errorf("reviewerID = %d, want %d", parser.reviewerID, tt.reviewerID)
			}
		})
	}
}

func TestConfigureScanner(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("test"))
	ConfigureScanner(scanner)

	// Test that scanner can handle a large line
	largeInput := strings.Repeat("x", 1024*1024) // 1MB line
	scanner = bufio.NewScanner(strings.NewReader(largeInput))
	ConfigureScanner(scanner)

	if !scanner.Scan() {
		t.Errorf("Scanner failed to scan 1MB line: %v", scanner.Err())
	}

	got := scanner.Text()
	if len(got) != len(largeInput) {
		t.Errorf("Scanned text length = %d, want %d", len(got), len(largeInput))
	}
}

func BenchmarkCodexOutputParser_ReadFinding(b *testing.B) {
	input := `{"item": {"type": "agent_message", "text": "Performance issue detected"}}
{"item": {"type": "agent_message", "text": "Memory leak found"}}
{"item": {"type": "agent_message", "text": "Security vulnerability"}}
`
	parser := NewCodexOutputParser(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
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
