package agent

import "fmt"

// SupportedAgents lists all valid agent names.
var SupportedAgents = []string{"codex", "claude", "gemini"}

// DefaultAgent is the default agent used for reviews when none is specified.
const DefaultAgent = "codex"

// DefaultSummarizerAgent is the default agent used for summarization when none is specified.
const DefaultSummarizerAgent = "codex"

// NewAgent creates an Agent by name.
// Supported agents: codex, claude, gemini
func NewAgent(name string) (Agent, error) {
	switch name {
	case "codex":
		return NewCodexAgent(), nil
	case "claude":
		return NewClaudeAgent(), nil
	case "gemini":
		return NewGeminiAgent(), nil
	default:
		return nil, fmt.Errorf("unknown agent %q, supported: codex, claude, gemini", name)
	}
}

// NewReviewParser creates a ReviewParser for the given agent name.
// The parser matches the output format of the corresponding agent.
func NewReviewParser(agentName string, reviewerID int) (ReviewParser, error) {
	switch agentName {
	case "codex":
		return NewCodexOutputParser(reviewerID), nil
	case "claude":
		return NewClaudeOutputParser(reviewerID), nil
	case "gemini":
		return NewGeminiOutputParser(reviewerID), nil
	default:
		return nil, fmt.Errorf("unknown agent %q, no parser available", agentName)
	}
}

// NewSummaryParser creates a SummaryParser for the given agent name.
// The parser matches the summary output format of the corresponding agent.
func NewSummaryParser(agentName string) (SummaryParser, error) {
	switch agentName {
	case "codex":
		return NewCodexSummaryParser(), nil
	case "claude":
		return NewClaudeSummaryParser(), nil
	case "gemini":
		return NewGeminiSummaryParser(), nil
	default:
		return nil, fmt.Errorf("unknown agent %q, no summary parser available", agentName)
	}
}
