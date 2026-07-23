package main

import (
	"github.com/richhaase/agentic-code-reviewer/internal/config"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type ReviewOpts struct {
	config.ResolvedConfig

	Verbose         bool
	Local           bool
	AutoYes         bool
	PRNumber        string
	DetectedPR      string
	WorktreeBranch  string
	UseRefFile      bool
	ExcludePatterns []string
	RepositoryRoot  string
	WorkDir         string
	PullRequest     *domain.PullRequestKey
	ConfigSource    config.SourceIdentity
	Trigger         domain.ReviewTrigger

	ForcePostComment bool
	ExpectedHeadSHA  string
	Outcome          *CycleOutcome
}
