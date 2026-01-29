package agent

import (
	"bufio"
	"encoding/json"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// GeminiOutputParser parses JSON output from the gemini CLI.
// Gemini outputs a single JSON object (often pretty-printed across multiple lines)
// with a "response" field containing the review findings.
type GeminiOutputParser struct {
	reviewerID  int
	parseErrors int
	findings    []domain.Finding // buffered findings
	parsed      bool             // whether we've parsed the input yet
}

// NewGeminiOutputParser creates a new parser for gemini output.
func NewGeminiOutputParser(reviewerID int) *GeminiOutputParser {
	return &GeminiOutputParser{
		reviewerID: reviewerID,
	}
}

// ReadFinding reads and parses the next finding from the gemini output stream.
// On first call, reads and parses the entire JSON output, buffering all findings.
// Subsequent calls return buffered findings one at a time.
// Returns nil when no more findings are available.
func (p *GeminiOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	// Parse input on first call
	if !p.parsed {
		p.parsed = true
		if err := p.parseFullOutput(scanner); err != nil {
			return nil, err
		}
	}

	// Return next buffered finding
	if len(p.findings) > 0 {
		finding := p.findings[0]
		p.findings = p.findings[1:]
		return &finding, nil
	}

	return nil, nil
}

// parseFullOutput reads all scanner input, parses JSON, and extracts findings.
func (p *GeminiOutputParser) parseFullOutput(scanner *bufio.Scanner) error {
	// Read all input
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

	// Try to parse as JSON
	var jsonObj map[string]any
	if err := json.Unmarshal([]byte(fullOutput), &jsonObj); err != nil {
		// Not valid JSON - treat entire output as plain text finding
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

	// Extract response text from known fields
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

	// The response may contain the full review as a single text block
	// Return it as one finding (aggregation/summarization handles grouping)
	responseText = strings.TrimSpace(responseText)
	if responseText != "" && !IsNonFindingText(responseText) {
		p.findings = append(p.findings, domain.Finding{
			Text:       responseText,
			ReviewerID: p.reviewerID,
		})
	}

	return nil
}

// ParseErrors returns the number of recoverable parse errors encountered.
func (p *GeminiOutputParser) ParseErrors() int {
	return p.parseErrors
}
