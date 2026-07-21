package agent

import (
	"bufio"
	"encoding/json"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

const (
	scannerInitialBuffer = 64 * 1024

	scannerMaxLineSize = 10 * 1024 * 1024
)

type CodexOutputParser struct {
	reviewerID  int
	lineNum     int
	parseErrors int
}

func NewCodexOutputParser(reviewerID int) *CodexOutputParser {
	return &CodexOutputParser{
		reviewerID: reviewerID,
	}
}

func (p *CodexOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	for scanner.Scan() {
		p.lineNum++
		line := scanner.Text()

		if line == "" {
			continue
		}

		var event struct {
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}

		if err := json.Unmarshal([]byte(line), &event); err != nil {
			p.parseErrors++
			return nil, &RecoverableParseError{
				Line:    p.lineNum,
				Message: fmt.Sprintf("invalid JSON: %v", err),
			}
		}

		if event.Item.Type == "agent_message" && event.Item.Text != "" &&
			!IsNonFindingText(event.Item.Text) {
			return &domain.Finding{
				Text:       event.Item.Text,
				ReviewerID: p.reviewerID,
			}, nil
		}

	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *CodexOutputParser) ParseErrors() int {
	return p.parseErrors
}

func ConfigureScanner(scanner *bufio.Scanner) {
	scanner.Buffer(make([]byte, 0, scannerInitialBuffer), scannerMaxLineSize)
}
