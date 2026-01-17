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

func TestNewOutputParser(t *testing.T) {
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
			parser, err := NewOutputParser(tt.agentName, tt.reviewerID)
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
