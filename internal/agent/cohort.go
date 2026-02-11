// Package agent provides LLM agent abstractions.
package agent

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ParseAgentNames splits a comma-separated agent string into a slice of names.
// Handles whitespace trimming. Returns default agent if input is empty.
func ParseAgentNames(input string) []string {
	if input == "" {
		return []string{DefaultAgent}
	}

	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			result = append(result, name)
		}
	}

	if len(result) == 0 {
		return []string{DefaultAgent}
	}

	return result
}

// ValidateAgentNames checks that all agent names are supported.
// Returns an error listing unsupported agents.
func ValidateAgentNames(names []string) error {
	var invalid []string
	for _, name := range names {
		if !slices.Contains(SupportedAgents, name) {
			invalid = append(invalid, name)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("unsupported agent(s): %s (supported: %v)",
			strings.Join(invalid, ", "), SupportedAgents)
	}

	return nil
}

// CreateAgents creates Agent instances for the given names.
// Validates all agent CLIs are available (fail fast).
func CreateAgents(names []string) ([]Agent, error) {
	agents := make([]Agent, 0, len(names))
	seen := make(map[string]Agent)

	for _, name := range names {
		// Reuse existing agent instance if same name appears multiple times
		if existing, ok := seen[name]; ok {
			agents = append(agents, existing)
			continue
		}

		agent, err := NewAgent(name)
		if err != nil {
			return nil, err
		}

		if err := agent.IsAvailable(); err != nil {
			return nil, fmt.Errorf("%s CLI not found: %w", name, err)
		}

		seen[name] = agent
		agents = append(agents, agent)
	}

	return agents, nil
}

// AgentsNeedDiff returns true if any agent in the list requires a pre-computed diff.
// Codex has built-in diff via --base and doesn't need one.
func AgentsNeedDiff(agents []Agent) bool {
	for _, a := range agents {
		if a.Name() != "codex" {
			return true
		}
	}
	return false
}

// FormatDistribution returns a human-readable distribution summary.
// Example: "2×codex, 2×claude, 1×gemini" for 5 reviewers with 3 agent types.
func FormatDistribution(agents []Agent, totalReviewers int) string {
	if len(agents) == 0 {
		return ""
	}

	if len(agents) == 1 {
		return agents[0].Name()
	}

	// Count how many times each agent will be used
	counts := make(map[string]int)
	for i := range totalReviewers {
		agent := agents[i%len(agents)]
		counts[agent.Name()]++
	}

	// Collect unique agent names and sort for consistent output
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Format as "N×agent"
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d×%s", counts[name], name))
	}

	return strings.Join(parts, ", ")
}

// AgentForReviewer returns the agent for a given reviewer ID using round-robin.
// Reviewer IDs are 1-based. Returns nil if agents slice is empty or reviewerID < 1.
func AgentForReviewer(agents []Agent, reviewerID int) Agent {
	if len(agents) == 0 || reviewerID < 1 {
		return nil
	}
	return agents[(reviewerID-1)%len(agents)]
}
