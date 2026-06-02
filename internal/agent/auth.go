package agent

import (
	"slices"
	"strings"
)

// authExitCodes maps agent names to documented, auth-specific failure exit codes.
// Do not add generic non-zero exit codes here: agent CLIs also use them for
// transient model, tool, or network failures that should remain retryable.
var authExitCodes = map[string][]int{}

// authStderrPatterns contains substrings that indicate authentication failure
// when found in stderr output (checked case-insensitively).
var authStderrPatterns = []string{
	"api_key",
	"unauthorized",
	"401",
	"authentication required",
	"invalid credentials",
}

// authHints maps agent names to actionable error messages shown on auth failure.
var authHints = map[string]string{
	"agy":    "Run 'agy' and complete Google sign-in, or check your Antigravity CLI credentials.",
	"claude": "Run 'claude login' or check your API key configuration.",
	"codex":  "Set OPENAI_API_KEY or run 'codex auth' to authenticate.",
}

// IsAuthFailure returns true if the given exit code and stderr indicate
// an authentication failure for the named agent. Exit code 0 is never
// considered an auth failure.
func IsAuthFailure(agentName string, exitCode int, stderr string) bool {
	if exitCode == 0 {
		return false
	}

	if codes, ok := authExitCodes[agentName]; ok {
		if slices.Contains(codes, exitCode) {
			return true
		}
	}

	lower := strings.ToLower(stderr)
	for _, pattern := range authStderrPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// AuthHint returns an actionable error message for the named agent.
// Returns a generic hint for unknown agents.
func AuthHint(agentName string) string {
	if hint, ok := authHints[agentName]; ok {
		return hint
	}
	return "Check your authentication configuration for " + agentName + "."
}
