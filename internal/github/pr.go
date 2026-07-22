package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

var ErrNoPRFound = errors.New("no pull request found")

var ErrAuthFailed = errors.New("GitHub authentication failed")

type CIStatus struct {
	AllPassed bool
	Pending   []string
	Failed    []string
	Error     string
}

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

type prViewResponse struct {
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
}

func parsePRViewJSON(data []byte) (head, base string, err error) {
	var resp prViewResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("failed to parse PR view response: %w", err)
	}
	return resp.HeadRefName, resp.BaseRefName, nil
}

func GetPRBranch(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	head, _, err := parsePRViewJSON(out)
	return head, err
}

func GetPRBaseRef(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "baseRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	_, base, err := parsePRViewJSON(out)
	return base, err
}

func ValidatePR(ctx context.Context, prNumber string) error {

	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "number")
	_, err := cmd.Output()
	if err != nil {
		return classifyGHError(err)
	}
	return nil
}

func GetRepoRemote(ctx context.Context) string {
	remote, err := FindRepoRemote(ctx, "")
	if err != nil {
		return "origin"
	}
	return remote
}

func FindRepoRemote(ctx context.Context, repositoryRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "url,sshUrl")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to identify repository through gh: %w", err)
	}

	var repoInfo struct {
		URL    string `json:"url"`
		SSHUrl string `json:"sshUrl"`
	}
	if err := json.Unmarshal(out, &repoInfo); err != nil {
		return "", fmt.Errorf("failed to parse repository identity: %w", err)
	}

	remoteCmd := exec.CommandContext(ctx, "git", "remote", "-v")
	remoteCmd.Dir = repositoryRoot
	remoteOut, err := remoteCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list repository remotes: %w", err)
	}

	remote := matchingFetchRemote(remoteOut, repoInfo.URL, repoInfo.SSHUrl)
	if remote != "" {
		return remote, nil
	}

	return "", fmt.Errorf("no configured remote matches the GitHub repository")
}

func matchingFetchRemote(remoteOut []byte, repositoryURL, repositorySSHURL string) string {
	lines := strings.Split(string(remoteOut), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "(fetch)" {
			continue
		}
		remoteName := fields[0]
		remoteURL := fields[1]

		if urlMatches(remoteURL, repositoryURL) || urlMatches(remoteURL, repositorySSHURL) {
			return remoteName
		}
	}
	return ""
}

func urlMatches(url1, url2 string) bool {
	return normalizeRepositoryURL(url1) == normalizeRepositoryURL(url2)
}

func normalizeRepositoryURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" {
		return normalizeRepositoryLocation(parsed.Host, parsed.Path)
	}

	colon := strings.Index(raw, ":")
	slash := strings.Index(raw, "/")
	if colon > 0 && (slash == -1 || colon < slash) {
		return normalizeRepositoryLocation(raw[:colon], raw[colon+1:])
	}

	return normalizeRepositoryLocation("", raw)
}

func normalizeRepositoryLocation(host, path string) string {
	host = strings.TrimSpace(host)
	if at := strings.LastIndex(host, "@"); at != -1 {
		host = host[at+1:]
	}
	path = strings.Trim(strings.TrimSpace(path), "/")

	location := path
	if host != "" && path != "" {
		location = host + "/" + path
	} else if host != "" {
		location = host
	}

	location = strings.ToLower(strings.TrimSuffix(location, "/"))
	return strings.TrimSuffix(location, ".git")
}

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

type CICheck struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
}

func ParseCIChecks(data []byte) CIStatus {
	var checks []CICheck
	if err := json.Unmarshal(data, &checks); err != nil {
		return CIStatus{Error: "failed to parse CI status"}
	}

	if len(checks) == 0 {

		return CIStatus{AllPassed: true}
	}

	var pending, failed []string
	for _, check := range checks {
		bucket := strings.ToLower(check.Bucket)
		switch bucket {
		case "pending":
			pending = append(pending, check.Name)
		case "pass", "skipping":

		default:

			failed = append(failed, check.Name)
		}
	}

	return CIStatus{
		AllPassed: len(pending) == 0 && len(failed) == 0,
		Pending:   pending,
		Failed:    failed,
	}
}

type PRWatchState struct {
	HeadSHA        string
	State          string
	ReviewRequests []string
	TeamRequests   []string
}

func (s PRWatchState) Closed() bool { return strings.EqualFold(s.State, "CLOSED") }

func (s PRWatchState) Merged() bool { return strings.EqualFold(s.State, "MERGED") }

func (s PRWatchState) ReviewRequestedFrom(login string) bool {
	if login == "" {
		return false
	}
	for _, r := range s.ReviewRequests {
		if strings.EqualFold(r, login) {
			return true
		}
	}
	return false
}

func GetPRWatchState(ctx context.Context, prNumber string) (PRWatchState, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefOid,state,reviewRequests")
	out, err := cmd.Output()
	if err != nil {
		return PRWatchState{}, classifyGHError(err)
	}
	return ParsePRWatchState(out)
}

func ParsePRWatchState(data []byte) (PRWatchState, error) {
	var resp struct {
		HeadRefOid     string `json:"headRefOid"`
		State          string `json:"state"`
		ReviewRequests []struct {
			Login string `json:"login"`
			Slug  string `json:"slug"`
		} `json:"reviewRequests"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return PRWatchState{}, fmt.Errorf("failed to parse PR state response: %w", err)
	}

	state := PRWatchState{
		HeadSHA: resp.HeadRefOid,
		State:   resp.State,
	}
	for _, r := range resp.ReviewRequests {
		switch {
		case r.Login != "":
			state.ReviewRequests = append(state.ReviewRequests, r.Login)
		case r.Slug != "":
			state.TeamRequests = append(state.TeamRequests, r.Slug)
		}
	}
	return state, nil
}

func IsGHAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func CheckGHAvailable() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not available: %w", err)
	}
	return nil
}

func GetCurrentUser(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GetPRAuthor(ctx context.Context, prNumber string) string {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "author", "--jq", ".author.login")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func IsSelfReview(ctx context.Context, prNumber string) bool {
	currentUser := GetCurrentUser(ctx)
	prAuthor := GetPRAuthor(ctx, prNumber)
	return checkSelfReview(currentUser, prAuthor)
}

func checkSelfReview(currentUser, prAuthor string) bool {
	if currentUser == "" || prAuthor == "" {

		return true
	}
	return strings.EqualFold(currentUser, prAuthor)
}
