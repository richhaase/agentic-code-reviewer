# Changelog

All notable changes to ACR are documented in this file.

This changelog is generated from git tag annotations.

## [v0.9.1] - 2026-01-29

Bug fixes and code quality improvements

### Fixed
- Continue parsing after recoverable errors - parser errors no longer discard remaining findings (#87)
- Capture PID before process group kill to prevent race conditions (#88)
- Log Close() errors instead of silently ignoring them (#91)

### Changed
- Replace io.Reader with ExecutionResult type for guaranteed cleanup API (#86)
- Extract common executeCommand helper, reducing ~143 lines of duplicated code (#89)
- Rename github_actions.go to pr_submit.go for clarity (#98)
- Extract shared non-finding detection logic for consistent behavior (#100)

### Added
- Compile-time interface compliance checks for Agent implementations (#96)
- Documentation for magic numbers (RefFileSizeThreshold, DefaultThreshold, maxFindingPreviewLength) (#94)
- Documentation for fetch/noFetch flag interaction (#95)

### Removed
- Unused joinInts function (#99)
- Old plan documents

## [v0.9.0] - 2026-01-28

feat: add --pr flag to review PRs by number

New Features:
- Add --pr flag to review PRs by number using temporary worktrees
- Auto-detect PR base ref from GitHub
- Dynamic remote detection for fork workflows
- Support for various base ref formats (tags, SHAs, branches with slashes)

New Functions:
- github.GetPRBranch and GetPRBaseRef for PR metadata retrieval
- git.CreateWorktreeFromPR for temporary worktree creation
- git.FetchBaseRef with intelligent ref qualification

Documentation:
- Add CHANGELOG.md generated from tag annotations

## [v0.8.1] - 2026-01-28

v0.8.1

Features:
- Enhanced FP filter with few-shot examples and chain-of-thought reasoning
- Claude and Gemini agents now fully supported (no longer experimental)

Documentation:
- Added Quick Start section
- Improved prerequisites with proper install links
- Fixed Codex install instructions (brew install codex)
- Updated Mermaid diagram with PR posting flow

## [v0.8.0] - 2026-01-28

v0.8.0

Features:
- Support reviewing PRs from forked repositories (#85)

Documentation:
- Replace ASCII diagram with Mermaid flowchart
- Document v0.7.0 features (fetch, false positive filter)

Chores:
- Stop tracking .claude/settings.local.json

## [v0.7.0] - 2026-01-27

v0.7.0: Remote base fetch and false positive filtering

Features:
- Fetch remote base branch before diff comparison
- Add false positive filter for code review findings

Fixes:
- Prevent invalid origin/ prefix on fully-qualified refs
- Add input validation to isTag function (gosec)
- Preserve tag refs without origin/ prefix after fetch
- Apply De Morgan's law to satisfy staticcheck

Chores:
- Remove beads integration
- Remove unused FetchRemote field and clarify FetchResult naming

## [v0.6.0] - 2026-01-26

v0.6.0: Add false positive filter

Features:
- Add LLM-based false positive filter for code review findings
- Configurable threshold (default 75) via CLI, config, or env vars
- New flags: --no-fp-filter, --fp-threshold
- New config: fp_filter.enabled, fp_filter.threshold
- New env vars: ACR_FP_FILTER, ACR_FP_THRESHOLD
- Enabled by default; disable with --no-fp-filter

Chores:
- Initialize beads issue tracking
- Remove tlog

## [v0.5.1] - 2026-01-24

v0.5.1 - Improved GitHub PR detection

Fixes:
- Distinguish auth failures from missing PRs in PR detection (#80)
  - Better error messages when GitHub authentication fails
  - Prevents confusing error states in CI environments

## [v0.5.0] - 2026-01-24

v0.5.0 - Large Diff Handling

Features:
- Add ref-file pattern for handling large diffs across all providers
- Add large input handling to ExecuteSummary for all providers

Bug Fixes:
- Fix TTY check to use stdin instead of stdout before prompting
- Fix WriteInputToTempFile to use working directory for sandboxed agents
- Fix custom prompts being dropped in ref-file mode
- Fix misleading comment about Gemini file reading capability

Maintenance:
- Mark uuid dependency as direct in go.mod
- Replace log.Printf with fmt.Fprintf for stderr output
- Add debug logging for ref-file mode usage

## [v0.4.2] - 2026-01-23

v0.4.2

Improvements:
- Remove raw markdown preview before PR submission prompt
- LGTM now displays as "✓ LGTM (2/2 reviewers)" without extra blank lines
- Fix line-clearing to use ANSI escape codes, fixing misalignment on narrow terminals

## [v0.4.1] - 2026-01-23

v0.4.1: PR review UX improvements and codex parser fixes

Fixes:
- Fix codex summarizer JSON output parsing (#64)
  - Properly handle streaming events with item.completed guard
  - Fail immediately on decode errors instead of silently ignoring
  - Performance optimizations with io.MultiReader

Improvements:
- Improve PR review posting UX (#65)
  - Post findings as PR reviews instead of comments
  - Add interactive prompt for review type (request changes/comment/skip)
  - Add 3-way choice for LGTM (approve/comment/skip)
  - Remove redundant -n/--no flag

## [v0.4.0] - 2026-01-21

v0.4.0 - Multi-agent Reviewer Cohorts

Features:
- Add multi-agent reviewer cohorts with round-robin distribution

Fixes:
- Fix default timeout and document multi-agent round-robin feature
- Fix inconsistent error wrapping - use %w instead of %s

Improvements:
- Add defensive checks and improve test coverage for cohorts
- Improve cohort feature error handling and separation of concerns
- Enhance README with additional reviewer tools

## [v0.3.0] - 2026-01-19

v0.3.0: Multi-Agent Support & Custom Prompts

Features:
- Add Claude and Gemini as alternative review backends
- Add --agent flag for selecting review backend
- Add --prompt and --prompt-file flags for custom review prompts
- Add --summarizer-agent flag for independent summarizer configuration
- Add BATS evaluation harness for agent comparison testing

Improvements:
- Refactor agent package with unified interface (ExecuteReview, ExecuteSummary)
- Add SummaryParser interface for agent-specific summary output parsing
- Pass git diff context to Claude/Gemini agents
- Pipe Claude prompts via stdin to avoid ARG_MAX limitations
- Capture stderr from agent subprocesses for diagnostics
- Improve Ctrl+C handling to properly kill process groups
- Consolidate agent factory into registration map
- Replace justfile with Makefile

Bug Fixes:
- Fix git diff argument injection vulnerability
- Fix agent exit codes being masked as success
- Fix parser error causing infinite loop
- Fix parse error double-counting in runner
- Fix git diff argument order regression
- Fix Gemini parser for multi-line JSON output
- Fix Claude summarizer JSON parsing
- Fix local mode (-l) showing interactive selector prompt
- Add thread-safe cmdReader.Close() with sync.Once
- Add scanner error checking after parse loops

## [v0.2.3] - 2026-01-15

Fixes an embarassing module naming issue.

## [v0.2.2] - 2026-01-15

allow posting LGTM comment on self-authored PRs

## [v0.2.1] - 2026-01-13

v0.2.1: Change PR prompt default to Yes

- PR confirmation prompt now defaults to Y instead of N
- Add staticcheck recipe and CI job
- Add vet CI job
- Upgrade Go to 1.24.6 to fix stdlib vulnerabilities
- Configure gosec exclusions for CLI false positives

## [v0.2.0] - 2026-01-13

v0.2.0

Breaking Changes:
- Renamed environment variables from REVIEW_* to ACR_* prefix
- Removed ACR_EXCLUDE_PATTERNS environment variable (use config file instead)

New Features:
- Expanded .acr.yaml config schema to support all main options
- Added warnings for unknown keys in config file with typo suggestions
- Added warnings when environment variable values fail to parse

Bug Fixes:
- Fixed levenshtein distance to operate on runes instead of bytes (proper unicode support)
- Fixed exitCodeError.Error() to return meaningful error messages
- Made color configuration thread-safe

Improvements:
- Added staticcheck to CI workflow and quality checks
- Significant test coverage improvements (summarizer, git, terminal, cmd/acr)
- Refactored cmd/acr/main.go into smaller focused files
- Replaced magic numbers with named constants
- Added context.Context to GitHub package functions
- Various code quality improvements

## [v0.1.3] - 2026-01-13

Adds short indicators

## [v0.1.2] - 2026-01-13

Adds -c|--concurrency flag

Sets max concurrent reviewer to avoid rate limiting with many reviewers, or
large PRs.

## [v0.1.1] - 2026-01-13

Fix panic when stdin returns empty response

## [v0.1.0] - 2026-01-13

Adds the ability to filter findings posted to PRs:

* Interactive UX for selecting findings manually.
* Pattern matching regex in finding messages for automated filtering

## [v0.0.3] - 2026-01-09

Splice strain f00c017c: create useful readme.md and claude.md files

## [v0.0.2] - 2026-01-09

Fix misspelling: cancelled -> canceled

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>

## [v0.0.1] - 2026-01-09

Initial version of acr converted from python script in my dotfiles

