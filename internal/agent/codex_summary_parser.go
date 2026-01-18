package agent

import (
	"encoding/json"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// CodexSummaryParser parses summary output from the Codex CLI.
// Codex returns raw JSON that can be parsed directly into GroupedFindings.
type CodexSummaryParser struct{}

// NewCodexSummaryParser creates a new CodexSummaryParser.
func NewCodexSummaryParser() *CodexSummaryParser {
	return &CodexSummaryParser{}
}

// Parse parses the summary output and returns grouped findings.
func (p *CodexSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	var grouped domain.GroupedFindings
	if err := json.Unmarshal(data, &grouped); err != nil {
		return nil, err
	}
	return &grouped, nil
}
