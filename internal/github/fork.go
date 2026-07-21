package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ForkRef struct {
	Username   string
	Branch     string
	RepoURL    string
	RemoteName string
	PRNumber   int
}

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

var ErrNoForkPRFound = fmt.Errorf("no open PR found for fork reference")

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

func buildForkRef(username, branch, repoName string, prNumber int) *ForkRef {
	return &ForkRef{
		Username:   username,
		Branch:     branch,
		RepoURL:    fmt.Sprintf("https://github.com/%s/%s.git", username, repoName),
		RemoteName: fmt.Sprintf("fork-%s", username),
		PRNumber:   prNumber,
	}
}

func ResolveForkRef(ctx context.Context, ref string) (*ForkRef, error) {
	username, branch, ok := ParseForkNotation(ref)
	if !ok {
		return nil, nil
	}

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
