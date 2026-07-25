package desk

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/repos"
	"github.com/richhaase/agentic-code-reviewer/internal/store"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

type Item struct {
	Key              store.PullRequestKeyV1 `json:"key"`
	URL              string                 `json:"url"`
	Title            string                 `json:"title"`
	Author           string                 `json:"author"`
	State            string                 `json:"state"`
	Draft            bool                   `json:"draft"`
	HeadObjectID     string                 `json:"head_object_id"`
	BaseObjectID     string                 `json:"base_object_id"`
	ReviewDecision   string                 `json:"review_decision,omitempty"`
	CheckRollupState string                 `json:"check_rollup_state,omitempty"`
	MergeState       string                 `json:"merge_state,omitempty"`
	UpdatedAt        time.Time              `json:"updated_at"`
	CapturedAt       time.Time              `json:"captured_at"`
	SnapshotStale    bool                   `json:"snapshot_stale"`
	OwnPR            bool                   `json:"own_pr"`
	RepositoryPath   string                 `json:"repository_path,omitempty"`
	DeskState        domain.DeskState       `json:"desk_state"`
	Reason           string                 `json:"reason"`
}

func (item Item) SnapshotAge(now time.Time) time.Duration {
	if item.CapturedAt.IsZero() {
		return 0
	}
	age := now.Sub(item.CapturedAt)
	if age < 0 {
		return 0
	}
	return age
}

type Inbox struct {
	GeneratedAt  time.Time `json:"generated_at"`
	FromLiveLock bool      `json:"from_live_lock"`
	Items        []Item    `json:"items"`
	Warnings     []string  `json:"warnings,omitempty"`
}

func Refresh(ctx context.Context, cfg workspace.Config, dataDir string, discovery github.Discovery, now time.Time) (Inbox, error) {
	resolution, err := repos.Resolve(ctx, cfg.Scope)
	if err != nil {
		return Inbox{}, fmt.Errorf("resolve configured repositories: %w", err)
	}

	keys, warnings := discoverCandidateKeys(ctx, discovery, cfg)

	tracked, err := store.ListTrackedPullRequests(dataDir)
	if err != nil {
		return Inbox{}, err
	}
	for _, key := range tracked {
		keys = appendUniqueKey(keys, key.ToDomain())
	}

	snapshotStore := store.NewFilesystemSnapshotStore(dataDir)

	inbox := Inbox{GeneratedAt: now}
	inbox.Warnings = append(inbox.Warnings, warnings...)
	inbox.Warnings = append(inbox.Warnings, resolution.RootWarnings...)

	for _, key := range keys {
		schemaKey := store.ToPullRequestKeySchema(key)

		if scopeExcludesKey(cfg, schemaKey) {
			continue
		}
		if events, _, eventsErr := loadDomainEvents(dataDir, schemaKey); eventsErr == nil && domain.IsReleased(events) {
			continue
		}

		var previous *domain.PullRequestSnapshot
		if storedSchema, loadErr := snapshotStore.LoadSnapshot(schemaKey); loadErr == nil {
			if domainSnapshot, convertErr := storedSchema.ToDomain(); convertErr == nil {
				previous = &domainSnapshot
			}
		}

		snapshot, err := github.ObserveSnapshot(ctx, discovery, key, previous)
		if err != nil {
			inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: %v", key.String(), err))
			continue
		}

		if !snapshot.Stale {
			if saveErr := snapshotStore.SaveSnapshot(store.ToPRSnapshotSchema(snapshot)); saveErr != nil {
				inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: failed to persist snapshot: %v", key.String(), saveErr))
			}
		}

		item, ok, itemWarnings, itemErr := classifySnapshot(dataDir, cfg, resolution, schemaKey, snapshot, now)
		inbox.Warnings = append(inbox.Warnings, itemWarnings...)
		if itemErr != nil {
			inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: %v", key.String(), itemErr))
			continue
		}
		if ok {
			inbox.Items = append(inbox.Items, item)
		}
	}

	sortItems(inbox.Items)
	return inbox, nil
}

func LoadStored(dataDir string, cfg workspace.Config, now time.Time) (Inbox, error) {
	resolution, err := repos.Resolve(context.Background(), cfg.Scope)
	if err != nil {
		return Inbox{}, fmt.Errorf("resolve configured repositories: %w", err)
	}

	tracked, err := store.ListTrackedPullRequests(dataDir)
	if err != nil {
		return Inbox{}, err
	}

	snapshotStore := store.NewFilesystemSnapshotStore(dataDir)
	inbox := Inbox{GeneratedAt: now, FromLiveLock: true}
	inbox.Warnings = append(inbox.Warnings, resolution.RootWarnings...)

	for _, schemaKey := range tracked {
		if scopeExcludesKey(cfg, schemaKey) {
			continue
		}

		storedSchema, err := snapshotStore.LoadSnapshot(schemaKey)
		if err != nil {
			inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: no stored snapshot: %v", schemaKey.String(), err))
			continue
		}
		snapshot, err := storedSchema.ToDomain()
		if err != nil {
			inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: stored snapshot is corrupt: %v", schemaKey.String(), err))
			continue
		}

		item, ok, itemWarnings, itemErr := classifySnapshot(dataDir, cfg, resolution, schemaKey, snapshot, now)
		inbox.Warnings = append(inbox.Warnings, itemWarnings...)
		if itemErr != nil {
			inbox.Warnings = append(inbox.Warnings, fmt.Sprintf("%s: %v", schemaKey.String(), itemErr))
			continue
		}
		if ok {
			inbox.Items = append(inbox.Items, item)
		}
	}

	sortItems(inbox.Items)
	return inbox, nil
}

func loadDomainEvents(dataDir string, key store.PullRequestKeyV1) ([]domain.LifecycleEvent, []store.CorruptRecord, error) {
	eventSchemas, corrupt, err := store.NewFilesystemEventStore(dataDir).ListEvents(key)
	if err != nil {
		return nil, nil, err
	}
	events := make([]domain.LifecycleEvent, 0, len(eventSchemas))
	for _, schema := range eventSchemas {
		if event, ok := schema.ToLifecycleEvent(); ok {
			events = append(events, event)
		}
	}
	return events, corrupt, nil
}

func scopeExcludesKey(cfg workspace.Config, key store.PullRequestKeyV1) bool {
	identity := repos.Identity{
		Host:  strings.ToLower(key.Host),
		Owner: strings.ToLower(key.Owner),
		Name:  strings.ToLower(key.Repository),
	}
	excluded, _ := repos.MatchesExclusion(identity, cfg.Scope.Include, cfg.Scope.Exclude)
	return excluded
}

func classifySnapshot(dataDir string, cfg workspace.Config, resolution repos.Resolution, key store.PullRequestKeyV1, snapshot domain.PullRequestSnapshot, now time.Time) (Item, bool, []string, error) {
	if resolved, found := matchingResolvedRepository(resolution, key); found && resolved.Status == repos.StatusExcluded {
		return Item{}, false, nil, nil
	}

	var warnings []string

	runSchemas, corruptRuns, err := store.NewFilesystemRunStore(dataDir).ListRuns(key)
	if err != nil {
		return Item{}, false, nil, fmt.Errorf("load review runs: %w", err)
	}
	if len(corruptRuns) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s: %d stored review run(s) could not be read; classification may be based on incomplete history", key.String(), len(corruptRuns)))
	}
	runs := make([]domain.ReviewRun, 0, len(runSchemas))
	for _, schema := range runSchemas {
		run, _, convertErr := store.FromReviewRunSchema(schema)
		if convertErr != nil {
			continue
		}
		runs = append(runs, run)
	}

	events, corruptEvents, err := loadDomainEvents(dataDir, key)
	if err != nil {
		return Item{}, false, nil, fmt.Errorf("load review events: %w", err)
	}
	if len(corruptEvents) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s: %d stored review event(s) could not be read; classification may be based on incomplete history", key.String(), len(corruptEvents)))
	}

	repositoryAvailable, repositoryPath := repositoryAvailability(resolution, key)

	classification := domain.Classify(domain.ClassificationInput{
		Snapshot:            snapshot,
		RepositoryAvailable: repositoryAvailable,
		ExpectedUser:        cfg.Identity.ExpectedUser,
		ReviewOwnPRs:        cfg.Behavior.OwnPRPolicy == workspace.OwnPRPolicyCommentOnly,
		SettleTime:          cfg.Behavior.SettleTime.AsDuration(),
		Now:                 now,
		Runs:                runs,
		Events:              events,
	})

	if !classification.Tracked {
		return Item{}, false, warnings, nil
	}

	item := Item{
		Key:              key,
		URL:              snapshot.URL,
		Title:            snapshot.Title,
		Author:           snapshot.Author,
		State:            string(snapshot.State),
		Draft:            snapshot.Draft,
		HeadObjectID:     snapshot.HeadObjectID,
		BaseObjectID:     snapshot.BaseObjectID,
		ReviewDecision:   snapshot.ReviewDecision,
		CheckRollupState: snapshot.CheckRollupState,
		MergeState:       snapshot.MergeState,
		UpdatedAt:        snapshot.UpdatedAt,
		CapturedAt:       snapshot.CapturedAt,
		SnapshotStale:    snapshot.Stale,
		OwnPR:            cfg.Identity.ExpectedUser != "" && strings.EqualFold(snapshot.Author, cfg.Identity.ExpectedUser),
		RepositoryPath:   repositoryPath,
		DeskState:        classification.State,
		Reason:           classification.Reason,
	}
	return item, true, warnings, nil
}

func repositoryAvailability(resolution repos.Resolution, key store.PullRequestKeyV1) (bool, string) {
	resolved, found := matchingResolvedRepository(resolution, key)
	if !found {
		return false, ""
	}
	return resolved.Status == repos.StatusReviewable, resolved.LocalPath
}

func matchingResolvedRepository(resolution repos.Resolution, key store.PullRequestKeyV1) (repos.ResolvedRepository, bool) {
	for _, repository := range resolution.Repositories {
		if strings.EqualFold(repository.Identity.Host, key.Host) &&
			strings.EqualFold(repository.Identity.Owner, key.Owner) &&
			strings.EqualFold(repository.Identity.Name, key.Repository) {
			return repository, true
		}
	}
	return repos.ResolvedRepository{}, false
}

func discoverCandidateKeys(ctx context.Context, discovery github.Discovery, cfg workspace.Config) ([]domain.PullRequestKey, []string) {
	var keys []domain.PullRequestKey
	var warnings []string

	search := func(query github.SearchQuery, label string) {
		found, err := discovery.Search(ctx, query)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", label, err))
			return
		}
		for _, key := range found {
			keys = appendUniqueKey(keys, key)
		}
	}

	if cfg.Identity.ExpectedUser != "" {
		if len(cfg.Scope.Organizations) == 0 {
			search(github.SearchQuery{Kind: github.SearchKindReviewRequested, Login: cfg.Identity.ExpectedUser}, "review-requested search (no organization scope configured)")
			search(github.SearchQuery{Kind: github.SearchKindAuthored, Login: cfg.Identity.ExpectedUser}, "authored search (no organization scope configured)")
		}
		for _, org := range cfg.Scope.Organizations {
			search(github.SearchQuery{Kind: github.SearchKindReviewRequested, Organization: org, Login: cfg.Identity.ExpectedUser}, fmt.Sprintf("review-requested search in %s", org))
			search(github.SearchQuery{Kind: github.SearchKindAuthored, Organization: org, Login: cfg.Identity.ExpectedUser}, fmt.Sprintf("authored search in %s", org))
		}
	}
	for _, team := range cfg.Scope.Teams {
		if strings.Contains(team, "/") {
			search(github.SearchQuery{Kind: github.SearchKindTeamReviewRequested, Team: team}, fmt.Sprintf("team-requested search for %s", team))
			continue
		}
		if len(cfg.Scope.Organizations) == 0 {
			warnings = append(warnings, fmt.Sprintf("scope.teams: %q is not qualified as org/team and scope.organizations is empty; skipping this team search", team))
			continue
		}
		for _, org := range cfg.Scope.Organizations {
			search(github.SearchQuery{Kind: github.SearchKindTeamReviewRequested, Team: team, Organization: org}, fmt.Sprintf("team-requested search for %s/%s", org, team))
		}
	}

	return keys, warnings
}

func appendUniqueKey(keys []domain.PullRequestKey, key domain.PullRequestKey) []domain.PullRequestKey {
	for _, existing := range keys {
		if sameRepositoryIdentity(existing, key) && existing.Number == key.Number {
			return keys
		}
	}
	return append(keys, key)
}

func sameRepositoryIdentity(a, b domain.PullRequestKey) bool {
	return strings.EqualFold(a.Host, b.Host) && strings.EqualFold(a.Owner, b.Owner) && strings.EqualFold(a.Repository, b.Repository)
}

var DeskStateDisplayOrder = []domain.DeskState{
	domain.DeskStateFailed,
	domain.DeskStateFindingsReady,
	domain.DeskStateDecisionReady,
	domain.DeskStateNeedsReview,
	domain.DeskStateNeedsRereview,
	domain.DeskStateRunning,
	domain.DeskStateQueued,
	domain.DeskStateStale,
	domain.DeskStateWaitingOnOthers,
	domain.DeskStateRepositoryUnavailable,
	domain.DeskStateResolved,
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].DeskState != items[j].DeskState {
			return deskStateRank(items[i].DeskState) < deskStateRank(items[j].DeskState)
		}
		return items[i].Key.String() < items[j].Key.String()
	})
}

func deskStateRank(state domain.DeskState) int {
	if i := slices.Index(DeskStateDisplayOrder, state); i >= 0 {
		return i
	}
	return len(DeskStateDisplayOrder)
}
