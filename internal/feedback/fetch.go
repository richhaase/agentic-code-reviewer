package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type PRContext struct {
	Number      string
	Description string
	Comments    []Comment
}

type Comment struct {
	Author     string
	Body       string
	IsResolved bool
	Replies    []Reply
}

type Reply struct {
	Author string
	Body   string
}

func (p *PRContext) HasContent() bool {
	return p.Description != "" || len(p.Comments) > 0
}

func FetchPRContext(ctx context.Context, prNumber string) (*PRContext, error) {
	return FetchPRContextFromDir(ctx, prNumber, "")
}

func FetchPRContextFromDir(ctx context.Context, prNumber, workDir string) (*PRContext, error) {
	return fetchPRContext(ctx, prNumber, workDir, nil)
}

func FetchPRContextForPullRequest(ctx context.Context, key domain.PullRequestKey, workDir string) (*PRContext, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return fetchPRContext(ctx, strconv.Itoa(key.Number), workDir, &key)
}

func fetchPRContext(ctx context.Context, prNumber, workDir string, key *domain.PullRequestKey) (*PRContext, error) {
	if prNumber == "" {
		return nil, errors.New("PR number is required")
	}

	result := &PRContext{Number: prNumber}

	desc, err := fetchPRDescription(ctx, prNumber, workDir, key)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR description: %w", err)
	}
	result.Description = desc

	comments, err := fetchPRComments(ctx, prNumber, workDir, key)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR comments: %w", err)
	}
	result.Comments = comments

	return result, nil
}

func fetchPRDescription(ctx context.Context, prNumber, workDir string, key *domain.PullRequestKey) (string, error) {
	args := []string{"pr", "view", prNumber, "--json", "body", "--jq", ".body"}
	if key != nil {
		args = append(args, "--repo", repositorySelector(*key))
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type prCommentResponse struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

func fetchPRComments(ctx context.Context, prNumber, workDir string, key *domain.PullRequestKey) ([]Comment, error) {
	var comments []Comment

	endpoint := pullRequestEndpoint(key, "pulls/"+prNumber+"/comments")
	cmd := exec.CommandContext(ctx, "gh", apiArgs(key, endpoint)...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch review comments: %w", err)
	}

	reviewComments, err := parseNDJSON(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse review comments: %w", err)
	}

	for _, r := range reviewComments {
		if r.Body != "" {
			comments = append(comments, Comment{
				Author: r.User.Login,
				Body:   r.Body,
			})
		}
	}

	endpoint = pullRequestEndpoint(key, "issues/"+prNumber+"/comments")
	cmd = exec.CommandContext(ctx, "gh", apiArgs(key, endpoint)...)
	cmd.Dir = workDir
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue comments: %w", err)
	}

	issueComments, err := parseNDJSON(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issue comments: %w", err)
	}

	for _, r := range issueComments {
		if r.Body != "" {
			comments = append(comments, Comment{
				Author: r.User.Login,
				Body:   r.Body,
			})
		}
	}

	endpoint = pullRequestEndpoint(key, "pulls/"+prNumber+"/reviews")
	cmd = exec.CommandContext(ctx, "gh", apiArgs(key, endpoint)...)
	cmd.Dir = workDir
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch review summaries: %w", err)
	}

	reviewSummaries, err := parseNDJSON(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse review summaries: %w", err)
	}

	for _, r := range reviewSummaries {
		if r.Body != "" {
			comments = append(comments, Comment{
				Author: r.User.Login,
				Body:   r.Body,
			})
		}
	}

	return comments, nil
}

func repositorySelector(key domain.PullRequestKey) string {
	return key.Host + "/" + key.Owner + "/" + key.Repository
}

func pullRequestEndpoint(key *domain.PullRequestKey, suffix string) string {
	if key == nil {
		return "repos/{owner}/{repo}/" + suffix
	}
	return "repos/" + key.Owner + "/" + key.Repository + "/" + suffix
}

func apiArgs(key *domain.PullRequestKey, endpoint string) []string {
	args := []string{"api"}
	if key != nil {
		args = append(args, "--hostname", key.Host)
	}
	return append(args, "--paginate", "--jq", ".[]", endpoint)
}

func parseNDJSON(data []byte) ([]prCommentResponse, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var results []prCommentResponse
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for {
		var item prCommentResponse
		if err := decoder.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		results = append(results, item)
	}
	return results, nil
}
