package agent

import (
	"encoding/json"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type geminiSummaryWrapper struct {
	Response string `json:"response"`
}

type GeminiSummaryParser struct{}

func NewGeminiSummaryParser() *GeminiSummaryParser {
	return &GeminiSummaryParser{}
}

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
