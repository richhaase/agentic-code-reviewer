package repos

import (
	"context"
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/config"
)

func TrustedSource(ctx context.Context, resolved ResolvedRepository) (config.Source, error) {
	if resolved.Status != StatusReviewable {
		return nil, fmt.Errorf("repository %s is not reviewable (%s): %s", resolved.Identity, resolved.Status, resolved.Reason)
	}

	return config.ResolveTrustedSource(ctx, config.TrustedSourceRequest{
		RepositoryRoot: resolved.LocalPath,
		Remote:         resolved.Remote,
		Policy:         config.CanonicalRemoteDefault,
	})
}
