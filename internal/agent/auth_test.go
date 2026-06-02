package agent

import (
	"testing"
)

func TestIsAuthFailure(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		exitCode int
		stderr   string
		want     bool
	}{
		{"agy exit 41 is not special", "agy", 41, "", false},
		{"agy auth stderr", "agy", 1, "authentication required", true},
		{"agy exit 0 is never auth failure", "agy", 0, "", false},
		{"agy generic failure remains retryable", "agy", 1, "model overloaded", false},
		{"unknown agent with auth stderr", "unknown", 1, "api_key not set", true},
		{"unknown agent no auth signal", "unknown", 1, "something failed", false},
		{"stderr unauthorized", "codex", 1, "Error: Unauthorized", true},
		{"stderr 401", "claude", 1, "HTTP 401 response", true},
		{"stderr authentication required", "agy", 1, "authentication required", true},
		{"stderr invalid credentials", "codex", 1, "invalid credentials", true},
		{"stderr bare credentials is not auth failure", "codex", 1, "credential helper error", false},
		{"exit 0 ignores auth stderr", "codex", 0, "api_key not set", false},
		{"case insensitive stderr", "claude", 1, "UNAUTHORIZED access", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAuthFailure(tt.agent, tt.exitCode, tt.stderr)
			if got != tt.want {
				t.Errorf("IsAuthFailure(%q, %d, %q) = %v, want %v",
					tt.agent, tt.exitCode, tt.stderr, got, tt.want)
			}
		})
	}
}

func TestAuthHint(t *testing.T) {
	agents := []string{"agy", "claude", "codex", "unknown"}
	for _, name := range agents {
		t.Run(name, func(t *testing.T) {
			hint := AuthHint(name)
			if hint == "" {
				t.Errorf("AuthHint(%q) returned empty string", name)
			}
		})
	}
}
