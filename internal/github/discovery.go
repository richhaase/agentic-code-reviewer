package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

var ErrTransient = errors.New("transient GitHub failure")

const maxSearchResults = 1000

type SearchKind string

const (
	SearchKindReviewRequested     SearchKind = "review_requested"
	SearchKindAuthored            SearchKind = "authored"
	SearchKindTeamReviewRequested SearchKind = "team_review_requested"
)

type SearchQuery struct {
	Kind         SearchKind
	Organization string
	Login        string
	Team         string
}

func (q SearchQuery) Validate() error {
	switch q.Kind {
	case SearchKindReviewRequested, SearchKindAuthored:
		if strings.TrimSpace(q.Login) == "" {
			return fmt.Errorf("search query: login is required for kind %q", q.Kind)
		}
	case SearchKindTeamReviewRequested:
		if strings.TrimSpace(q.Team) == "" {
			return fmt.Errorf("search query: team is required for kind %q", q.Kind)
		}
		if !strings.Contains(q.Team, "/") && strings.TrimSpace(q.Organization) == "" {
			return fmt.Errorf("search query: team %q is ambiguous without org/team or an organization", q.Team)
		}
	default:
		return fmt.Errorf("search query: unknown kind %q", q.Kind)
	}
	return nil
}

type Discovery interface {
	Search(ctx context.Context, query SearchQuery) ([]domain.PullRequestKey, error)
	Enrich(ctx context.Context, key domain.PullRequestKey) (domain.PullRequestSnapshot, error)
}

type ghDiscovery struct{}

func NewDiscovery() Discovery {
	return ghDiscovery{}
}

var _ Discovery = ghDiscovery{}

func (ghDiscovery) Search(ctx context.Context, query SearchQuery) ([]domain.PullRequestKey, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}

	args := []string{"search", "prs", "--state", "open", "--limit", strconv.Itoa(maxSearchResults), "--json", "number,url,repository"}
	if query.Organization != "" {
		args = append(args, "--owner", query.Organization)
	}
	switch query.Kind {
	case SearchKindReviewRequested:
		args = append(args, "--review-requested", query.Login)
	case SearchKindAuthored:
		args = append(args, "--author", query.Login)
	case SearchKindTeamReviewRequested:
		team := query.Team
		if query.Organization != "" && !strings.Contains(team, "/") {
			team = query.Organization + "/" + team
		}
		args = append(args, "--review-requested", team)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, classifyDiscoveryError(err)
	}
	return parseSearchResults(out)
}

type searchResultRepository struct {
	NameWithOwner string `json:"nameWithOwner"`
}

type searchResultItem struct {
	Number     int                    `json:"number"`
	URL        string                 `json:"url"`
	Repository searchResultRepository `json:"repository"`
}

func parseSearchResults(data []byte) ([]domain.PullRequestKey, error) {
	var items []searchResultItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	keys := make([]domain.PullRequestKey, 0, len(items))
	for _, item := range items {
		host, err := hostFromURL(item.URL)
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(item.Repository.NameWithOwner, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("search result has unexpected repository %q", item.Repository.NameWithOwner)
		}
		key := domain.PullRequestKey{Host: host, Owner: parts[0], Repository: parts[1], Number: item.Number}
		if err := key.Validate(); err != nil {
			return nil, fmt.Errorf("search result produced invalid pull request key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func hostFromURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("failed to parse pull request URL %q", raw)
	}
	return parsed.Hostname(), nil
}

func (ghDiscovery) Enrich(ctx context.Context, key domain.PullRequestKey) (domain.PullRequestSnapshot, error) {
	if err := key.Validate(); err != nil {
		return domain.PullRequestSnapshot{}, err
	}

	repo := key.Host + "/" + key.Owner + "/" + key.Repository

	args := []string{
		"pr", "view", strconv.Itoa(key.Number),
		"-R", repo,
		"--json", "number,url,title,author,state,isDraft,headRefOid,baseRefOid,reviewDecision,reviewRequests,latestReviews,statusCheckRollup,mergeStateStatus,updatedAt",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return domain.PullRequestSnapshot{}, classifyDiscoveryError(err)
	}
	return parseEnrichResponse(out, key)
}

type enrichAuthor struct {
	Login string `json:"login"`
}

type enrichReviewRequest struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
	Slug     string `json:"slug"`
}

type enrichLatestReview struct {
	Author      enrichAuthor `json:"author"`
	State       string       `json:"state"`
	SubmittedAt time.Time    `json:"submittedAt"`
}

type enrichCheck struct {
	Typename   string `json:"__typename"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

type prViewEnrichResponse struct {
	Number            int                   `json:"number"`
	URL               string                `json:"url"`
	Title             string                `json:"title"`
	Author            enrichAuthor          `json:"author"`
	State             string                `json:"state"`
	IsDraft           bool                  `json:"isDraft"`
	HeadRefOid        string                `json:"headRefOid"`
	BaseRefOid        string                `json:"baseRefOid"`
	ReviewDecision    string                `json:"reviewDecision"`
	ReviewRequests    []enrichReviewRequest `json:"reviewRequests"`
	LatestReviews     []enrichLatestReview  `json:"latestReviews"`
	StatusCheckRollup []enrichCheck         `json:"statusCheckRollup"`
	MergeStateStatus  string                `json:"mergeStateStatus"`
	UpdatedAt         time.Time             `json:"updatedAt"`
}

func parseEnrichResponse(data []byte, key domain.PullRequestKey) (domain.PullRequestSnapshot, error) {
	var resp prViewEnrichResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return domain.PullRequestSnapshot{}, fmt.Errorf("failed to parse pull request enrichment: %w", err)
	}
	if resp.Number != key.Number {
		return domain.PullRequestSnapshot{}, fmt.Errorf("enrichment response number %d does not match requested %d", resp.Number, key.Number)
	}

	reviewRequests := make([]domain.ReviewRequest, 0, len(resp.ReviewRequests))
	for _, r := range resp.ReviewRequests {
		if r.Typename == "Team" {
			slug := r.Slug
			if !strings.Contains(slug, "/") {
				slug = key.Owner + "/" + slug
			}
			reviewRequests = append(reviewRequests, domain.ReviewRequest{Kind: domain.ReviewRequestKindTeam, Login: slug})
			continue
		}
		reviewRequests = append(reviewRequests, domain.ReviewRequest{Kind: domain.ReviewRequestKindUser, Login: r.Login})
	}

	latestReviews := make([]domain.LatestReview, 0, len(resp.LatestReviews))
	for _, r := range resp.LatestReviews {
		latestReviews = append(latestReviews, domain.LatestReview{Author: r.Author.Login, State: r.State, SubmittedAt: r.SubmittedAt})
	}

	return domain.PullRequestSnapshot{
		PullRequest:      key,
		URL:              resp.URL,
		Title:            resp.Title,
		Author:           resp.Author.Login,
		State:            normalizePullRequestState(resp.State),
		Draft:            resp.IsDraft,
		HeadObjectID:     resp.HeadRefOid,
		BaseObjectID:     resp.BaseRefOid,
		ReviewRequests:   reviewRequests,
		ReviewDecision:   resp.ReviewDecision,
		LatestReviews:    latestReviews,
		CheckRollupState: reduceCheckRollup(resp.StatusCheckRollup),
		MergeState:       resp.MergeStateStatus,
		UpdatedAt:        resp.UpdatedAt,
		CapturedAt:       time.Now(),
	}, nil
}

func normalizePullRequestState(raw string) domain.PullRequestState {
	switch strings.ToUpper(raw) {
	case "OPEN":
		return domain.PullRequestStateOpen
	case "CLOSED":
		return domain.PullRequestStateClosed
	case "MERGED":
		return domain.PullRequestStateMerged
	default:
		return domain.PullRequestState(strings.ToLower(raw))
	}
}

func reduceCheckRollup(checks []enrichCheck) string {
	if len(checks) == 0 {
		return "NONE"
	}
	pending := false
	for _, c := range checks {
		if isFailingCheck(c) {
			return "FAILURE"
		}
		if isPendingCheck(c) {
			pending = true
		}
	}
	if pending {
		return "PENDING"
	}
	return "SUCCESS"
}

func isFailingCheck(c enrichCheck) bool {
	if c.Typename == "StatusContext" {
		return c.State == "FAILURE" || c.State == "ERROR"
	}
	if c.Status != "COMPLETED" {
		return false
	}
	switch c.Conclusion {
	case "FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE":
		return true
	default:
		return false
	}
}

func isPendingCheck(c enrichCheck) bool {
	if c.Typename == "StatusContext" {
		return c.State == "PENDING" || c.State == "EXPECTED" || c.State == ""
	}
	return c.Status != "COMPLETED"
}

func classifyDiscoveryError(err error) error {
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

	transientMarkers := []string{
		"rate limit",
		"timeout",
		"timed out",
		"temporarily unavailable",
		"could not resolve host",
		"connection reset",
		"connection refused",
		"502",
		"503",
		"504",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(stderr, marker) {
			return fmt.Errorf("%w: %s", ErrTransient, strings.TrimSpace(string(exitErr.Stderr)))
		}
	}

	errMsg := strings.TrimSpace(string(exitErr.Stderr))
	if errMsg != "" {
		return fmt.Errorf("gh command failed: %s", errMsg)
	}
	return fmt.Errorf("gh command failed: %w", err)
}

func ObserveSnapshot(ctx context.Context, discovery Discovery, key domain.PullRequestKey, previous *domain.PullRequestSnapshot) (domain.PullRequestSnapshot, error) {
	snapshot, err := discovery.Enrich(ctx, key)
	if err == nil {
		return snapshot, nil
	}
	if errors.Is(err, ErrTransient) && previous != nil {
		stale := *previous
		stale.Stale = true
		return stale, nil
	}
	return domain.PullRequestSnapshot{}, err
}
