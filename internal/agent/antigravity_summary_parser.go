package agent

import (
	"encoding/json"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// Compile-time interface check
var _ SummaryParser = (*AntigravitySummaryParser)(nil)

// AntigravitySummaryParser parses summary output from the Antigravity CLI.
// agy --print emits the model response directly, so summary output is expected
// to be JSON text, optionally wrapped in markdown fences or surrounding prose.
type AntigravitySummaryParser struct{}

// NewAntigravitySummaryParser creates a new AntigravitySummaryParser.
func NewAntigravitySummaryParser() *AntigravitySummaryParser {
	return &AntigravitySummaryParser{}
}

// ExtractText extracts the raw JSON response text from agy output.
func (p *AntigravitySummaryParser) ExtractText(data []byte) (string, error) {
	return ExtractJSON(string(data))
}

// Parse parses the summary output and returns grouped findings.
func (p *AntigravitySummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
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
