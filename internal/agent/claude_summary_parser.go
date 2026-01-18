package agent

import (
	"encoding/json"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// claudeSummaryWrapper represents the JSON wrapper Claude outputs with --output-format json.
// The actual structured output is in the StructuredOutput field when using --json-schema.
type claudeSummaryWrapper struct {
	StructuredOutput domain.GroupedFindings `json:"structured_output"`
}

// ClaudeSummaryParser parses summary output from the Claude CLI.
// Claude wraps the output in a metadata object with structured_output field.
type ClaudeSummaryParser struct{}

// NewClaudeSummaryParser creates a new ClaudeSummaryParser.
func NewClaudeSummaryParser() *ClaudeSummaryParser {
	return &ClaudeSummaryParser{}
}

// Parse parses the summary output and returns grouped findings.
// Claude wraps output in a metadata object; this extracts the structured_output field.
func (p *ClaudeSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	var wrapper claudeSummaryWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.StructuredOutput, nil
}
