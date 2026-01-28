// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"strings"
)

// ForkRef contains resolved information about a fork reference.
type ForkRef struct {
	Username   string // Fork owner username (e.g., "yunidbauza")
	Branch     string // Branch name (e.g., "feat/enable-pr-number-review")
	RepoURL    string // Clone URL (e.g., "https://github.com/yunidbauza/repo.git")
	RemoteName string // Temporary remote name (e.g., "fork-yunidbauza")
	PRNumber   int    // Associated PR number
}

// ParseForkNotation parses GitHub's "username:branch" fork notation.
// Returns the username, branch, and true if valid fork notation.
// Returns "", "", false if not fork notation or invalid.
func ParseForkNotation(ref string) (username, branch string, ok bool) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	username, branch = parts[0], parts[1]
	if username == "" || branch == "" {
		return "", "", false
	}
	return username, branch, true
}
