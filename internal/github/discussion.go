package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

type DiscussionID struct {
	Kind string
	ID   int64
}

type Discussion struct {
	ID       DiscussionID
	Author   string
	Body     string
	Path     string
	Line     int
	DiffHunk string
	Revision string
}

type discussionResponse struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	UpdatedAt    string `json:"updated_at"`
	SubmittedAt  string `json:"submitted_at"`
	State        string `json:"state"`
	Path         string `json:"path"`
	Line         int    `json:"line"`
	OriginalLine int    `json:"original_line"`
	DiffHunk     string `json:"diff_hunk"`
}

func GetPRDiscussion(ctx context.Context, repositoryRoot, repositoryHost, prNumber string) ([]Discussion, error) {
	sources := []struct {
		kind     string
		endpoint string
	}{
		{kind: "issue_comment", endpoint: fmt.Sprintf("repos/{owner}/{repo}/issues/%s/comments", prNumber)},
		{kind: "review", endpoint: fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/reviews", prNumber)},
		{kind: "review_comment", endpoint: fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/comments", prNumber)},
	}
	var discussion []Discussion
	for _, source := range sources {
		args := []string{"api"}
		if repositoryHost != "" {
			args = append(args, "--hostname", repositoryHost)
		}
		args = append(args, "--paginate", "--jq", ".[]", source.endpoint)
		cmd := exec.CommandContext(ctx, "gh", args...)
		cmd.Dir = repositoryRoot
		out, err := cmd.Output()
		if err != nil {
			return nil, classifyGHError(err)
		}
		items, err := parseDiscussion(out, source.kind)
		if err != nil {
			return nil, err
		}
		discussion = append(discussion, items...)
	}
	sort.Slice(discussion, func(i, j int) bool {
		if discussion[i].ID.Kind == discussion[j].ID.Kind {
			return discussion[i].ID.ID < discussion[j].ID.ID
		}
		return discussion[i].ID.Kind < discussion[j].ID.Kind
	})
	return discussion, nil
}

func parseDiscussion(data []byte, kind string) ([]Discussion, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var items []Discussion
	for {
		var raw discussionResponse
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to parse PR discussion: %w", err)
		}
		body := strings.TrimSpace(raw.Body)
		if raw.ID == 0 || body == "" || kind == "review" && strings.EqualFold(raw.State, "PENDING") {
			continue
		}
		timestamp := raw.UpdatedAt
		if timestamp == "" {
			timestamp = raw.SubmittedAt
		}
		line := raw.Line
		if line == 0 {
			line = raw.OriginalLine
		}
		revisionContent := strings.Join([]string{body, raw.Path, fmt.Sprint(line), raw.DiffHunk}, "\x00")
		digest := sha256.Sum256([]byte(revisionContent))
		items = append(items, Discussion{
			ID:       DiscussionID{Kind: kind, ID: raw.ID},
			Author:   raw.User.Login,
			Body:     body,
			Path:     raw.Path,
			Line:     line,
			DiffHunk: raw.DiffHunk,
			Revision: timestamp + ":" + hex.EncodeToString(digest[:]),
		})
	}
	return items, nil
}
