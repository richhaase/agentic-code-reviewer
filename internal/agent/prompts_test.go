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

func TestRenderPrompt(t *testing.T) {
	tests := []struct {
		name     string
		template string
		guidance string
		want     string
	}{
		{
			name:     "empty guidance strips placeholder",
			template: "Review this code.\n{{guidance}}",
			guidance: "",
			want:     "Review this code.\n",
		},
		{
			name:     "non-empty guidance injects section",
			template: "Review this code.\n{{guidance}}",
			guidance: "Focus on security issues.",
			want:     "Review this code.\n\n\nAdditional context:\nFocus on security issues.",
		},
		{
			name:     "no placeholder in template is no-op",
			template: "Review this code with no placeholder.",
			guidance: "This should not appear anywhere unexpected.",
			want:     "Review this code with no placeholder.",
		},
		{
			name:     "multiline guidance",
			template: "Review this code.\n{{guidance}}",
			guidance: "Line one.\nLine two.\nLine three.",
			want:     "Review this code.\n\n\nAdditional context:\nLine one.\nLine two.\nLine three.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderPrompt(tt.template, tt.guidance)
			if got != tt.want {
				t.Errorf("RenderPrompt() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestDefaultPrompts_ContainPlaceholder(t *testing.T) {
	prompts := map[string]string{
		"DefaultClaudePrompt":        DefaultClaudePrompt,
		"DefaultClaudeRefFilePrompt": DefaultClaudeRefFilePrompt,
		"DefaultGeminiPrompt":        DefaultGeminiPrompt,
		"DefaultGeminiRefFilePrompt": DefaultGeminiRefFilePrompt,
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(prompt, "{{guidance}}") {
				t.Errorf("%s does not contain {{guidance}} placeholder", name)
			}
		})
	}
}

func TestRenderPrompt_DefaultPrompts_NoGuidance(t *testing.T) {
	prompts := map[string]string{
		"DefaultClaudePrompt":        DefaultClaudePrompt,
		"DefaultClaudeRefFilePrompt": DefaultClaudeRefFilePrompt,
		"DefaultGeminiPrompt":        DefaultGeminiPrompt,
		"DefaultGeminiRefFilePrompt": DefaultGeminiRefFilePrompt,
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			rendered := RenderPrompt(prompt, "")
			if strings.Contains(rendered, "{{guidance}}") {
				t.Errorf("%s rendered with empty guidance still contains {{guidance}} placeholder", name)
			}
			if strings.Contains(rendered, "Additional context:") {
				t.Errorf("%s rendered with empty guidance contains 'Additional context:' header", name)
			}
		})
	}
}
