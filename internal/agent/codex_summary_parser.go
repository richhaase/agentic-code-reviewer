package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type CodexSummaryParser struct{}

func NewCodexSummaryParser() *CodexSummaryParser {
	return &CodexSummaryParser{}
}

type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

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

	return ExtractJSON(messageText)
}

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
