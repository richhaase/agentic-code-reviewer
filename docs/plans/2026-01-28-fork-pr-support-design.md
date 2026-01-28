# Fork PR Support Design

**Issue:** #84
**Branch:** `feat/support_fork_prs`
**Date:** 2026-01-28

## Problem

ACR cannot review pull requests from forked repositories. The `-B` flag accepts GitHub's fork notation (`username:branch`), but this fails because it's not a valid git reference:

```
❯ acr -B yunidbauza:feat/enable-pr-number-review
[!] Error: failed to create worktree for branch 'yunidbauza:feat/enable-pr-number-review'
    (fatal: invalid reference: yunidbauza:feat/enable-pr-number-review): exit status 128
```

## Solution

Detect fork notation, resolve the fork's repository URL via GitHub PR metadata, fetch the branch, and create the worktree from the fetched ref.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Code location | `github/` package | Fork resolution requires GitHub API (`gh` CLI); keeps `git/` pure |
| Fork URL resolution | PR metadata lookup | Handles renamed forks; validates PR exists |
| Remote naming | `fork-{username}` | Clear, readable, unlikely to collide |
| Remote cleanup | Always remove after worktree creation | Keeps repo clean |
| No-PR behavior | Error with clear message | YAGNI; edge case can manually add remote |

## API

### New: `github/fork.go`

```go
type ForkRef struct {
    Username   string  // e.g., "yunidbauza"
    Branch     string  // e.g., "feat/enable-pr-number-review"
    RepoURL    string  // e.g., "https://github.com/yunidbauza/agentic-code-reviewer.git"
    RemoteName string  // e.g., "fork-yunidbauza"
    PRNumber   int     // e.g., 83
}

// ParseForkRef detects and resolves "username:branch" notation.
// Returns (nil, nil) if not fork notation.
// Returns (*ForkRef, nil) for valid fork refs.
// Returns (nil, error) if fork notation used but no PR found.
func ParseForkRef(ctx context.Context, ref string) (*ForkRef, error)
```

### New: `git/remote.go`

```go
func AddRemote(repoDir, name, url string) error
func RemoveRemote(repoDir, name string) error
func FetchBranch(ctx context.Context, repoDir, remote, branch string) error
```

## Implementation Flow

```
main.go receives -B yunidbauza:feat/branch
    │
    ▼
github.ParseForkRef("yunidbauza:feat/branch")
    │
    ├─ Split on ":" → username="yunidbauza", branch="feat/branch"
    │
    ├─ Run: gh pr list --head feat/branch --json headRepositoryOwner,headRepository,number
    │
    ├─ Filter: headRepositoryOwner.login == "yunidbauza"
    │
    └─ Return ForkRef with constructed URL
    │
    ▼
git.AddRemote(repoRoot, "fork-yunidbauza", "https://github.com/yunidbauza/repo.git")
    │
    ▼
git.FetchBranch(ctx, repoRoot, "fork-yunidbauza", "feat/branch")
    │
    ▼
git.CreateWorktree("fork-yunidbauza/feat/branch")
    │
    ▼
git.RemoveRemote(repoRoot, "fork-yunidbauza")
    │
    ▼
Review proceeds normally in worktree
```

## Error Handling

| Scenario | Error Message |
|----------|---------------|
| Fork notation but no PR found | `No open PR found for {user}:{branch}. Fork reviews require an open PR.` |
| `gh` CLI not available | `gh CLI not found. Install from https://cli.github.com/` |
| Not authenticated | `GitHub authentication required. Run 'gh auth login'.` |
| Remote add fails | `Failed to add remote 'fork-{user}': {git error}` |
| Fetch fails | `Failed to fetch branch '{branch}' from fork: {git error}` |

Cleanup: If any step fails after remote is added, remove the remote before returning.

## Files

**New:**
- `internal/github/fork.go`
- `internal/github/fork_test.go`
- `internal/git/remote.go`
- `internal/git/remote_test.go`

**Modified:**
- `cmd/acr/main.go`

**Unchanged:**
- `internal/git/worktree.go`
- `internal/github/pr.go`
