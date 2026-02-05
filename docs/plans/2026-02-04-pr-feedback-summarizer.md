# PR Feedback Summarizer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a PR feedback summarizer agent that reads PR context and provides prior feedback to the FP filter.

**Architecture:** A new `internal/feedback` package fetches PR data via `gh api` and summarizes it via an LLM agent. This runs in parallel with reviewers and passes its output to the FP filter.

**Tech Stack:** Go, gh CLI, existing agent abstraction

---

## Task 1: Create feedback package with fetch types

**Files:**
- Create: `internal/feedback/fetch.go`
- Test: `internal/feedback/fetch_test.go`

**Step 1: Write the test for PRContext types**

```go
// internal/feedback/fetch_test.go
package feedback

import "testing"

func TestPRContext_HasContent(t *testing.T) {
	tests := []struct {
		name     string
		ctx      PRContext
		expected bool
	}{
		{
			name:     "empty context",
			ctx:      PRContext{},
			expected: false,
		},
		{
			name:     "description only",
			ctx:      PRContext{Description: "Fix bug"},
			expected: true,
		},
		{
			name:     "comments only",
			ctx:      PRContext{Comments: []Comment{{Body: "LGTM"}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.HasContent(); got != tt.expected {
				t.Errorf("HasContent() = %v, want %v", got, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/feedback/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/feedback/fetch.go
// Package feedback provides PR feedback summarization for false positive filtering.
package feedback

// PRContext holds the PR description and all comments.
type PRContext struct {
	Number      string
	Description string
	Comments    []Comment
}

// Comment represents a PR comment with its replies.
type Comment struct {
	Author     string
	Body       string
	IsResolved bool
	Replies    []Reply
}

// Reply represents a reply to a comment.
type Reply struct {
	Author string
	Body   string
}

// HasContent returns true if the context has any content worth summarizing.
func (p *PRContext) HasContent() bool {
	return p.Description != "" || len(p.Comments) > 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/feedback/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/feedback/fetch.go internal/feedback/fetch_test.go
git commit -m "feat(feedback): add PRContext types for PR data"
```

---

## Task 2: Add FetchPRContext function

**Files:**
- Modify: `internal/feedback/fetch.go`
- Modify: `internal/feedback/fetch_test.go`

**Step 1: Write test for FetchPRContext**

```go
// Add to internal/feedback/fetch_test.go

func TestFetchPRContext_NoPRNumber(t *testing.T) {
	ctx := context.Background()
	_, err := FetchPRContext(ctx, "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/feedback/... -v -run TestFetchPRContext`
Expected: FAIL - function doesn't exist

**Step 3: Write implementation**

```go
// Add to internal/feedback/fetch.go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FetchPRContext retrieves the PR description and all comments via gh CLI.
func FetchPRContext(ctx context.Context, prNumber string) (*PRContext, error) {
	if prNumber == "" {
		return nil, errors.New("PR number is required")
	}

	result := &PRContext{Number: prNumber}

	// Fetch PR description
	desc, err := fetchPRDescription(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR description: %w", err)
	}
	result.Description = desc

	// Fetch comments
	comments, err := fetchPRComments(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR comments: %w", err)
	}
	result.Comments = comments

	return result, nil
}

func fetchPRDescription(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "body", "--jq", ".body")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// prCommentResponse represents a comment from gh api.
type prCommentResponse struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

func fetchPRComments(ctx context.Context, prNumber string) ([]Comment, error) {
	// Fetch review comments (comments on code)
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/comments", prNumber),
		"--jq", ".[].body")
	out, err := cmd.Output()
	if err != nil {
		// No comments is not an error
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, err
	}

	// For simplicity, fetch all comments as a JSON array
	cmd = exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%s/comments", prNumber))
	out, err = cmd.Output()
	if err != nil {
		return nil, nil // No comments
	}

	var responses []prCommentResponse
	if err := json.Unmarshal(out, &responses); err != nil {
		return nil, nil // Parse error, treat as no comments
	}

	comments := make([]Comment, 0, len(responses))
	for _, r := range responses {
		if r.Body != "" {
			comments = append(comments, Comment{
				Author: r.User.Login,
				Body:   r.Body,
			})
		}
	}

	// Also fetch issue comments (general PR comments)
	cmd = exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/issues/%s/comments", prNumber))
	out, err = cmd.Output()
	if err == nil {
		var issueComments []prCommentResponse
		if json.Unmarshal(out, &issueComments) == nil {
			for _, r := range issueComments {
				if r.Body != "" {
					comments = append(comments, Comment{
						Author: r.User.Login,
						Body:   r.Body,
					})
				}
			}
		}
	}

	return comments, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/feedback/... -v -run TestFetchPRContext`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/feedback/fetch.go internal/feedback/fetch_test.go
git commit -m "feat(feedback): add FetchPRContext to retrieve PR data"
```

---

## Task 3: Add summarizer prompt

**Files:**
- Create: `internal/feedback/prompt.go`

**Step 1: Create prompt file (no test needed for constant)**

```go
// internal/feedback/prompt.go
package feedback

const summarizePrompt = `You are analyzing a GitHub PR to summarize feedback about code review findings.

## Input
You will receive:
- PR description
- All comments and replies from the PR

## Task
Summarize any feedback that indicates code review issues have been:
- Dismissed as false positives or not applicable
- Explained as intentional design decisions
- Acknowledged but deferred to future work
- Marked as resolved

Focus on feedback relevant to code quality findings (bugs, security issues, error handling, etc.).
Ignore unrelated discussion (feature questions, deployment, CI status, etc.).

## Output
Write a concise prose summary (2-5 sentences). Focus on what a code reviewer should probably ignore based on prior discussion.

If no relevant feedback exists, respond with exactly:
No prior feedback on code review findings.

Example outputs:

"The PR description notes this is a prototype and comprehensive error handling will be added in a follow-up PR. A reviewer's concern about the unchecked error in auth.go was addressed - the author explained errors are validated by middleware upstream."

"The author acknowledged the SQL query could use parameterized queries but noted this is an internal admin tool with no user input. A thread about missing nil checks was resolved."

"No prior feedback on code review findings."`
```

**Step 2: Commit**

```bash
git add internal/feedback/prompt.go
git commit -m "feat(feedback): add PR feedback summarization prompt"
```

---

## Task 4: Add Summarizer struct and Summarize method

**Files:**
- Create: `internal/feedback/summarizer.go`
- Create: `internal/feedback/summarizer_test.go`

**Step 1: Write test for Summarizer**

```go
// internal/feedback/summarizer_test.go
package feedback

import (
	"context"
	"testing"
)

func TestSummarizer_EmptyPRNumber(t *testing.T) {
	s := NewSummarizer("codex", false)
	_, err := s.Summarize(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}

func TestNewSummarizer(t *testing.T) {
	s := NewSummarizer("claude", true)
	if s == nil {
		t.Fatal("NewSummarizer returned nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/feedback/... -v -run TestSummarizer`
Expected: FAIL - Summarizer doesn't exist

**Step 3: Write implementation**

```go
// internal/feedback/summarizer.go
package feedback

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

// Result contains the output from the feedback summarizer.
type Result struct {
	Summary  string
	Duration time.Duration
	Error    error
}

// Summarizer summarizes PR feedback for the FP filter.
type Summarizer struct {
	agentName string
	verbose   bool
}

// NewSummarizer creates a new PR feedback summarizer.
func NewSummarizer(agentName string, verbose bool) *Summarizer {
	return &Summarizer{
		agentName: agentName,
		verbose:   verbose,
	}
}

// Summarize fetches PR context and returns a prose summary of prior feedback.
func (s *Summarizer) Summarize(ctx context.Context, prNumber string) (string, error) {
	if prNumber == "" {
		return "", fmt.Errorf("PR number is required")
	}

	// Fetch PR context
	prCtx, err := FetchPRContext(ctx, prNumber)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PR context: %w", err)
	}

	if !prCtx.HasContent() {
		return "", nil
	}

	// Build input for the LLM
	input := s.buildInput(prCtx)

	// Create agent
	ag, err := agent.NewAgent(s.agentName)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	// Execute summary
	execResult, err := ag.ExecuteSummary(ctx, summarizePrompt, []byte(input))
	if err != nil {
		if ctx.Err() != nil {
			return "", nil // Context canceled, return empty
		}
		return "", fmt.Errorf("agent execution failed: %w", err)
	}
	defer func() {
		if err := execResult.Close(); err != nil && s.verbose {
			log.Printf("[feedback] close error (non-fatal): %v", err)
		}
	}()

	// Read output
	output, err := io.ReadAll(execResult)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil
		}
		return "", fmt.Errorf("failed to read agent output: %w", err)
	}

	summary := strings.TrimSpace(string(output))

	// Clean up markdown code fences if present
	summary = agent.StripMarkdownCodeFence(summary)

	// Check for "no feedback" response
	if strings.Contains(strings.ToLower(summary), "no prior feedback") {
		return "", nil
	}

	return summary, nil
}

func (s *Summarizer) buildInput(prCtx *PRContext) string {
	var sb strings.Builder

	sb.WriteString("## PR Description\n\n")
	if prCtx.Description != "" {
		sb.WriteString(prCtx.Description)
	} else {
		sb.WriteString("(No description)")
	}
	sb.WriteString("\n\n")

	if len(prCtx.Comments) > 0 {
		sb.WriteString("## Comments\n\n")
		for _, c := range prCtx.Comments {
			fmt.Fprintf(&sb, "**%s**: %s\n\n", c.Author, c.Body)
			for _, r := range c.Replies {
				fmt.Fprintf(&sb, "  > **%s**: %s\n\n", r.Author, r.Body)
			}
		}
	}

	return sb.String()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/feedback/... -v -run TestSummarizer`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/feedback/summarizer.go internal/feedback/summarizer_test.go
git commit -m "feat(feedback): add Summarizer to run LLM on PR context"
```

---

## Task 5: Update FP filter to accept prior feedback

**Files:**
- Modify: `internal/fpfilter/filter.go`
- Modify: `internal/fpfilter/prompt.go`
- Create: `internal/fpfilter/filter_test.go`

**Step 1: Write test for Apply with priorFeedback**

```go
// internal/fpfilter/filter_test.go
package fpfilter

import (
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestFilter_EmptyFindings(t *testing.T) {
	// Just test that empty input works - can't easily test LLM behavior
	f := New("codex", 75, false)
	if f == nil {
		t.Fatal("New returned nil")
	}
}

func TestBuildPromptWithFeedback(t *testing.T) {
	feedback := "User said the null check is intentional"
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, feedback)

	if !contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should contain Prior Feedback Context section")
	}
	if !contains(prompt, feedback) {
		t.Error("prompt should contain the feedback text")
	}
}

func TestBuildPromptWithoutFeedback(t *testing.T) {
	prompt := buildPromptWithFeedback(fpEvaluationPrompt, "")

	if contains(prompt, "Prior Feedback Context") {
		t.Error("prompt should not contain Prior Feedback Context when feedback is empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/fpfilter/... -v`
Expected: FAIL - buildPromptWithFeedback doesn't exist

**Step 3: Update prompt.go with feedback section**

```go
// Add to internal/fpfilter/prompt.go

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

// buildPromptWithFeedback appends prior feedback context to the base prompt if provided.
func buildPromptWithFeedback(basePrompt, priorFeedback string) string {
	if priorFeedback == "" {
		return basePrompt
	}
	return basePrompt + fmt.Sprintf(priorFeedbackSection, priorFeedback)
}
```

Add import for `fmt` at top of prompt.go.

**Step 4: Update filter.go Apply signature**

```go
// Modify internal/fpfilter/filter.go

// Change Apply signature from:
func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings) (*Result, error)

// To:
func (f *Filter) Apply(ctx context.Context, grouped domain.GroupedFindings, priorFeedback string) (*Result, error)

// And update the prompt construction:
// Change:
execResult, err := ag.ExecuteSummary(ctx, fpEvaluationPrompt, payload)

// To:
prompt := buildPromptWithFeedback(fpEvaluationPrompt, priorFeedback)
execResult, err := ag.ExecuteSummary(ctx, prompt, payload)
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/fpfilter/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/fpfilter/filter.go internal/fpfilter/prompt.go internal/fpfilter/filter_test.go
git commit -m "feat(fpfilter): add priorFeedback parameter to Apply"
```

---

## Task 6: Update review.go caller to pass empty string

**Files:**
- Modify: `cmd/acr/review.go`

**Step 1: Update fpFilter.Apply call**

Find this line in `cmd/acr/review.go`:
```go
fpResult, err := fpFilter.Apply(ctx, summaryResult.Grouped)
```

Change to:
```go
fpResult, err := fpFilter.Apply(ctx, summaryResult.Grouped, "")
```

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: PASS (all tests should still pass)

**Step 3: Commit**

```bash
git add cmd/acr/review.go
git commit -m "fix: update fpFilter.Apply call with empty priorFeedback"
```

---

## Task 7: Add config fields for PR feedback

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add PRFeedback config struct**

Add to config.go after FPFilterConfig:
```go
// PRFeedbackConfig holds PR feedback summarization settings.
type PRFeedbackConfig struct {
	Enabled *bool   `yaml:"enabled"`
	Agent   *string `yaml:"agent"`
}
```

Add to Config struct:
```go
PRFeedback PRFeedbackConfig `yaml:"pr_feedback"`
```

Add to knownTopLevelKeys:
```go
var knownTopLevelKeys = []string{..., "pr_feedback"}
```

Add new knownPRFeedbackKeys:
```go
var knownPRFeedbackKeys = []string{"enabled", "agent"}
```

Add validation in checkUnknownKeys:
```go
if prFeedback, ok := raw["pr_feedback"].(map[string]any); ok {
	for key := range prFeedback {
		if !slices.Contains(knownPRFeedbackKeys, key) {
			warning := fmt.Sprintf("unknown key %q in pr_feedback section of %s", key, ConfigFileName)
			if suggestion := findSimilar(key, knownPRFeedbackKeys); suggestion != "" {
				warning += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			warnings = append(warnings, warning)
		}
	}
}
```

Add to ResolvedConfig:
```go
PRFeedbackEnabled bool
PRFeedbackAgent   string
```

Add to Defaults:
```go
PRFeedbackEnabled: true,
PRFeedbackAgent:   "", // empty means use summarizer agent
```

Add to FlagState:
```go
NoPRFeedbackSet    bool
PRFeedbackAgentSet bool
```

Add to EnvState:
```go
PRFeedbackEnabled    bool
PRFeedbackEnabledSet bool
PRFeedbackAgent      string
PRFeedbackAgentSet   bool
```

**Step 2: Add env var loading in LoadEnvState**

```go
if v := os.Getenv("ACR_PR_FEEDBACK"); v != "" {
	switch v {
	case "true", "1":
		state.PRFeedbackEnabled = true
		state.PRFeedbackEnabledSet = true
	case "false", "0":
		state.PRFeedbackEnabled = false
		state.PRFeedbackEnabledSet = true
	}
}

if v := os.Getenv("ACR_PR_FEEDBACK_AGENT"); v != "" {
	state.PRFeedbackAgent = v
	state.PRFeedbackAgentSet = true
}
```

**Step 3: Add resolution in Resolve function**

In config file section:
```go
if cfg.PRFeedback.Enabled != nil {
	result.PRFeedbackEnabled = *cfg.PRFeedback.Enabled
}
if cfg.PRFeedback.Agent != nil {
	result.PRFeedbackAgent = *cfg.PRFeedback.Agent
}
```

In env var section:
```go
if envState.PRFeedbackEnabledSet {
	result.PRFeedbackEnabled = envState.PRFeedbackEnabled
}
if envState.PRFeedbackAgentSet {
	result.PRFeedbackAgent = envState.PRFeedbackAgent
}
```

In flag section:
```go
if flagState.NoPRFeedbackSet {
	result.PRFeedbackEnabled = flagValues.PRFeedbackEnabled
}
if flagState.PRFeedbackAgentSet {
	result.PRFeedbackAgent = flagValues.PRFeedbackAgent
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add pr_feedback configuration fields"
```

---

## Task 8: Add CLI flags for PR feedback

**Files:**
- Modify: `cmd/acr/main.go`

**Step 1: Add flag variables**

Add to var block:
```go
noPRFeedback    bool
prFeedbackAgent string
```

**Step 2: Add flag definitions**

Add after fpThreshold flag:
```go
rootCmd.Flags().BoolVar(&noPRFeedback, "no-pr-feedback", false,
	"Disable reading PR comments for feedback context (env: ACR_PR_FEEDBACK=false)")
rootCmd.Flags().StringVar(&prFeedbackAgent, "pr-feedback-agent", "",
	"Agent for PR feedback summarization (default: same as --summarizer-agent, env: ACR_PR_FEEDBACK_AGENT)")
```

**Step 3: Update flagState building**

Add to flagState:
```go
NoPRFeedbackSet:    cmd.Flags().Changed("no-pr-feedback"),
PRFeedbackAgentSet: cmd.Flags().Changed("pr-feedback-agent"),
```

**Step 4: Update flagValues building**

Add to flagValues:
```go
PRFeedbackEnabled: !noPRFeedback,
PRFeedbackAgent:   prFeedbackAgent,
```

**Step 5: Run build**

Run: `go build ./cmd/acr`
Expected: Build succeeds

**Step 6: Commit**

```bash
git add cmd/acr/main.go
git commit -m "feat(cli): add --no-pr-feedback and --pr-feedback-agent flags"
```

---

## Task 9: Integrate feedback summarizer into review flow

**Files:**
- Modify: `cmd/acr/review.go`

**Step 1: Add import**

```go
"github.com/richhaase/agentic-code-reviewer/internal/feedback"
```

**Step 2: Add feedback summarizer parameters to executeReview**

Change function signature:
```go
func executeReview(ctx context.Context, workDir string, excludePatterns []string, customPrompt string, reviewerAgentNames []string, summarizerAgentName string, fetchRemote bool, useRefFile bool, fpFilterEnabled bool, fpThreshold int, prFeedbackEnabled bool, prFeedbackAgent string, prNumber string, logger *terminal.Logger) domain.ExitCode {
```

**Step 3: Add parallel feedback fetching**

Add after the reviewers start (before `results, wallClock, err := r.Run(ctx)`):

```go
// Start PR feedback summarizer in parallel with reviewers (if enabled and reviewing a PR)
var priorFeedback string
var feedbackWg sync.WaitGroup
if prFeedbackEnabled && prNumber != "" {
	feedbackWg.Add(1)
	go func() {
		defer feedbackWg.Done()

		// Determine which agent to use for feedback summarization
		feedbackAgentName := prFeedbackAgent
		if feedbackAgentName == "" {
			feedbackAgentName = summarizerAgentName
		}

		summarizer := feedback.NewSummarizer(feedbackAgentName, verbose)
		summary, err := summarizer.Summarize(ctx, prNumber)
		if err != nil {
			if verbose {
				logger.Logf(terminal.StyleWarning, "PR feedback summarizer: %v", err)
			}
			return
		}
		priorFeedback = summary
	}()
}
```

Add import for `"sync"`.

**Step 4: Wait for feedback before FP filter**

Add before FP filter section:
```go
// Wait for PR feedback summarizer
feedbackWg.Wait()
if priorFeedback != "" && verbose {
	logger.Logf(terminal.StyleDim, "PR feedback context available")
}
```

**Step 5: Pass priorFeedback to FP filter**

Change:
```go
fpResult, err := fpFilter.Apply(ctx, summaryResult.Grouped, "")
```

To:
```go
fpResult, err := fpFilter.Apply(ctx, summaryResult.Grouped, priorFeedback)
```

**Step 6: Update call site in main.go**

Find the executeReview call and add the new parameters:
```go
code := executeReview(ctx, workDir, allExcludePatterns, customPrompt, resolved.ReviewerAgents, resolved.SummarizerAgent, resolved.Fetch, refFile, resolved.FPFilterEnabled, resolved.FPThreshold, resolved.PRFeedbackEnabled, resolved.PRFeedbackAgent, prNumber, logger)
```

**Step 7: Run tests**

Run: `go test ./... -v`
Expected: PASS

**Step 8: Commit**

```bash
git add cmd/acr/main.go cmd/acr/review.go
git commit -m "feat: integrate PR feedback summarizer into review flow"
```

---

## Task 10: Run full test suite and lint

**Step 1: Run all tests**

Run: `make test`
Expected: All tests pass

**Step 2: Run linter**

Run: `make lint`
Expected: No lint errors

**Step 3: Run full check**

Run: `make check`
Expected: All checks pass

**Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address lint issues"
```

---

## Task 11: Manual integration test

**Step 1: Build the binary**

Run: `make build`

**Step 2: Test on a real PR (if available)**

Run: `./bin/acr --pr <number> -v`

Observe:
- Feedback summarizer should run in parallel
- If PR has comments, should see "PR feedback context available" in verbose mode
- FP filter should receive the context

**Step 3: Test with --no-pr-feedback**

Run: `./bin/acr --pr <number> -v --no-pr-feedback`

Observe:
- No feedback summarizer output
- Review should complete normally

---

## Summary of files changed

**New files:**
- `internal/feedback/fetch.go`
- `internal/feedback/fetch_test.go`
- `internal/feedback/prompt.go`
- `internal/feedback/summarizer.go`
- `internal/feedback/summarizer_test.go`
- `internal/fpfilter/filter_test.go`

**Modified files:**
- `internal/fpfilter/filter.go` - Add priorFeedback parameter
- `internal/fpfilter/prompt.go` - Add feedback section builder
- `internal/config/config.go` - Add pr_feedback config
- `cmd/acr/main.go` - Add CLI flags
- `cmd/acr/review.go` - Integrate feedback summarizer
