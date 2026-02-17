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
}
