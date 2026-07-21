package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type claudeTextWrapper struct {
	Result           string           `json:"result"`
	StructuredOutput *json.RawMessage `json:"structured_output"`
}

type ClaudeSummaryParser struct{}

func NewClaudeSummaryParser() *ClaudeSummaryParser {
	return &ClaudeSummaryParser{}
}

func (p *ClaudeSummaryParser) ExtractText(data []byte) (string, error) {
	var wrapper claudeTextWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return "", fmt.Errorf("failed to parse claude output: %w", err)
	}

	if wrapper.Result != "" {
		return ExtractJSON(wrapper.Result)
	}

	if wrapper.StructuredOutput != nil {
		raw := strings.TrimSpace(string(*wrapper.StructuredOutput))
		if raw != "null" && raw != "" {
			return raw, nil
		}
	}

	return "", fmt.Errorf("claude output has no result or structured_output field")
}

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
