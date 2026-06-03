package agent

import (
	"encoding/json"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// geminiSummaryWrapper represents the JSON wrapper Gemini outputs with -o json.
// The response field contains a JSON string that must be parsed separately.
type geminiSummaryWrapper struct {
	Response string `json:"response"`
}

// GeminiSummaryParser parses summary output from the Gemini CLI.
// Gemini wraps output with a response field containing a JSON string,
// which may also be wrapped in markdown code fences.
type GeminiSummaryParser struct{}

// NewGeminiSummaryParser creates a new GeminiSummaryParser.
func NewGeminiSummaryParser() *GeminiSummaryParser {
	return &GeminiSummaryParser{}
}

// ExtractText extracts the raw response text from gemini JSON output.
// Returns the response field with markdown code fences stripped.
func (p *GeminiSummaryParser) ExtractText(data []byte) (string, error) {
	var wrapper geminiSummaryWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return "", fmt.Errorf("failed to parse gemini output: %w", err)
	}

	if wrapper.Response == "" {
		return "", fmt.Errorf("gemini output has empty response field")
	}

	return ExtractJSON(wrapper.Response)
}

// Parse parses the summary output and returns grouped findings.
// Gemini wraps output; response field is a JSON string requiring double-parse.
// Response may also contain markdown code fences that need stripping.
func (p *GeminiSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	text, err := p.ExtractText(data)
	if err != nil {
		return nil, err
	}

	var grouped domain.GroupedFindings
	if err := json.Unmarshal([]byte(text), &grouped); err != nil {
		return nil, err
	}
	return &grouped, nil
}
