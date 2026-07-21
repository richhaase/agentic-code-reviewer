package agent

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

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

func CreateAgents(names []string) ([]Agent, error) {
	return CreateAgentsWithModel(names, "")
}

func CreateAgentsWithModel(names []string, model string) ([]Agent, error) {
	agents := make([]Agent, 0, len(names))
	seen := make(map[string]Agent)

	for _, name := range names {

		if existing, ok := seen[name]; ok {
			agents = append(agents, existing)
			continue
		}

		agent, err := NewAgentWithModel(name, model)
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

func AgentsNeedDiff(agents []Agent) bool {
	for _, a := range agents {
		if AgentNeedsDiff(a.Name()) {
			return true
		}
	}
	return false
}

func AgentNeedsDiff(name string) bool {
	switch name {
	case "codex":
		return false
	case "agy", "claude", "gemini":
		return true
	default:
		return true
	}
}

func FormatDistribution(agents []Agent, totalReviewers int) string {
	if len(agents) == 0 {
		return ""
	}

	if len(agents) == 1 {
		return agents[0].Name()
	}

	counts := make(map[string]int)
	for i := range totalReviewers {
		agent := agents[i%len(agents)]
		counts[agent.Name()]++
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d×%s", counts[name], name))
	}

	return strings.Join(parts, ", ")
}

func AgentForReviewer(agents []Agent, reviewerID int) Agent {
	if len(agents) == 0 || reviewerID < 1 {
		return nil
	}
	return agents[(reviewerID-1)%len(agents)]
}
