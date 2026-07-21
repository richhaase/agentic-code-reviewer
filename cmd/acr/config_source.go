package main

import (
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/git"
	"github.com/richhaase/agentic-code-reviewer/internal/github"
)

func resolveTrustedReviewConfigSource(ctx context.Context, disabled bool) (config.Source, error) {
	if disabled {
		return config.ResolveTrustedSource(ctx, config.TrustedSourceRequest{Disabled: true})
	}

	repositoryRoot, err := git.GetRoot()
	if err != nil {
		return nil, err
	}
	remotes, err := git.Remotes(ctx, repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect trusted review configuration remote: %w", err)
	}
	remote, hasRemote, err := selectTrustedReviewConfigRemote(ctx, repositoryRoot, remotes)
	if err != nil {
		return nil, err
	}
	if hasRemote {
		return config.ResolveTrustedSource(ctx, config.TrustedSourceRequest{
			RepositoryRoot: repositoryRoot,
			Remote:         remote,
			Policy:         config.CanonicalRemoteDefault,
		})
	}
	return config.ResolveTrustedSource(ctx, config.TrustedSourceRequest{
		RepositoryRoot: repositoryRoot,
		Branch:         "main",
		Policy:         config.CanonicalNamedBranch,
	})
}

func selectTrustedReviewConfigRemote(ctx context.Context, repositoryRoot string, remotes []string) (string, bool, error) {
	if len(remotes) == 0 {
		return "", false, nil
	}
	if len(remotes) == 1 {
		return remotes[0], true, nil
	}
	remote, err := github.FindRepoRemote(ctx, repositoryRoot)
	if err != nil {
		return "", false, fmt.Errorf("failed to select a canonical remote from %v: %w", remotes, err)
	}
	for _, configured := range remotes {
		if configured == remote {
			return remote, true, nil
		}
	}
	return "", false, fmt.Errorf("selected canonical remote %q is not configured", remote)
}
