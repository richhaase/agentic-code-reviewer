package agent

import (
	"bufio"
	"encoding/json"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type GeminiOutputParser struct {
	reviewerID  int
	parseErrors int
	findings    []domain.Finding
	parsed      bool
}

func NewGeminiOutputParser(reviewerID int) *GeminiOutputParser {
	return &GeminiOutputParser{
		reviewerID: reviewerID,
	}
}

func (p *GeminiOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {

	if !p.parsed {
		p.parsed = true
		if err := p.parseFullOutput(scanner); err != nil {
			return nil, err
		}
	}

	if len(p.findings) > 0 {
		finding := p.findings[0]
		p.findings = p.findings[1:]
		return &finding, nil
	}

	return nil, nil
}

func (p *GeminiOutputParser) parseFullOutput(scanner *bufio.Scanner) error {

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	fullOutput := strings.Join(lines, "\n")
	if strings.TrimSpace(fullOutput) == "" {
		return nil
	}

	var jsonObj map[string]any
	if err := json.Unmarshal([]byte(fullOutput), &jsonObj); err != nil {

		p.parseErrors++
		text := strings.TrimSpace(fullOutput)
		if text != "" && !IsNonFindingText(text) {
			p.findings = append(p.findings, domain.Finding{
				Text:       text,
				ReviewerID: p.reviewerID,
			})
		}
		return nil
	}

	var responseText string
	for _, field := range []string{"response", "text", "message", "content", "finding"} {
		if val, ok := jsonObj[field]; ok {
			if str, ok := val.(string); ok && str != "" {
				responseText = str
				break
			}
		}
	}

	if responseText == "" {
		return nil
	}

	responseText = strings.TrimSpace(responseText)
	if responseText != "" && !IsNonFindingText(responseText) {
		p.findings = append(p.findings, domain.Finding{
			Text:       responseText,
			ReviewerID: p.reviewerID,
		})
	}

	return nil
}

func (p *GeminiOutputParser) ParseErrors() int {
	return p.parseErrors
}
