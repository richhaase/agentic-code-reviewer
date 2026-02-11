package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// codexEvent represents a JSONL event from codex --json output.
type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

// ExtractText extracts the raw response text from codex JSONL output.
// Decodes the event stream and returns the text from the last item.completed agent_message,
// with markdown code fences stripped.
func (p *CodexSummaryParser) ExtractText(data []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var messageText string
	var decodeErr error

	for {
		var event codexEvent
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			decodeErr = err
			break
		}

		// Extract text from completed agent_message items.
		// We check both event.Type and item.Type to ensure we only process final
		// messages, not partial/streaming events. We use the last message because
		// codex may emit multiple during streaming.
		if event.Type == "item.completed" && event.Item.Type == "agent_message" && event.Item.Text != "" {
			messageText = event.Item.Text
		}
	}

	if decodeErr != nil {
		return "", fmt.Errorf("failed to decode codex JSONL output: %w", decodeErr)
	}

	if messageText == "" {
		preview := truncate(string(data), 200)
		return "", fmt.Errorf("no agent_message found in codex output (received: %s)", preview)
	}

	return StripMarkdownCodeFence(messageText), nil
}

// Parse parses the summary output and returns grouped findings.
// Handles JSONL event stream format from codex --json output.
// Events may be newline-separated or concatenated without separators.
func (p *CodexSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	cleaned, err := p.ExtractText(data)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(strings.NewReader(cleaned))
	var grouped domain.GroupedFindings
	if err := decoder.Decode(&grouped); err != nil {
		return nil, fmt.Errorf("failed to parse JSON in agent_message: %w (content: %s)", err, truncate(cleaned, 200))
	}
	return &grouped, nil
}

// truncate returns the first n runes of s, or s if shorter.
// Iterates runes incrementally to avoid O(N) allocation for long strings.
func truncate(s string, n int) string {
	count := 0
	for i := range s {
		if count >= n {
			return s[:i] + "..."
		}
		count++
	}
	return s
}
