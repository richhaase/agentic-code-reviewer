package agent

import (
	"encoding/json"
	"fmt"

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
	if err := validateGroupedFindingsShape([]byte(text)); err != nil {
		return nil, err
	}
	if grouped.Info == nil {
		grouped.Info = []domain.FindingGroup{}
	}
	return &grouped, nil
}

func validateGroupedFindingsShape(data []byte) error {
	var shape struct {
		Findings *[]domain.FindingGroup `json:"findings"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return err
	}
	if shape.Findings == nil {
		return fmt.Errorf(`summary JSON missing required "findings" array`)
	}
	return nil
}
