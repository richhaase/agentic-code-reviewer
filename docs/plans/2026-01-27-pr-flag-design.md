# Design: Review PR by Number (`--pr` flag)

**Date:** 2026-01-27
**Status:** Approved

## Summary

Add a `--pr <number>` flag that allows reviewing a PR without having the branch checked out locally. ACR will fetch the PR branch into a temporary worktree, run the review, and clean up automatically.

## User Interface

**New flag:**
```
--pr <number>    Review a PR by number (fetches branch into temp worktree)
```

**Example usage:**
```bash
acr -r 10 -c 5 --pr 123
acr --pr 123 --local          # Review PR 123 without posting back to GitHub
acr --pr 123 -b main          # Explicit base ref override
```

**Behavior:**
- `--pr` and `--worktree-branch` are mutually exclusive
- Base ref is auto-detected from PR target branch, but can be overridden with `-b`

## Implementation

### New Functions

**`internal/github/pr.go`:**
```go
// GetPRBranch returns the head branch name for a PR number
func GetPRBranch(ctx context.Context, prNumber string) (string, error)

// GetPRBaseRef returns the base branch name for a PR number
func GetPRBaseRef(ctx context.Context, prNumber string) (string, error)
```

**`internal/git/worktree.go`:**
```go
// CreateWorktreeFromPR fetches a PR and creates a worktree for it
func CreateWorktreeFromPR(ctx context.Context, prNumber string) (*Worktree, error)
```

### Flow

`CreateWorktreeFromPR` performs:
1. `gh pr view <number> --json headRefName` to get branch name
2. `git fetch origin pull/<number>/head:<branch>` to fetch the PR ref
3. Call existing `CreateWorktree(branch)` to create the worktree

### Changes to `cmd/acr/main.go`

```go
var prNumber string

// Flag setup:
rootCmd.Flags().StringVar(&prNumber, "pr", "", "Review a PR by number (fetches into temp worktree)")

// Validation in runReview:
if prNumber != "" && worktreeBranch != "" {
    return error("--pr and --worktree-branch are mutually exclusive")
}

if prNumber != "" {
    if err := github.CheckGHAvailable(); err != nil {
        return fmt.Errorf("--pr requires gh CLI: %w", err)
    }

    // Auto-detect base ref if not explicitly set
    if !cmd.Flags().Changed("base") {
        detectedBase, err := github.GetPRBaseRef(ctx, prNumber)
        if err == nil {
            baseRef = detectedBase
        }
    }

    wt, err := git.CreateWorktreeFromPR(ctx, prNumber)
    if err != nil {
        return err
    }
    defer wt.Remove()
    workDir = wt.Path
}
```

## Error Handling

| Scenario | Error Message |
|----------|---------------|
| PR doesn't exist | "PR #123 not found" |
| `gh` CLI not available | "--pr requires gh CLI" |
| `gh` not authenticated | "GitHub authentication required (run `gh auth login`)" |
| Network failure | Standard error with context |

Fork PRs are handled automatically via `git fetch origin pull/N/head:branch`.

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/acr/main.go` | Add `--pr` flag, validation, worktree creation, base ref auto-detection |
| `internal/github/pr.go` | Add `GetPRBranch()`, `GetPRBaseRef()` |
| `internal/git/worktree.go` | Add `CreateWorktreeFromPR()` |
| `internal/github/pr_test.go` | Tests for new functions |
| `internal/git/worktree_test.go` | Tests for `CreateWorktreeFromPR` |

## Testing

**Unit tests:**
- `GetPRBranch` / `GetPRBaseRef` JSON parsing
- Mutual exclusivity validation

**Manual test scenarios:**
1. `acr --pr 123` - Happy path
2. `acr --pr 999` - PR doesn't exist
3. `acr --pr 123 -b main` - Override auto-detected base
4. `acr --pr 123 --worktree-branch foo` - Should error
5. `acr --pr 123` without `gh` - Should error with clear message
