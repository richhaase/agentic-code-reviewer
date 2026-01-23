package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

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

// Parse parses the summary output and returns grouped findings.
// Handles JSONL event stream format from codex --json output.
// Events may be newline-separated or concatenated without separators.
func (p *CodexSummaryParser) Parse(data []byte) (*domain.GroupedFindings, error) {
	// Use json.Decoder to handle both newline-separated and concatenated JSON
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

		// Extract text from agent_message items.
		// We use the last message because codex may emit multiple agent_message events
		// during streaming, with the final one containing the complete response.
		if event.Item.Type == "agent_message" && event.Item.Text != "" {
			messageText = event.Item.Text
		}
	}

	// Fail on any decode error - don't silently proceed with partial output
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode codex JSONL output: %w", decodeErr)
	}

	if messageText == "" {
		return nil, fmt.Errorf("no agent_message found in codex output")
	}

	// Strip markdown code fences if present
	cleaned := StripMarkdownCodeFence(messageText)

	// Parse the JSON from the agent_message text field
	decoder = json.NewDecoder(strings.NewReader(cleaned))
	var grouped domain.GroupedFindings
	if err := decoder.Decode(&grouped); err != nil {
		return nil, fmt.Errorf("failed to parse JSON in agent_message: %w (content: %s)", err, truncate(cleaned, 200))
	}
	return &grouped, nil
}

// truncate returns the first n runes of s, or s if shorter.
// Uses rune counting to avoid splitting multi-byte UTF-8 characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "..."
}
