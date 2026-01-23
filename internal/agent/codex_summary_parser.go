package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// CodexSummaryParser parses summary output from the Codex CLI.
// With --json flag, Codex outputs JSONL format with events like:
//
//	{"type":"item.completed","item":{"type":"agent_message","text":"..."}}
//
// The actual summary JSON is in the text field of agent_message items.
type CodexSummaryParser struct{}

// NewCodexSummaryParser creates a new CodexSummaryParser.
func NewCodexSummaryParser() *CodexSummaryParser {
	return &CodexSummaryParser{}
}

// Parse parses the summary output and returns grouped findings.
// Handles JSONL event stream format from codex --json output.
func (p *CodexSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	// Parse JSONL and extract agent_message text
	lines := strings.Split(string(data), "\n")
	var messageText string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event struct {
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}

		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip lines that don't match the expected format
			continue
		}

		// Extract text from agent_message items
		if event.Item.Type == "agent_message" && event.Item.Text != "" {
			messageText = event.Item.Text
		}
	}

	if messageText == "" {
		return nil, fmt.Errorf("no agent_message found in codex output")
	}

	// Strip markdown code fences if present
	cleaned := StripMarkdownCodeFence(messageText)

	var grouped domain.GroupedFindings
	if err := json.Unmarshal([]byte(cleaned), &grouped); err != nil {
		return nil, err
	}
	return &grouped, nil
}
