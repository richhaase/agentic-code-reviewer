package fpfilter

import (
	"strings"
	"testing"
)

func TestFilter_New(t *testing.T) {
	f := New("codex", 75, false)
	if f == nil {
		t.Fatal("New returned nil")
	}
}

func TestBuildPromptWithFeedback(t *testing.T) {
	feedback := "User said the null check is intentional"
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	if !strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should contain Prior Feedback Context section")
	}
	if !strings.Contains(prompt, feedback) {
		t.Error("prompt should contain the feedback text")
	}
}

func TestBuildPromptWithoutFeedback(t *testing.T) {
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, "")

	if strings.Contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should not contain Prior Feedback Context when feedback is empty")
	}
}
