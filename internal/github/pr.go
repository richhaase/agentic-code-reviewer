// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoPRFound indicates no pull request exists for the given branch.
var ErrNoPRFound = errors.New("no pull request found")

// ErrAuthFailed indicates GitHub authentication failed.
var ErrAuthFailed = errors.New("GitHub authentication failed")

// CIStatus represents the CI check status for a PR.
type CIStatus struct {
	AllPassed bool
	Pending   []string
	Failed    []string
	Error     string
}

// GetCurrentPRNumber returns the PR number for the given branch (or current branch).
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed,
// or another error for other failures.
func GetCurrentPRNumber(ctx context.Context, branch string) (string, error) {
	args := []string{"pr", "view"}
	if branch != "" {
		args = append(args, branch)
	}
	args = append(args, "--json", "number", "--jq", ".number")

	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

// prViewResponse represents the JSON response from gh pr view.
type prViewResponse struct {
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
}

// parsePRViewJSON parses the JSON output from gh pr view.
func parsePRViewJSON(data []byte) (head, base string, err error) {
	var resp prViewResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("failed to parse PR view response: %w", err)
	}
	return resp.HeadRefName, resp.BaseRefName, nil
}

// GetPRBranch returns the head branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBranch(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	head, _, err := parsePRViewJSON(out)
	return head, err
}

// GetPRBaseRef returns the base branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBaseRef(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "baseRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	_, base, err := parsePRViewJSON(out)
	return base, err
}

// classifyGHError examines a gh CLI error and returns a typed error.
func classifyGHError(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("gh command failed: %w", err)
	}

	stderr := strings.ToLower(string(exitErr.Stderr))

	if strings.Contains(stderr, "no pull request") {
		return ErrNoPRFound
	}

	if strings.Contains(stderr, "401") ||
		strings.Contains(stderr, "auth") ||
		strings.Contains(stderr, "credentials") ||
		strings.Contains(stderr, "login") {
		return ErrAuthFailed
	}

	errMsg := strings.TrimSpace(string(exitErr.Stderr))
	if errMsg != "" {
		return fmt.Errorf("gh command failed: %s", errMsg)
	}
	return fmt.Errorf("gh command failed: %w", err)
}

// ApprovePR approves a PR with the given body.
func ApprovePR(ctx context.Context, prNumber, body string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "review", prNumber, "--approve", "--body-file", "-")
	cmd.Stdin = strings.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("failed to approve PR (%s): %w", errMsg, err)
		}
		return fmt.Errorf("failed to approve PR: %w", err)
	}
	return nil
}

// SubmitPRReview submits a PR review with the given body.
// If requestChanges is true, uses --request-changes; otherwise uses --comment.
func SubmitPRReview(ctx context.Context, prNumber, body string, requestChanges bool) error {
	flag := "--comment"
	if requestChanges {
		flag = "--request-changes"
	}

	cmd := exec.CommandContext(ctx, "gh", "pr", "review", prNumber, flag, "--body-file", "-")
	cmd.Stdin = strings.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("failed to submit PR review (%s): %w", errMsg, err)
		}
		return fmt.Errorf("failed to submit PR review: %w", err)
	}
	return nil
}

// CheckCIStatus checks the CI status for a PR.
func CheckCIStatus(ctx context.Context, prNumber string) CIStatus {
	cmd := exec.CommandContext(ctx, "gh", "pr", "checks", prNumber, "--json", "name,bucket")
	out, err := cmd.Output()
	if err != nil {
		var stderr bytes.Buffer
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr.Write(exitErr.Stderr)
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return CIStatus{Error: errMsg}
	}

	return ParseCIChecks(out)
}

// CICheck represents a single CI check from the GitHub API.
type CICheck struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
}

// ParseCIChecks parses CI check JSON output and categorizes results.
func ParseCIChecks(data []byte) CIStatus {
	var checks []CICheck
	if err := json.Unmarshal(data, &checks); err != nil {
		return CIStatus{Error: "failed to parse CI status"}
	}

	if len(checks) == 0 {
		// No CI checks configured - allow approval
		return CIStatus{AllPassed: true}
	}

	var pending, failed []string
	for _, check := range checks {
		bucket := strings.ToLower(check.Bucket)
		switch bucket {
		case "pending":
			pending = append(pending, check.Name)
		case "pass", "skipping":
			// OK
		default:
			// fail, cancel, or unknown
			failed = append(failed, check.Name)
		}
	}

	return CIStatus{
		AllPassed: len(pending) == 0 && len(failed) == 0,
		Pending:   pending,
		Failed:    failed,
	}
}

// IsGHAvailable checks if the gh CLI is available.
func IsGHAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// CheckGHAvailable returns an error if the gh CLI is not available.
func CheckGHAvailable() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not available: %w", err)
	}
	return nil
}

// GetCurrentUser returns the username of the authenticated gh user.
// Returns empty string on error.
func GetCurrentUser(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetPRAuthor returns the username of the PR author.
// Returns empty string on error.
func GetPRAuthor(ctx context.Context, prNumber string) string {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "author", "--jq", ".author.login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsSelfReview checks if the current user is the author of the PR.
func IsSelfReview(ctx context.Context, prNumber string) bool {
	currentUser := GetCurrentUser(ctx)
	prAuthor := GetPRAuthor(ctx, prNumber)
	return checkSelfReview(currentUser, prAuthor)
}

// checkSelfReview compares usernames to determine if this is a self-review.
// Returns true if:
// - Both usernames are non-empty and match (case-insensitive), OR
// - Either username is empty (fail closed: assume self-review when uncertain)
func checkSelfReview(currentUser, prAuthor string) bool {
	if currentUser == "" || prAuthor == "" {
		// Fail closed: if we can't determine users, assume self-review
		// to prevent accidental self-approvals
		return true
	}
	return strings.EqualFold(currentUser, prAuthor)
}
