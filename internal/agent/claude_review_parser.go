package agent

import (
	"bufio"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// ClaudeOutputParser parses text output from the claude CLI.
type ClaudeOutputParser struct {
	reviewerID  int
	parseErrors int
}

// NewClaudeOutputParser creates a new parser for claude output.
func NewClaudeOutputParser(reviewerID int) *ClaudeOutputParser {
	return &ClaudeOutputParser{
		reviewerID: reviewerID,
	}
}

// ReadFinding reads and parses the next finding from the claude output stream.
// Claude outputs plain text findings, one per line.
// Lines are filtered to exclude common non-finding content.
//
// Returns nil when no more findings are available.
func (p *ClaudeOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip common non-finding lines
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "```") ||
			strings.Contains(lower, "no issues") ||
			strings.Contains(lower, "no findings") ||
			strings.Contains(lower, "looks good") ||
			strings.Contains(lower, "code looks clean") ||
			strings.Contains(lower, "no problems") ||
			strings.Contains(lower, "review complete") {
			continue
		}

		return &domain.Finding{
			Text:       line,
			ReviewerID: p.reviewerID,
		}, nil
	}

	// Check for scanner error
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// No more findings
	return nil, nil
}

// ParseErrors returns the number of recoverable parse errors encountered.
// ClaudeOutputParser parses plain text, so parse errors don't occur.
func (p *ClaudeOutputParser) ParseErrors() int {
	return p.parseErrors
}
