package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// claudeTextWrapper is used by ExtractText to get structured_output as raw JSON.
type claudeTextWrapper struct {
	Result           string           `json:"result"`
	StructuredOutput *json.RawMessage `json:"structured_output"`
}

// ClaudeSummaryParser parses summary output from the Claude CLI.
// Claude wraps the output in a metadata object with structured_output field.
type ClaudeSummaryParser struct{}

// NewClaudeSummaryParser creates a new ClaudeSummaryParser.
func NewClaudeSummaryParser() *ClaudeSummaryParser {
	return &ClaudeSummaryParser{}
}

// ExtractText extracts the raw response text from claude JSON output.
// Returns the structured_output as raw JSON if present, otherwise falls back to result field.
func (p *ClaudeSummaryParser) ExtractText(data []byte) (string, error) {
	var wrapper claudeTextWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return "", fmt.Errorf("failed to parse claude output: %w", err)
	}

	if wrapper.StructuredOutput != nil {
		return strings.TrimSpace(string(*wrapper.StructuredOutput)), nil
	}

	if wrapper.Result != "" {
		return StripMarkdownCodeFence(wrapper.Result), nil
	}

	return "", fmt.Errorf("claude output has no structured_output or result field")
}

// Parse parses the summary output and returns grouped findings.
// Claude wraps output in a metadata object; this extracts the structured_output field.
// Returns an error if structured_output is missing (indicates CLI error or format change).
func (p *ClaudeSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
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
