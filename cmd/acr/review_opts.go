package main

import "github.com/richhaase/agentic-code-reviewer/internal/config"

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
	WorkDir         string

	ForcePostComment bool
	ExpectedHeadSHA  string
	Outcome          *CycleOutcome
}
