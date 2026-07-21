package agent

import (
	"encoding/json"
	"slices"
	"strings"
)

var authExitCodes = map[string][]int{}

var authStderrPatterns = []string{
	"api_key",
	"unauthorized",
	"401",
	"authentication required",
	"invalid credentials",
	"login required",
	"not authenticated",
	"not signed in",
}

var authStdoutPrefixes = []string{
	"api error: 401",
	"api error: 403",
	"authentication failed",
	"error: api error: 401",
	"error: api error: 403",
	"error: authentication failed",
	"error: authentication required",
	"error: failed to authenticate",
	"error: invalid authentication credentials",
	"error: login required",
	"error: not authenticated",
	"error: not signed in",
	"failed to authenticate",
}

var authStdoutExactMessages = []string{
	"authentication required",
	"invalid authentication credentials",
	"login required",
	"not authenticated",
	"not signed in",
}

var authHints = map[string]string{
	"agy":    "Run 'agy' and complete Google sign-in, or check your Antigravity CLI credentials.",
	"claude": "Run 'claude login' or check your API key configuration.",
	"codex":  "Set OPENAI_API_KEY or run 'codex auth' to authenticate.",
	"gemini": "Authenticate Gemini CLI with enterprise credentials, or use 'agy' for non-enterprise Google access.",
}

func IsAuthFailure(agentName string, exitCode int, stderr string, stdout ...string) bool {
	if exitCode == 0 {
		return false
	}

	if codes, ok := authExitCodes[agentName]; ok {
		if slices.Contains(codes, exitCode) {
			return true
		}
	}

	if containsAuthPattern(stderr) {
		return true
	}

	for _, text := range stdout {
		if looksLikeStdoutAuthFailure(text) {
			return true
		}
	}

	return false
}

func containsAuthPattern(text string) bool {
	lower := strings.ToLower(text)
	for _, pattern := range authStderrPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func looksLikeStdoutAuthFailure(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if looksLikeStructuredStdoutAuthFailure(trimmed) {
		return true
	}
	return looksLikeShortAuthMessage(trimmed)
}

func looksLikeStructuredStdoutAuthFailure(text string) bool {
	var envelope struct {
		IsError        bool   `json:"is_error"`
		APIErrorStatus int    `json:"api_error_status"`
		Result         string `json:"result"`
		Error          string `json:"error"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return false
	}

	message := strings.Join([]string{envelope.Result, envelope.Error, envelope.Message}, "\n")
	if envelope.APIErrorStatus == 401 || envelope.APIErrorStatus == 403 {
		return envelope.IsError
	}
	return envelope.IsError && looksLikeShortAuthMessage(message)
}

func looksLikeShortAuthMessage(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) == 0 || len(nonEmpty) > 3 {
		return false
	}

	normalized := strings.ToLower(strings.Join(nonEmpty, " "))
	if len(normalized) > 1024 {
		return false
	}
	for _, message := range authStdoutExactMessages {
		if normalized == message {
			return true
		}
	}
	for _, phrase := range authStdoutPrefixes {
		if strings.HasPrefix(normalized, phrase) {
			return true
		}
	}
	return false
}

func AuthHint(agentName string) string {
	if hint, ok := authHints[agentName]; ok {
		return hint
	}
	return "Check your authentication configuration for " + agentName + "."
}
