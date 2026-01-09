// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CIStatus represents the CI check status for a PR.
type CIStatus struct {
	AllPassed bool
	Pending   []string
	Failed    []string
	Error     string
}

// GetCurrentPRNumber returns the PR number for the given branch (or current branch).
// Returns empty string if no PR is found.
func GetCurrentPRNumber(branch string) string {
	args := []string{"pr", "view"}
	if branch != "" {
		args = append(args, branch)
	}
	args = append(args, "--json", "number", "--jq", ".number")

	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// PostPRComment posts a comment to a PR.
func PostPRComment(prNumber, body string) error {
	cmd := exec.Command("gh", "pr", "comment", prNumber, "--body-file", "-")
	cmd.Stdin = strings.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Errorf("failed to post comment: %s", errMsg)
	}
	return nil
}

// ApprovePR approves a PR with the given body.
func ApprovePR(prNumber, body string) error {
	cmd := exec.Command("gh", "pr", "review", prNumber, "--approve", "--body-file", "-")
	cmd.Stdin = strings.NewReader(body)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Errorf("failed to approve PR: %s", errMsg)
	}
	return nil
}

// CheckCIStatus checks the CI status for a PR.
func CheckCIStatus(prNumber string) CIStatus {
	cmd := exec.Command("gh", "pr", "checks", prNumber, "--json", "name,bucket")
	out, err := cmd.Output()
	if err != nil {
		var stderr bytes.Buffer
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr.Write(exitErr.Stderr)
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return CIStatus{Error: errMsg}
	}

	var checks []struct {
		Name   string `json:"name"`
		Bucket string `json:"bucket"`
	}
	if err := json.Unmarshal(out, &checks); err != nil {
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
