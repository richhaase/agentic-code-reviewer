package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type pullRequestIdentityResponse struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

func GetPullRequestKey(ctx context.Context, repositoryRoot, prNumber string) (domain.PullRequestKey, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "number,url")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return domain.PullRequestKey{}, classifyGHError(err)
	}
	return parsePullRequestKey(out)
}

func parsePullRequestKey(data []byte) (domain.PullRequestKey, error) {
	var response pullRequestIdentityResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return domain.PullRequestKey{}, fmt.Errorf("failed to parse pull request identity: %w", err)
	}
	parsed, err := url.Parse(response.URL)
	if err != nil {
		return domain.PullRequestKey{}, fmt.Errorf("failed to parse pull request URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return domain.PullRequestKey{}, fmt.Errorf("pull request URL %q has an unexpected path", response.URL)
	}
	pathNumber, err := strconv.Atoi(parts[3])
	if err != nil || pathNumber != response.Number {
		return domain.PullRequestKey{}, fmt.Errorf("pull request URL %q does not match number %d", response.URL, response.Number)
	}
	key := domain.PullRequestKey{
		Host:       parsed.Hostname(),
		Owner:      parts[0],
		Repository: parts[1],
		Number:     response.Number,
	}
	if err := key.Validate(); err != nil {
		return domain.PullRequestKey{}, err
	}
	return key, nil
}
