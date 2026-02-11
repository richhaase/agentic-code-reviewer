package agent

import (
	"strings"
	"testing"
)

func TestBuildPromptWithDiff_EmptyDiff(t *testing.T) {
	prompt := "Review this code"
	result := BuildPromptWithDiff(prompt, "")
	expected := "Review this code\n\n(No changes detected)"
	if result != expected {
		t.Errorf("BuildPromptWithDiff() = %q, want %q", result, expected)
	}
}

func TestBuildPromptWithDiff_WithDiff(t *testing.T) {
	prompt := "Review this code"
	diff := "- old\n+ new"
	result := BuildPromptWithDiff(prompt, diff)
	if !strings.Contains(result, prompt) {
		t.Errorf("BuildPromptWithDiff() result doesn't contain prompt")
	}
	if !strings.Contains(result, "```diff") {
		t.Errorf("BuildPromptWithDiff() result doesn't contain diff block")
	}
	if !strings.Contains(result, diff) {
		t.Errorf("BuildPromptWithDiff() result doesn't contain diff")
	}
}
