package agent

import (
	"strings"
	"testing"
)

func TestDefaultClaudePrompt(t *testing.T) {
	if DefaultClaudePrompt == "" {
		t.Error("DefaultClaudePrompt should not be empty")
	}

	// Check for key elements that should be in a code review prompt
	requiredElements := []string{
		"code review",
		"bugs",
		"security",
		"performance",
		"finding",
		"file",
		"line",
	}

	lowerPrompt := strings.ToLower(DefaultClaudePrompt)
	for _, element := range requiredElements {
		if !strings.Contains(lowerPrompt, element) {
			t.Errorf("DefaultClaudePrompt should contain %q", element)
		}
	}
}

func TestDefaultGeminiPrompt(t *testing.T) {
	if DefaultGeminiPrompt == "" {
		t.Error("DefaultGeminiPrompt should not be empty")
	}

	// Check for key elements
	requiredElements := []string{
		"code review",
		"bugs",
		"security",
		"performance",
		"finding",
	}

	lowerPrompt := strings.ToLower(DefaultGeminiPrompt)
	for _, element := range requiredElements {
		if !strings.Contains(lowerPrompt, element) {
			t.Errorf("DefaultGeminiPrompt should contain %q", element)
		}
	}
}

func TestDefaultPromptsAreDecoupled(t *testing.T) {
	// Prompts are decoupled to allow independent tuning per agent
	// Both should be valid prompts but don't need to be identical
	if DefaultClaudePrompt == "" || DefaultGeminiPrompt == "" {
		t.Error("Both prompts should be non-empty")
	}
}

func TestPromptInstructionsIncludeExamples(t *testing.T) {
	// Verify that the prompt includes examples to guide the agent
	if !strings.Contains(DefaultClaudePrompt, "Example") {
		t.Error("DefaultClaudePrompt should include examples")
	}

	// Check for example patterns like file paths with line numbers
	if !strings.Contains(DefaultClaudePrompt, ".go:") {
		t.Error("DefaultClaudePrompt should include Go file examples with line numbers")
	}
}

func TestPromptInstructsNoFalsePositives(t *testing.T) {
	// Verify that the prompt instructs agents not to output "looks good" messages
	lowerPrompt := strings.ToLower(DefaultClaudePrompt)
	if !strings.Contains(lowerPrompt, "do not output") || !strings.Contains(lowerPrompt, "looks good") {
		t.Error("DefaultClaudePrompt should instruct agents not to output 'looks good' messages")
	}
}
