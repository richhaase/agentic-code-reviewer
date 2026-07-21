package agent

import (
	"fmt"
	"slices"
)

type agentRegistry struct {
	newAgent         func(model string) Agent
	newReviewParser  func(reviewerID int) ReviewParser
	newSummaryParser func() SummaryParser
}

var registry = map[string]agentRegistry{
	"agy": {
		newAgent:         func(model string) Agent { return NewAntigravityAgent(model) },
		newReviewParser:  func(id int) ReviewParser { return NewAntigravityOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewAntigravitySummaryParser() },
	},
	"codex": {
		newAgent:         func(model string) Agent { return NewCodexAgent(model) },
		newReviewParser:  func(id int) ReviewParser { return NewCodexOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewCodexSummaryParser() },
	},
	"claude": {
		newAgent:         func(model string) Agent { return NewClaudeAgent(model) },
		newReviewParser:  func(id int) ReviewParser { return NewClaudeOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewClaudeSummaryParser() },
	},
	"gemini": {
		newAgent:         func(model string) Agent { return NewGeminiAgent(model) },
		newReviewParser:  func(id int) ReviewParser { return NewGeminiOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewGeminiSummaryParser() },
	},
}

var SupportedAgents = func() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}()

const DefaultAgent = "codex"

const DefaultSummarizerAgent = "codex"

func NewAgent(name string) (Agent, error) {
	return NewAgentWithModel(name, "")
}

func NewAgentWithModel(name, model string) (Agent, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, supported: %v", name, SupportedAgents)
	}
	return reg.newAgent(model), nil
}

func NewReviewParser(agentName string, reviewerID int) (ReviewParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no parser available", agentName)
	}
	return reg.newReviewParser(reviewerID), nil
}

func NewSummaryParser(agentName string) (SummaryParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no summary parser available", agentName)
	}
	return reg.newSummaryParser(), nil
}
