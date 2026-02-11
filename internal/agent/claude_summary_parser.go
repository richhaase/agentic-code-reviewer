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
// Prefers the result field (which contains the LLM's actual text response to the prompt)
// over structured_output (which is constrained by --json-schema to a fixed schema).
// This allows callers like the FP filter to get the response in whatever format
// the prompt requested, rather than being forced into the summarizer's schema.
func (p *ClaudeSummaryParser) ExtractText(data []byte) (string, error) {
	var wrapper claudeTextWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return "", fmt.Errorf("failed to parse claude output: %w", err)
	}

	// Prefer result field — it contains the LLM's actual response text,
	// which respects the caller's prompt format (not the hardcoded schema).
	// Use ExtractJSON to handle cases where Claude wraps JSON in prose text.
	if wrapper.Result != "" {
		return ExtractJSON(wrapper.Result), nil
	}

	// Fall back to structured_output if result is empty
	if wrapper.StructuredOutput != nil {
		raw := strings.TrimSpace(string(*wrapper.StructuredOutput))
		if raw != "null" && raw != "" {
			return raw, nil
		}
	}

	return "", fmt.Errorf("claude output has no result or structured_output field")
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
