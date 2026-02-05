# PR Feedback Summarizer

## Overview

Add a PR feedback summarizer agent that reads the full PR context (description + all comments) and produces a prose summary of issues that have been discussed, dismissed, or explained as intentional. This summary feeds into the existing FP filter to improve filtering accuracy when ACR re-runs on the same PR.

## Problem

ACR writes comments to GitHub PRs but never reads responses. When ACR re-runs on a PR (e.g., after new commits), it may report the same findings that humans have already dismissed or explained. This creates noise and erodes trust in the tool.

## Solution

Run a PR feedback summarizer agent in parallel with the reviewers. This agent:

1. Fetches the PR description and all comments
2. Summarizes what issues have been acknowledged, dismissed, or explained
3. Passes this summary to the FP filter as additional context

The FP filter's LLM evaluation then considers both the finding content AND the prior human feedback when scoring.

## Architecture

```
┌─────────────────────┐     ┌─────────────────────┐
│   PR Feedback       │     │   Reviewers (N)     │
│   Summarizer Agent  │     │                     │
└──────────┬──────────┘     └──────────┬──────────┘
           │                           │
           │  (parallel)               │  (parallel)
           │                           │
           ▼                           ▼
         ┌───────────────────────────────┐
         │      Aggregate Findings       │
         └───────────────┬───────────────┘
                         │
                         ▼
         ┌───────────────────────────────┐
         │         Summarizer            │
         │     (cluster findings)        │
         └───────────────┬───────────────┘
                         │
                         ▼
         ┌───────────────────────────────┐
         │         FP Filter             │
         │  (scores findings, informed   │
         │   by prior feedback summary)  │
         └───────────────┬───────────────┘
                         │
                         ▼
                      Output
```

## Design Decisions

### Read all PR content, not just ACR comments

The summarizer reads the entire PR context - description and all comments from any author. This is simpler than tracking ACR-specific comments with markers, and captures more context:

- PR description may explain why certain patterns are intentional
- Human reviewers may have dismissed issues before ACR ran
- Author replies to any reviewer provide context

### Prose summary output

The summarizer outputs natural language, not structured data. The FP filter's LLM interprets this prose alongside its own analysis. This handles nuance well - a reply like "we discussed this in standup, leaving as-is" is understood as dismissal without needing keyword matching.

### LLM interprets feedback intent

Replies are passed as context; the LLM determines whether feedback indicates dismissal ("this is intentional") vs acknowledgment ("good catch, will fix"). No keyword-based classification.

### Semantic matching for findings

Line numbers shift between commits. The LLM performs semantic matching between prior feedback and current findings, understanding that "unchecked error in database connection" is the same issue even if line numbers changed.

### Graceful degradation

If the PR feedback summarizer fails or times out, the review continues without it. The FP filter receives an empty string and behaves exactly as today.

### Filtered like any other FP

Findings informed by prior feedback go through the same FP scoring. They appear in `Result.Removed` with reasoning like "Previously discussed: author indicated this is intentional." No separate category needed.

## Implementation

### New package: `internal/feedback/`

**fetch.go** - GitHub data fetching via gh CLI:

```go
// PRContext holds the PR description and all comments
type PRContext struct {
    Number      string
    Description string
    Comments    []Comment
}

type Comment struct {
    Author     string
    Body       string
    IsResolved bool
    Replies    []Reply
}

type Reply struct {
    Author string
    Body   string
}

// FetchPRContext retrieves the PR description and all comments
func FetchPRContext(ctx context.Context, prNumber string) (*PRContext, error)
```

GitHub API calls:
- `gh pr view {number} --json body` for PR description
- `gh api repos/{owner}/{repo}/pulls/{number}/comments` for review comments
- GraphQL query for review threads with `isResolved` status

**summarizer.go** - Agent execution:

```go
// Summarizer summarizes PR feedback for the FP filter
type Summarizer struct {
    agentName string
    verbose   bool
}

// Summarize fetches PR context and returns a prose summary of prior feedback
func (s *Summarizer) Summarize(ctx context.Context, prNumber string) (string, error)
```

**prompt.go** - Summarization prompt:

```
You are analyzing a GitHub PR to summarize feedback about code review findings.

## Input
- PR description
- All comments and replies
- Conversation resolved status

## Task
Summarize any feedback that indicates code review issues have been:
- Dismissed as false positives
- Explained as intentional
- Acknowledged but deferred
- Marked as resolved

Focus on feedback relevant to code quality findings (bugs, security, error handling).
Ignore unrelated discussion (feature questions, deployment, etc.).

## Output
Write a concise prose summary. If no relevant feedback exists, respond with
"No prior feedback on code review findings."

Example output:
"The PR description notes this is a prototype and error handling will be added
in a follow-up. A thread about the unchecked error in auth.go was resolved after
the author explained errors are validated upstream. A reviewer's concern about
SQL injection was acknowledged - author replied 'good catch, fixing now.'"
```

### Modify: `internal/fpfilter/filter.go`

Update the Apply method signature:

```go
// Before
func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings) (*Result, error)

// After
func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings, priorFeedback string) (*Result, error)
```

When `priorFeedback` is non-empty, append it to the evaluation prompt.

### Modify: `internal/fpfilter/prompt.go`

Add a template section for prior feedback:

```go
const priorFeedbackSection = `

## Prior Feedback Context

The following feedback has been gathered from the PR discussion:

%s

Consider this context when scoring findings. If a finding matches something
that has been explicitly dismissed or explained as intentional, weight that
heavily toward false positive (higher fp_score). If feedback indicates an
issue was acknowledged as valid ("good catch, will fix"), weight toward
true positive (lower fp_score).
`
```

### Modify: `cmd/acr/review.go`

Orchestration changes:

```go
// Start PR feedback summarizer in parallel with reviewers
var priorFeedback string
var feedbackErr error
var feedbackWg sync.WaitGroup

if prNumber != "" && !cfg.NoPRFeedback {
    feedbackWg.Add(1)
    go func() {
        defer feedbackWg.Done()
        summarizer := feedback.NewSummarizer(cfg.Agent, cfg.Verbose)
        priorFeedback, feedbackErr = summarizer.Summarize(ctx, prNumber)
    }()
}

// ... run reviewers ...

// Wait for feedback summarizer
feedbackWg.Wait()
if feedbackErr != nil {
    logger.Warn("PR feedback summarizer failed: %v", feedbackErr)
    // Continue without feedback
}

// ... aggregate, summarize ...

// Pass feedback to FP filter
result, err := fpFilter.Apply(ctx, grouped, priorFeedback)
```

### CLI flags

```go
rootCmd.Flags().BoolVar(&cfg.NoPRFeedback, "no-pr-feedback",
    getEnvBool("ACR_NO_PR_FEEDBACK", false),
    "Disable reading PR comments for feedback context")

rootCmd.Flags().StringVar(&cfg.FeedbackAgent, "feedback-agent",
    getEnvStr("ACR_FEEDBACK_AGENT", ""),
    "Agent for PR feedback summarization (default: same as --agent)")
```

### Config file (.acr.yaml)

```yaml
pr_feedback:
  enabled: true          # default true, set false to disable
  agent: claude          # optional, defaults to main agent
```

## Behavior Matrix

| Scenario | Feedback Summarizer | FP Filter Receives |
|----------|--------------------|--------------------|
| Local review (no PR) | Does not run | Empty string |
| PR review, first run | Runs, likely minimal output | Summary (possibly empty) |
| PR review, re-run with feedback | Runs, produces summary | Summary of prior feedback |
| PR review, `--no-pr-feedback` | Does not run | Empty string |
| PR review, summarizer fails | Logs warning | Empty string |
| PR review, no comments | Runs, returns minimal | "No prior feedback..." |

## Testing

### Unit tests

- `internal/feedback/fetch_test.go` - Mock gh CLI responses
- `internal/feedback/summarizer_test.go` - Test prompt construction and parsing
- `internal/fpfilter/filter_test.go` - Test with/without priorFeedback parameter

### Integration tests

- End-to-end test with mock PR data
- Verify findings with prior "dismissed" feedback get higher FP scores
- Verify findings with prior "acknowledged as valid" feedback get lower FP scores

## Future Considerations

Not in scope for this design, but potential future enhancements:

- **Cross-PR learning**: Remember feedback patterns across PRs in a repo
- **Caching**: Cache PR context to avoid re-fetching on rapid re-runs
- **Selective fetching**: Only fetch comments since last ACR run (needs timestamp tracking)

## Files Changed

New files:
- `internal/feedback/fetch.go`
- `internal/feedback/summarizer.go`
- `internal/feedback/prompt.go`
- `internal/feedback/fetch_test.go`
- `internal/feedback/summarizer_test.go`

Modified files:
- `internal/fpfilter/filter.go` - Add priorFeedback parameter
- `internal/fpfilter/prompt.go` - Add feedback context section
- `internal/fpfilter/filter_test.go` - Update tests for new parameter
- `cmd/acr/review.go` - Orchestration, new flags
- `internal/config/config.go` - New config fields
