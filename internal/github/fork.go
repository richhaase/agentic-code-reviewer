// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// ErrNoForkPRFound indicates no PR exists for the given fork reference.
var ErrNoForkPRFound = fmt.Errorf("no open PR found for fork reference")

// prListResult represents a single PR from gh pr list output.
type prListResult struct {
	Number              int    `json:"number"`
	HeadRefName         string `json:"headRefName"`
	HeadRepositoryOwner struct {
		Login string `json:"login"`
	} `json:"headRepositoryOwner"`
	HeadRepository struct {
		Name string `json:"name"`
	} `json:"headRepository"`
}

// buildForkRef constructs a ForkRef from resolved PR metadata.
func buildForkRef(username, branch, repoName string, prNumber int) *ForkRef {
	return &ForkRef{
		Username:   username,
		Branch:     branch,
		RepoURL:    fmt.Sprintf("https://github.com/%s/%s.git", username, repoName),
		RemoteName: fmt.Sprintf("fork-%s", username),
		PRNumber:   prNumber,
	}
}

// ResolveForkRef detects and resolves "username:branch" fork notation.
// Returns (*ForkRef, nil) for valid fork refs with an open PR.
// Returns (nil, nil) if ref is not fork notation (caller should use ref as-is).
// Returns (nil, error) if fork notation is used but resolution fails.
func ResolveForkRef(ctx context.Context, ref string) (*ForkRef, error) {
	username, branch, ok := ParseForkNotation(ref)
	if !ok {
		return nil, nil // Not fork notation
	}

	// Query GitHub for PRs with this head branch
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--head", branch,
		"--json", "number,headRefName,headRepositoryOwner,headRepository",
		"--limit", "100",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query PRs: %w", classifyGHError(err))
	}

	var prs []prListResult
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}

	// Find PR from the specified fork owner
	for _, pr := range prs {
		if strings.EqualFold(pr.HeadRepositoryOwner.Login, username) {
			return buildForkRef(
				pr.HeadRepositoryOwner.Login,
				pr.HeadRefName,
				pr.HeadRepository.Name,
				pr.Number,
			), nil
		}
	}

	return nil, fmt.Errorf("%w: %s:%s", ErrNoForkPRFound, username, branch)
}
