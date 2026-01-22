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
// When Claude runs in agent mode with tools, output includes exploration
// commentary and tool invocations. These are filtered to extract only
// actual findings in the format: file:line: description
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

		// Skip agent exploration commentary (tool-use mode)
		// These are lines Claude outputs while exploring but aren't findings
		if strings.HasPrefix(line, "I'll ") ||
			strings.HasPrefix(line, "I will ") ||
			strings.HasPrefix(line, "Let me ") ||
			strings.HasPrefix(line, "Now ") ||
			strings.HasPrefix(line, "First") ||
			strings.HasPrefix(line, "Next") ||
			strings.HasPrefix(line, "Looking at") ||
			strings.HasPrefix(line, "Examining") ||
			strings.HasPrefix(line, "Checking") ||
			strings.HasPrefix(line, "Reading") ||
			strings.HasPrefix(line, "Running") ||
			strings.HasPrefix(line, "The ") ||
			strings.HasPrefix(line, "This ") ||
			strings.HasPrefix(line, "Here") ||
			strings.HasPrefix(line, "Based on") ||
			strings.HasPrefix(line, "After") ||
			strings.HasPrefix(line, "**") ||
			strings.HasPrefix(line, "- ") && !strings.Contains(line, ".go:") && !strings.Contains(line, ".py:") {
			continue
		}

		// Skip tool output markers and content
		if strings.Contains(lower, "$ git") ||
			strings.Contains(lower, "$ cd") ||
			strings.Contains(line, "diff --git") ||
			strings.HasPrefix(line, "+") && !strings.Contains(line, ":") ||
			strings.HasPrefix(line, "-") && !strings.Contains(line, ":") ||
			strings.HasPrefix(line, "@@") {
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
