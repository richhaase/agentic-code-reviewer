// Package feedback provides PR feedback summarization for false positive filtering.
package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PRContext holds the PR description and all comments.
type PRContext struct {
	Number      string
	Description string
	Comments    []Comment
}

// Comment represents a PR comment with its replies.
type Comment struct {
	Author     string
	Body       string
	IsResolved bool
	Replies    []Reply
}

// Reply represents a reply to a comment.
type Reply struct {
	Author string
	Body   string
}

// HasContent returns true if the context has any content worth summarizing.
func (p *PRContext) HasContent() bool {
	return p.Description != "" || len(p.Comments) > 0
}

// FetchPRContext retrieves the PR description and all comments via gh CLI.
func FetchPRContext(ctx context.Context, prNumber string) (*PRContext, error) {
	if prNumber == "" {
		return nil, errors.New("PR number is required")
	}

	result := &PRContext{Number: prNumber}

	// Fetch PR description
	desc, err := fetchPRDescription(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR description: %w", err)
	}
	result.Description = desc

	// Fetch comments
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

// prCommentResponse represents a comment from gh api.
type prCommentResponse struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

func fetchPRComments(ctx context.Context, prNumber string) ([]Comment, error) {
	// Fetch review comments (comments on code)
	endpoint := "repos/{owner}/{repo}/pulls/" + prNumber + "/comments"
	cmd := exec.CommandContext(ctx, "gh", "api", endpoint)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // No comments is not an error
	}

	var responses []prCommentResponse
	if err := json.Unmarshal(out, &responses); err != nil {
		return nil, nil // Parse error, treat as no comments
	}

	comments := make([]Comment, 0, len(responses))
	for _, r := range responses {
		if r.Body != "" {
			comments = append(comments, Comment{
				Author: r.User.Login,
				Body:   r.Body,
			})
		}
	}

	// Also fetch issue comments (general PR comments)
	endpoint = "repos/{owner}/{repo}/issues/" + prNumber + "/comments"
	cmd = exec.CommandContext(ctx, "gh", "api", endpoint)
	out, err = cmd.Output()
	if err == nil {
		var issueComments []prCommentResponse
		if json.Unmarshal(out, &issueComments) == nil {
			for _, r := range issueComments {
				if r.Body != "" {
					comments = append(comments, Comment{
						Author: r.User.Login,
						Body:   r.Body,
					})
				}
			}
		}
	}

	return comments, nil
}
