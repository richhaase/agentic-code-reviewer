package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
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
	if prNumber == "" {
		return nil, errors.New("PR number is required")
	}

	result := &PRContext{Number: prNumber}

	desc, err := fetchPRDescription(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR description: %w", err)
	}
	result.Description = desc

	comments, err := fetchPRComments(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR comments: %w", err)
	}
	result.Comments = comments

	return result, nil
}

func fetchPRDescription(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "body", "--jq", ".body")
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

func fetchPRComments(ctx context.Context, prNumber string) ([]Comment, error) {
	var comments []Comment

	endpoint := "repos/{owner}/{repo}/pulls/" + prNumber + "/comments"
	cmd := exec.CommandContext(ctx, "gh", "api", "--paginate", "--jq", ".[]", endpoint)
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

	endpoint = "repos/{owner}/{repo}/issues/" + prNumber + "/comments"
	cmd = exec.CommandContext(ctx, "gh", "api", "--paginate", "--jq", ".[]", endpoint)
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

	endpoint = "repos/{owner}/{repo}/pulls/" + prNumber + "/reviews"
	cmd = exec.CommandContext(ctx, "gh", "api", "--paginate", "--jq", ".[]", endpoint)
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
