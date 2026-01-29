package agent

import (
	"context"
	"testing"
)

func TestParseAgentNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string returns default",
			input:    "",
			expected: []string{DefaultAgent},
		},
		{
			name:     "single agent",
			input:    "codex",
			expected: []string{"codex"},
		},
		{
			name:     "multiple agents comma-separated",
			input:    "codex,claude,gemini",
			expected: []string{"codex", "claude", "gemini"},
		},
		{
			name:     "multiple agents with spaces",
			input:    "codex, claude, gemini",
			expected: []string{"codex", "claude", "gemini"},
		},
		{
			name:     "extra whitespace trimmed",
			input:    "  codex  ,  claude  ,  gemini  ",
			expected: []string{"codex", "claude", "gemini"},
		},
		{
			name:     "empty parts ignored",
			input:    "codex,,claude",
			expected: []string{"codex", "claude"},
		},
		{
			name:     "only commas returns default",
			input:    ",,,",
			expected: []string{DefaultAgent},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseAgentNames(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d agents, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, name := range result {
				if name != tt.expected[i] {
					t.Errorf("agent[%d]: expected %q, got %q", i, tt.expected[i], name)
				}
			}
		})
	}
}

func TestValidateAgentNames(t *testing.T) {
	tests := []struct {
		name      string
		agents    []string
		expectErr bool
	}{
		{
			name:      "valid single agent",
			agents:    []string{"codex"},
			expectErr: false,
		},
		{
			name:      "valid multiple agents",
			agents:    []string{"codex", "claude", "gemini"},
			expectErr: false,
		},
		{
			name:      "invalid agent",
			agents:    []string{"invalid"},
			expectErr: true,
		},
		{
			name:      "mix of valid and invalid",
			agents:    []string{"codex", "invalid", "claude"},
			expectErr: true,
		},
		{
			name:      "empty list is valid",
			agents:    []string{},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentNames(tt.agents)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAgentForReviewer(t *testing.T) {
	// Create mock agents
	agents := []Agent{
		&mockAgent{name: "codex"},
		&mockAgent{name: "claude"},
		&mockAgent{name: "gemini"},
	}

	tests := []struct {
		reviewerID   int
		expectedName string
	}{
		{1, "codex"},  // (1-1) % 3 = 0 → codex
		{2, "claude"}, // (2-1) % 3 = 1 → claude
		{3, "gemini"}, // (3-1) % 3 = 2 → gemini
		{4, "codex"},  // (4-1) % 3 = 0 → codex (wrap around)
		{5, "claude"}, // (5-1) % 3 = 1 → claude
		{6, "gemini"}, // (6-1) % 3 = 2 → gemini
		{7, "codex"},  // (7-1) % 3 = 0 → codex
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			agent := AgentForReviewer(agents, tt.reviewerID)
			if agent.Name() != tt.expectedName {
				t.Errorf("reviewer %d: expected %s, got %s", tt.reviewerID, tt.expectedName, agent.Name())
			}
		})
	}
}

func TestAgentForReviewer_SingleAgent(t *testing.T) {
	agents := []Agent{&mockAgent{name: "codex"}}

	// All reviewers should get the same agent
	for i := 1; i <= 10; i++ {
		agent := AgentForReviewer(agents, i)
		if agent.Name() != "codex" {
			t.Errorf("reviewer %d: expected codex, got %s", i, agent.Name())
		}
	}
}

func TestAgentForReviewer_EmptySlice(t *testing.T) {
	agent := AgentForReviewer([]Agent{}, 1)
	if agent != nil {
		t.Errorf("expected nil for empty slice, got %v", agent)
	}
}

func TestAgentForReviewer_InvalidReviewerID(t *testing.T) {
	agents := []Agent{&mockAgent{name: "codex"}}

	tests := []struct {
		name       string
		reviewerID int
	}{
		{"zero", 0},
		{"negative", -1},
		{"very negative", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AgentForReviewer(agents, tt.reviewerID)
			if result != nil {
				t.Errorf("expected nil for reviewerID=%d, got %v", tt.reviewerID, result)
			}
		})
	}
}

func TestFormatDistribution(t *testing.T) {
	tests := []struct {
		name           string
		agents         []Agent
		totalReviewers int
		expected       string
	}{
		{
			name:           "single agent returns name only",
			agents:         []Agent{&mockAgent{name: "codex"}},
			totalReviewers: 5,
			expected:       "codex",
		},
		{
			name: "two agents even split",
			agents: []Agent{
				&mockAgent{name: "codex"},
				&mockAgent{name: "claude"},
			},
			totalReviewers: 4,
			expected:       "2×claude, 2×codex", // sorted alphabetically
		},
		{
			name: "three agents uneven split",
			agents: []Agent{
				&mockAgent{name: "codex"},
				&mockAgent{name: "claude"},
				&mockAgent{name: "gemini"},
			},
			totalReviewers: 5,
			expected:       "2×claude, 2×codex, 1×gemini", // sorted alphabetically
		},
		{
			name: "fewer reviewers than agents",
			agents: []Agent{
				&mockAgent{name: "codex"},
				&mockAgent{name: "claude"},
				&mockAgent{name: "gemini"},
			},
			totalReviewers: 2,
			expected:       "1×claude, 1×codex",
		},
		{
			name:           "empty agents",
			agents:         []Agent{},
			totalReviewers: 5,
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDistribution(tt.agents, tt.totalReviewers)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// mockAgent implements Agent interface for testing
type mockAgent struct {
	name string
}

func (m *mockAgent) Name() string {
	return m.name
}

func (m *mockAgent) IsAvailable() error {
	return nil
}

func (m *mockAgent) ExecuteReview(_ context.Context, _ *ReviewConfig) (*ExecutionResult, error) {
	return nil, nil
}

func (m *mockAgent) ExecuteSummary(_ context.Context, _ string, _ []byte) (*ExecutionResult, error) {
	return nil, nil
}
