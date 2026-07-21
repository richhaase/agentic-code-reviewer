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
	remote := github.GetRepoRemote(ctx)
	remoteExists, err := git.RemoteExists(ctx, repositoryRoot, remote)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect trusted review configuration remote: %w", err)
	}
	if remoteExists {
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
