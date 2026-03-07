package agent

import (
	"slices"
	"testing"
)

func TestNewAgent(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		wantName  string
		wantErr   bool
	}{
		{
			name:      "codex agent",
			agentName: "codex",
			wantName:  "codex",
			wantErr:   false,
		},
		{
			name:      "claude agent",
			agentName: "claude",
			wantName:  "claude",
			wantErr:   false,
		},
		{
			name:      "gemini agent",
			agentName: "gemini",
			wantName:  "gemini",
			wantErr:   false,
		},
		{
			name:      "unknown agent",
			agentName: "unknown",
			wantErr:   true,
		},
		{
			name:      "empty agent name",
			agentName: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := NewAgent(tt.agentName)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agent == nil {
				t.Fatal("expected agent, got nil")
			}
			if got := agent.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestNewReviewParser(t *testing.T) {
	tests := []struct {
		name       string
		agentName  string
		reviewerID int
		wantErr    bool
	}{
		{
			name:       "codex parser",
			agentName:  "codex",
			reviewerID: 1,
			wantErr:    false,
		},
		{
			name:       "claude parser",
			agentName:  "claude",
			reviewerID: 2,
			wantErr:    false,
		},
		{
			name:       "gemini parser",
			agentName:  "gemini",
			reviewerID: 3,
			wantErr:    false,
		},
		{
			name:       "unknown agent parser",
			agentName:  "unknown",
			reviewerID: 1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewReviewParser(tt.agentName, tt.reviewerID)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parser == nil {
				t.Fatal("expected parser, got nil")
			}
		})
	}
}

func TestSupportedAgents(t *testing.T) {
	expected := []string{"codex", "claude", "gemini"}
	if len(SupportedAgents) != len(expected) {
		t.Errorf("SupportedAgents has %d elements, want %d", len(SupportedAgents), len(expected))
	}

	for _, name := range expected {
		if !slices.Contains(SupportedAgents, name) {
			t.Errorf("SupportedAgents missing %q", name)
		}
	}
}

func TestDefaultAgent(t *testing.T) {
	if DefaultAgent != "codex" {
		t.Errorf("DefaultAgent = %q, want %q", DefaultAgent, "codex")
	}
}

func TestNewAgent_PrefersAPIKeyOverCLI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")
	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := agent.(*AnthropicAPIAgent); !ok {
		t.Errorf("expected *AnthropicAPIAgent, got %T", agent)
	}
}

func TestNewAgent_FallsBackToCLI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := agent.(*ClaudeAgent); !ok {
		t.Errorf("expected *ClaudeAgent, got %T", agent)
	}
}

func TestNewAgent_OpenAIAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	agent, err := NewAgent("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := agent.(*OpenAIAPIAgent); !ok {
		t.Errorf("expected *OpenAIAPIAgent, got %T", agent)
	}
}

func TestNewAgent_GeminiAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	agent, err := NewAgent("gemini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := agent.(*GoogleAPIAgent); !ok {
		t.Errorf("expected *GoogleAPIAgent, got %T", agent)
	}
}

func TestNewAgent_CustomModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")
	t.Setenv("ACR_ANTHROPIC_MODEL", "claude-opus-4-6")
	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	apiAgent, ok := agent.(*AnthropicAPIAgent)
	if !ok {
		t.Fatalf("expected *AnthropicAPIAgent, got %T", agent)
	}
	if apiAgent.model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", apiAgent.model, "claude-opus-4-6")
	}
}
