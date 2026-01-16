package agent

import (
	"bufio"
	"encoding/json"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// GeminiOutputParser parses JSON output from the gemini CLI.
type GeminiOutputParser struct {
	reviewerID int
}

// NewGeminiOutputParser creates a new parser for gemini output.
func NewGeminiOutputParser(reviewerID int) *GeminiOutputParser {
	return &GeminiOutputParser{
		reviewerID: reviewerID,
	}
}

// ReadFinding reads and parses the next finding from the gemini output stream.
// Gemini outputs JSON format. This parser handles several common formats:
//   - {"text": "finding description"}
//   - {"message": "finding description"}
//   - {"content": "finding description"}
//   - Plain text lines (treated as findings)
//
// Returns nil when no more findings are available.
func (p *GeminiOutputParser) ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error) {
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse as JSON first
		var jsonObj map[string]any
		if err := json.Unmarshal([]byte(line), &jsonObj); err == nil {
			// Successfully parsed as JSON, extract text from common fields
			var text string
			for _, field := range []string{"text", "message", "content", "finding"} {
				if val, ok := jsonObj[field]; ok {
					if str, ok := val.(string); ok && str != "" {
						text = str
						break
					}
				}
			}

			if text != "" {
				return &domain.Finding{
					Text:       text,
					ReviewerID: p.reviewerID,
				}, nil
			}
			// Skip JSON objects without recognized text fields
			continue
		}

		// Not JSON, treat as plain text finding
		// Skip common non-finding lines
		if strings.HasPrefix(line, "#") ||
			strings.Contains(strings.ToLower(line), "no issues") ||
			strings.Contains(strings.ToLower(line), "looks good") {
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

// Close releases any resources held by the parser.
func (p *GeminiOutputParser) Close() error {
	// No resources to clean up for gemini parser
	return nil
}
