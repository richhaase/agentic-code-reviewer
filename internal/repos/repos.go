package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/workspace"
)

const ReasonRepositoryUnavailable = "repository_unavailable"

type Status string

const (
	StatusReviewable Status = "reviewable"
	StatusMissing    Status = "missing"
	StatusAmbiguous  Status = "ambiguous"
	StatusExcluded   Status = "excluded"
	StatusInvalid    Status = "invalid"
)

type Identity struct {
	Owner string
	Name  string
}

func (id Identity) String() string {
	return id.Owner + "/" + id.Name
}

type ResolvedRepository struct {
	Identity  Identity
	Status    Status
	LocalPath string
	Remote    string
	Reason    string
}

type Resolution struct {
	Repositories []ResolvedRepository
	RootWarnings []string
}

func Resolve(ctx context.Context, scope workspace.ScopeConfig) (Resolution, error) {
	discovered := map[Identity][]string{}
	var rootWarnings []string

	for _, root := range scope.RepositoryRoots {
		candidates, warning := scanRoot(root)
		if warning != "" {
			rootWarnings = append(rootWarnings, warning)
		}
		for _, dir := range candidates {
			identity, ok, err := repositoryIdentity(ctx, dir)
			if err != nil {
				rootWarnings = append(rootWarnings, fmt.Sprintf("%s: %v", dir, err))
				continue
			}
			if !ok {
				continue
			}
			discovered[identity] = append(discovered[identity], dir)
		}
	}

	results := map[Identity]ResolvedRepository{}
	for identity, paths := range discovered {
		sort.Strings(paths)
		if len(paths) > 1 {
			results[identity] = ResolvedRepository{
				Identity: identity,
				Status:   StatusAmbiguous,
				Reason:   fmt.Sprintf("multiple local clones claim %s: %s", identity, strings.Join(paths, ", ")),
			}
			continue
		}
		results[identity] = ResolvedRepository{
			Identity:  identity,
			Status:    StatusReviewable,
			LocalPath: paths[0],
			Remote:    "origin",
		}
	}

	for raw, localPath := range scope.PathOverrides {
		identity, err := parseIdentity(raw)
		if err != nil {
			rootWarnings = append(rootWarnings, fmt.Sprintf("path_overrides: %v", err))
			continue
		}
		results[identity] = resolvePathOverride(identity, localPath)
	}

	for identity, result := range results {
		if excluded, reason := matchesExclusion(identity, scope.Include, scope.Exclude); excluded {
			result.Status = StatusExcluded
			result.Reason = reason
			results[identity] = result
		}
	}

	identities := make([]Identity, 0, len(results))
	for identity := range results {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		return identities[i].String() < identities[j].String()
	})

	repositories := make([]ResolvedRepository, 0, len(identities))
	for _, identity := range identities {
		repositories = append(repositories, results[identity])
	}

	return Resolution{Repositories: repositories, RootWarnings: rootWarnings}, nil
}

func resolvePathOverride(identity Identity, localPath string) ResolvedRepository {
	info, err := os.Stat(localPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ResolvedRepository{
			Identity:  identity,
			Status:    StatusMissing,
			LocalPath: localPath,
			Reason:    fmt.Sprintf("%s: local path %s does not exist (%s)", identity, localPath, ReasonRepositoryUnavailable),
		}
	case err != nil:
		return ResolvedRepository{
			Identity:  identity,
			Status:    StatusInvalid,
			LocalPath: localPath,
			Reason:    fmt.Sprintf("%s: failed to inspect %s: %v", identity, localPath, err),
		}
	case !info.IsDir():
		return ResolvedRepository{
			Identity:  identity,
			Status:    StatusInvalid,
			LocalPath: localPath,
			Reason:    fmt.Sprintf("%s: %s is not a directory", identity, localPath),
		}
	case !isGitRepository(localPath):
		return ResolvedRepository{
			Identity:  identity,
			Status:    StatusInvalid,
			LocalPath: localPath,
			Reason:    fmt.Sprintf("%s: %s is not a git repository", identity, localPath),
		}
	default:
		return ResolvedRepository{
			Identity:  identity,
			Status:    StatusReviewable,
			LocalPath: localPath,
			Remote:    "origin",
		}
	}
}

func parseIdentity(raw string) (Identity, error) {
	segments := strings.Split(raw, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return Identity{}, fmt.Errorf("invalid owner/repo identity %q", raw)
	}
	return Identity{Owner: strings.ToLower(segments[0]), Name: strings.ToLower(segments[1])}, nil
}

func matchesExclusion(identity Identity, include, exclude []string) (bool, string) {
	name := strings.ToLower(identity.String())

	for _, pattern := range exclude {
		if matched, _ := path.Match(strings.ToLower(pattern), name); matched {
			return true, fmt.Sprintf("%s matches exclude pattern %q", identity, pattern)
		}
	}

	if len(include) == 0 {
		return false, ""
	}
	for _, pattern := range include {
		if matched, _ := path.Match(strings.ToLower(pattern), name); matched {
			return false, ""
		}
	}
	return true, fmt.Sprintf("%s does not match any configured include pattern", identity)
}

func scanRoot(root string) ([]string, string) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Sprintf("repository root %s: %v", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Sprintf("repository root %s is not a directory", root)
	}

	candidates := []string{root}
	entries, err := os.ReadDir(root)
	if err != nil {
		return candidates, fmt.Sprintf("repository root %s: %v", root, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			candidates = append(candidates, filepath.Join(root, entry.Name()))
		}
	}
	return candidates, ""
}

func repositoryIdentity(ctx context.Context, dir string) (Identity, bool, error) {
	if !isGitRepository(dir) {
		return Identity{}, false, nil
	}

	remoteURL, err := git.RemoteURL(ctx, dir, "origin")
	if err != nil {
		return Identity{}, false, nil
	}

	_, owner, name, ok := github.ParseRemoteURL(remoteURL)
	if !ok {
		return Identity{}, false, nil
	}
	return Identity{Owner: owner, Name: name}, true, nil
}

func isGitRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
