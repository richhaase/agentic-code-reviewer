package agent

import (
	"fmt"
	"os"
	"slices"
)

// agentRegistry holds the factory functions for a single agent.
type agentRegistry struct {
	newAgent         func() Agent
	newReviewParser  func(reviewerID int) ReviewParser
	newSummaryParser func() SummaryParser
}

// registry maps agent names to their factory functions.
// To add a new agent, add an entry here - no other changes needed.
var registry = map[string]agentRegistry{
	"codex": {
		newAgent:         func() Agent { return NewCodexAgent() },
		newReviewParser:  func(id int) ReviewParser { return NewCodexOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewCodexSummaryParser() },
	},
	"claude": {
		newAgent:         func() Agent { return NewClaudeAgent() },
		newReviewParser:  func(id int) ReviewParser { return NewClaudeOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewClaudeSummaryParser() },
	},
	"gemini": {
		newAgent:         func() Agent { return NewGeminiAgent() },
		newReviewParser:  func(id int) ReviewParser { return NewGeminiOutputParser(id) },
		newSummaryParser: func() SummaryParser { return NewGeminiSummaryParser() },
	},
}

// apiAgentConfig maps agent names to their API key configuration.
// When the corresponding environment variable is set, the factory
// creates an API-direct agent instead of shelling out to a CLI.
var apiAgentConfig = map[string]struct {
	keyEnvVar    string
	modelEnvVar  string
	defaultModel string
	newAgent     func(apiKey, model string) Agent
}{
	"claude": {
		keyEnvVar:    "ANTHROPIC_API_KEY",
		modelEnvVar:  "ACR_ANTHROPIC_MODEL",
		defaultModel: "claude-sonnet-4-6",
		newAgent:     func(k, m string) Agent { return NewAnthropicAPIAgent(k, m) },
	},
	"codex": {
		keyEnvVar:    "OPENAI_API_KEY",
		modelEnvVar:  "ACR_OPENAI_MODEL",
		defaultModel: "gpt-5.4",
		newAgent:     func(k, m string) Agent { return NewOpenAIAPIAgent(k, m) },
	},
	"gemini": {
		keyEnvVar:    "GEMINI_API_KEY",
		modelEnvVar:  "ACR_GOOGLE_MODEL",
		defaultModel: "gemini-3.0-flash",
		newAgent:     func(k, m string) Agent { return NewGoogleAPIAgent(k, m) },
	},
}

// SupportedAgents lists all valid agent names.
// Derived from the registry to stay in sync automatically.
var SupportedAgents = func() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}()

// DefaultAgent is the default agent used for reviews when none is specified.
const DefaultAgent = "codex"

// DefaultSummarizerAgent is the default agent used for summarization when none is specified.
const DefaultSummarizerAgent = "codex"

// NewAgent creates an Agent by name.
// Supported agents: codex, claude, gemini
func NewAgent(name string) (Agent, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, supported: %v", name, SupportedAgents)
	}

	// Check for API key — if present, use API-direct agent
	if apiCfg, ok := apiAgentConfig[name]; ok {
		if apiKey := os.Getenv(apiCfg.keyEnvVar); apiKey != "" {
			model := os.Getenv(apiCfg.modelEnvVar)
			if model == "" {
				model = apiCfg.defaultModel
			}
			return apiCfg.newAgent(apiKey, model), nil
		}
	}

	// Fall back to CLI agent
	return reg.newAgent(), nil
}

// NewReviewParser creates a ReviewParser for the given agent name.
// The parser matches the output format of the corresponding agent.
func NewReviewParser(agentName string, reviewerID int) (ReviewParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no parser available", agentName)
	}
	return reg.newReviewParser(reviewerID), nil
}

// NewSummaryParser creates a SummaryParser for the given agent name.
// The parser matches the summary output format of the corresponding agent.
func NewSummaryParser(agentName string) (SummaryParser, error) {
	reg, ok := registry[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, no summary parser available", agentName)
	}
	return reg.newSummaryParser(), nil
}
