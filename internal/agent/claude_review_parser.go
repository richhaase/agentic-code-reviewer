package agent

import (
	"bufio"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type ClaudeOutputParser struct {
	reviewerID  int
	parseErrors int
}

func NewClaudeOutputParser(reviewerID int) *ClaudeOutputParser {
	return &ClaudeOutputParser{
		reviewerID: reviewerID,
	}
}

func (p *ClaudeOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "```") ||
			IsNonFindingText(line) {
			continue
		}

		return &domain.Finding{
			Text:       line,
			ReviewerID: p.reviewerID,
		}, nil
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *ClaudeOutputParser) ParseErrors() int {
	return p.parseErrors
}
