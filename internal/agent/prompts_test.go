package agent

import (
	"strings"
	"testing"
)

func TestDefaultClaudePrompt(t *testing.T) {
	if DefaultClaudePrompt == "" {
		t.Error("DefaultClaudePrompt should not be empty")
	}

	// Check for key elements in the tuned Claude prompt
	requiredElements := []string{
		"bugs",
		"security",
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
	// Verify that the Gemini prompt includes examples to guide the agent
	// (Claude prompt is tuned for brevity and omits examples)
	if !strings.Contains(DefaultGeminiPrompt, "Example") {
		t.Error("DefaultGeminiPrompt should include examples")
	}

	// Check for example patterns like file paths with line numbers
	if !strings.Contains(DefaultGeminiPrompt, ".go:") {
		t.Error("DefaultGeminiPrompt should include Go file examples with line numbers")
	}
}

func TestPromptInstructsNoFalsePositives(t *testing.T) {
	// Verify that the Gemini prompt instructs agents not to output "looks good" messages
	// (Claude prompt achieves this via "Skip: Suggestions" instead)
	lowerPrompt := strings.ToLower(DefaultGeminiPrompt)
	if !strings.Contains(lowerPrompt, "do not output") || !strings.Contains(lowerPrompt, "looks good") {
		t.Error("DefaultGeminiPrompt should instruct agents not to output 'looks good' messages")
	}
}
