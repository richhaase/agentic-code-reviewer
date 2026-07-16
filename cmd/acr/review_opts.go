package main

import "github.com/richhaase/agentic-code-reviewer/internal/config"

// ReviewOpts holds all resolved configuration and runtime flags needed to
// execute a review. It bundles config.ResolvedConfig (from flag/env/file
// resolution) with CLI-only flags that don't participate in config resolution.
//
// This struct replaces direct reads of package-level variables in review.go
// and pr_submit.go, making those functions testable in isolation.
type ReviewOpts struct {
	config.ResolvedConfig

	// CLI-only flags (not part of config resolution)
	Verbose         bool
	Local           bool
	AutoYes         bool
	PRNumber        string // Explicit --pr flag value (empty if not set)
	DetectedPR      string // Auto-detected or explicit PR number for feedback summarization
	WorktreeBranch  string // Explicit --worktree-branch flag value
	UseRefFile      bool
	ExcludePatterns []string
	WorkDir         string // Worktree path (empty = current directory)

	// Watch-mode policy: post every result as a comment review only, never
	// request changes or approve (and never run the pre-approval CI check).
	ForcePostComment bool
	// ExpectedHeadSHA, when set, makes submission verify the PR head still
	// matches the commit that was reviewed and skip posting if it moved.
	ExpectedHeadSHA string
	// Outcome, when non-nil, receives a record of what the cycle posted.
	Outcome *CycleOutcome
}
