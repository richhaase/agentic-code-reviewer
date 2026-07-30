package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

func GetCurrentPRNumber(ctx context.Context, repositoryRoot, branch string) (string, error) {
	args := []string{"pr", "view"}
	if branch != "" {
		args = append(args, branch)
	}
	args = append(args, "--json", "number", "--jq", ".number")

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repositoryRoot
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

func GetPRBaseRef(ctx context.Context, repositoryRoot, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "baseRefName")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	_, base, err := parsePRViewJSON(out)
	return base, err
}

func ValidatePR(ctx context.Context, repositoryRoot, prNumber string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "number")
	cmd.Dir = repositoryRoot
	_, err := cmd.Output()
	if err != nil {
		return classifyGHError(err)
	}
	return nil
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

	remote, err := matchingFetchRemote(remoteOut, repoInfo.URL, repoInfo.SSHUrl)
	if err != nil {
		return "", err
	}
	if remote != "" {
		return remote, nil
	}

	return "", fmt.Errorf("no configured remote matches the GitHub repository")
}

func matchingFetchRemote(remoteOut []byte, repositoryURL, repositorySSHURL string) (string, error) {
	lines := strings.Split(string(remoteOut), "\n")
	var matches []string
	seen := make(map[string]struct{})
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "(fetch)" {
			continue
		}
		remoteName := fields[0]
		remoteURL := fields[1]

		if urlMatches(remoteURL, repositoryURL) || urlMatches(remoteURL, repositorySSHURL) {
			if _, ok := seen[remoteName]; !ok {
				seen[remoteName] = struct{}{}
				matches = append(matches, remoteName)
			}
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", fmt.Errorf("multiple configured fetch remotes match the GitHub repository: %s", strings.Join(matches, ", "))
}

func urlMatches(url1, url2 string) bool {
	if strings.TrimSpace(url1) == "" || strings.TrimSpace(url2) == "" {
		return strings.TrimSpace(url1) == strings.TrimSpace(url2)
	}
	first, firstHasHost := normalizeRepositoryURL(url1)
	second, secondHasHost := normalizeRepositoryURL(url2)
	return firstHasHost && secondHasHost && first == second
}

func isSupportedGitScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ssh", "git", "git+ssh", "ssh+git":
		return true
	default:
		return false
	}
}

func parseRemoteHostAndPath(raw string) (host, path string, hasHost bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Host != "" && isSupportedGitScheme(parsed.Scheme) {
		host := parsed.Hostname()
		if port := parsed.Port(); port != "" && !isDefaultRepositoryPort(parsed.Scheme, port) {
			host = net.JoinHostPort(host, port)
		}
		return host, parsed.Path, host != ""
	}

	colon := strings.Index(raw, ":")
	slash := strings.Index(raw, "/")
	if !strings.Contains(raw, "://") && colon > 0 && (slash == -1 || colon < slash) {
		return raw[:colon], raw[colon+1:], true
	}

	return "", raw, false
}

func normalizeRepositoryURL(raw string) (string, bool) {
	host, path, hasHost := parseRemoteHostAndPath(raw)
	return normalizeRepositoryLocation(host, path), hasHost
}

func ParseRemoteURL(raw string) (host, owner, repo string, ok bool) {
	rawHost, rawPath, hasHost := parseRemoteHostAndPath(raw)
	if !hasHost {
		return "", "", "", false
	}

	host = strings.TrimSpace(rawHost)
	if at := strings.LastIndex(host, "@"); at != -1 {
		host = host[at+1:]
	}
	host = strings.ToLower(host)
	if host == "" {
		return "", "", "", false
	}

	trimmedPath := strings.ToLower(strings.Trim(strings.TrimSpace(rawPath), "/"))
	trimmedPath = strings.TrimSuffix(trimmedPath, ".git")
	segments := strings.Split(trimmedPath, "/")
	if len(segments) != 2 {
		return "", "", "", false
	}

	owner = segments[0]
	repo = segments[1]
	if owner == "" || repo == "" {
		return "", "", "", false
	}
	return host, owner, repo, true
}

func isDefaultRepositoryPort(scheme, port string) bool {
	switch strings.ToLower(scheme) {
	case "ssh", "git+ssh", "ssh+git":
		return port == "22"
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	case "git":
		return port == "9418"
	default:
		return false
	}
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

func ApprovePR(ctx context.Context, repositoryRoot, prNumber, body string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "review", prNumber, "--approve", "--body-file", "-")
	cmd.Dir = repositoryRoot
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

func SubmitPRReview(ctx context.Context, repositoryRoot, prNumber, body string, requestChanges bool) error {
	flag := "--comment"
	if requestChanges {
		flag = "--request-changes"
	}

	cmd := exec.CommandContext(ctx, "gh", "pr", "review", prNumber, flag, "--body-file", "-")
	cmd.Dir = repositoryRoot
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

func SubmitPRReviewWithID(ctx context.Context, repositoryRoot, prNumber, body string, requestChanges bool) (DiscussionID, error) {
	event := "COMMENT"
	if requestChanges {
		event = "REQUEST_CHANGES"
	}

	payload, err := json.Marshal(map[string]string{"body": body, "event": event})
	if err != nil {
		return DiscussionID{}, fmt.Errorf("failed to encode PR review: %w", err)
	}
	endpoint := fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/reviews", prNumber)
	args := []string{"api"}
	if host := GetRepositoryHost(ctx, repositoryRoot); host != "" {
		args = append(args, "--hostname", host)
	}
	args = append(args, "--method", "POST", endpoint, "--input", "-")
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repositoryRoot
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return DiscussionID{}, fmt.Errorf("failed to submit PR review (%s): %w", errMsg, err)
		}
		return DiscussionID{}, fmt.Errorf("failed to submit PR review: %w", err)
	}
	var response struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return DiscussionID{}, fmt.Errorf("failed to parse submitted PR review: %w", err)
	}
	if response.ID == 0 {
		return DiscussionID{}, errors.New("submitted PR review response did not include an ID")
	}
	return DiscussionID{Kind: "review", ID: response.ID}, nil
}

func CheckCIStatus(ctx context.Context, repositoryRoot, prNumber string) CIStatus {
	cmd := exec.CommandContext(ctx, "gh", "pr", "checks", prNumber, "--json", "name,bucket")
	cmd.Dir = repositoryRoot
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
	BaseSHA        string
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

func GetPRWatchState(ctx context.Context, repositoryRoot, prNumber string) (PRWatchState, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefOid,baseRefOid,state,reviewRequests")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return PRWatchState{}, classifyGHError(err)
	}
	return ParsePRWatchState(out)
}

func ParsePRWatchState(data []byte) (PRWatchState, error) {
	var resp struct {
		HeadRefOid     string `json:"headRefOid"`
		BaseRefOid     string `json:"baseRefOid"`
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
		BaseSHA: resp.BaseRefOid,
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

func GetCurrentUser(ctx context.Context, host string) string {
	args := []string{"api", "user", "--jq", ".login"}
	if host != "" {
		args = append(args, "--hostname", host)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func GetRepositoryHost(ctx context.Context, repositoryRoot string) string {
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "url", "--jq", ".url")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	host, _, _, ok := ParseRemoteURL(strings.TrimSpace(string(out)))
	if !ok {
		return ""
	}
	return host
}

func GetPRAuthor(ctx context.Context, repositoryRoot, prNumber string) string {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "author", "--jq", ".author.login")
	cmd.Dir = repositoryRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func IsSelfReview(ctx context.Context, repositoryRoot, prNumber string) bool {
	currentUser := GetCurrentUser(ctx, GetRepositoryHost(ctx, repositoryRoot))
	prAuthor := GetPRAuthor(ctx, repositoryRoot, prNumber)
	return checkSelfReview(currentUser, prAuthor)
}

func checkSelfReview(currentUser, prAuthor string) bool {
	if currentUser == "" || prAuthor == "" {

		return true
	}
	return strings.EqualFold(currentUser, prAuthor)
}
