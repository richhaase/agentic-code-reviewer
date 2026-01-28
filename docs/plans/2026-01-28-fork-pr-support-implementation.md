# Fork PR Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable ACR to review PRs from forked repositories using GitHub's `username:branch` notation.

**Architecture:** Detect fork notation in `-B` flag, resolve fork URL via GitHub PR metadata (`gh pr list`), add temporary remote, fetch branch, create worktree from fetched ref, cleanup remote after.

**Tech Stack:** Go, `gh` CLI for GitHub API, `git` CLI for remote/fetch operations.

---

### Task 1: Create `github/fork.go` with ForkRef Type and ParseForkRef Function

**Files:**
- Create: `internal/github/fork.go`

**Step 1: Write the failing test**

Create `internal/github/fork_test.go`:

```go
package github

import (
	"testing"
)

func TestParseForkNotation_Valid(t *testing.T) {
	username, branch, ok := ParseForkNotation("yunidbauza:feat/enable-pr-number-review")
	if !ok {
		t.Error("expected ok=true for valid fork notation")
	}
	if username != "yunidbauza" {
		t.Errorf("expected username 'yunidbauza', got %q", username)
	}
	if branch != "feat/enable-pr-number-review" {
		t.Errorf("expected branch 'feat/enable-pr-number-review', got %q", branch)
	}
}

func TestParseForkNotation_NotForkNotation(t *testing.T) {
	_, _, ok := ParseForkNotation("main")
	if ok {
		t.Error("expected ok=false for non-fork notation")
	}
}

func TestParseForkNotation_EmptyUsername(t *testing.T) {
	_, _, ok := ParseForkNotation(":branch")
	if ok {
		t.Error("expected ok=false for empty username")
	}
}

func TestParseForkNotation_EmptyBranch(t *testing.T) {
	_, _, ok := ParseForkNotation("user:")
	if ok {
		t.Error("expected ok=false for empty branch")
	}
}

func TestParseForkNotation_MultipleColons(t *testing.T) {
	username, branch, ok := ParseForkNotation("user:feat/with:colon")
	if !ok {
		t.Error("expected ok=true for branch with colon")
	}
	if username != "user" {
		t.Errorf("expected username 'user', got %q", username)
	}
	if branch != "feat/with:colon" {
		t.Errorf("expected branch 'feat/with:colon', got %q", branch)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestParseForkNotation -v`
Expected: FAIL with "undefined: ParseForkNotation"

**Step 3: Write minimal implementation**

Create `internal/github/fork.go`:

```go
// Package github provides GitHub PR operations via the gh CLI.
package github

import (
	"strings"
)

// ForkRef contains resolved information about a fork reference.
type ForkRef struct {
	Username   string // Fork owner username (e.g., "yunidbauza")
	Branch     string // Branch name (e.g., "feat/enable-pr-number-review")
	RepoURL    string // Clone URL (e.g., "https://github.com/yunidbauza/repo.git")
	RemoteName string // Temporary remote name (e.g., "fork-yunidbauza")
	PRNumber   int    // Associated PR number
}

// ParseForkNotation parses GitHub's "username:branch" fork notation.
// Returns the username, branch, and true if valid fork notation.
// Returns "", "", false if not fork notation or invalid.
func ParseForkNotation(ref string) (username, branch string, ok bool) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	username, branch = parts[0], parts[1]
	if username == "" || branch == "" {
		return "", "", false
	}
	return username, branch, true
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestParseForkNotation -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/fork.go internal/github/fork_test.go
git commit -m "feat(github): add ForkRef type and ParseForkNotation function"
```

---

### Task 2: Add ResolveForkRef Function to Query GitHub PR Metadata

**Files:**
- Modify: `internal/github/fork.go`
- Modify: `internal/github/fork_test.go`

**Step 1: Write the failing test**

Add to `internal/github/fork_test.go`:

```go
func TestBuildForkRef(t *testing.T) {
	// Test the ForkRef construction logic (not the gh CLI call)
	ref := buildForkRef("yunidbauza", "feat/branch", "agentic-code-reviewer", 83)

	if ref.Username != "yunidbauza" {
		t.Errorf("expected username 'yunidbauza', got %q", ref.Username)
	}
	if ref.Branch != "feat/branch" {
		t.Errorf("expected branch 'feat/branch', got %q", ref.Branch)
	}
	if ref.RepoURL != "https://github.com/yunidbauza/agentic-code-reviewer.git" {
		t.Errorf("expected RepoURL with .git suffix, got %q", ref.RepoURL)
	}
	if ref.RemoteName != "fork-yunidbauza" {
		t.Errorf("expected RemoteName 'fork-yunidbauza', got %q", ref.RemoteName)
	}
	if ref.PRNumber != 83 {
		t.Errorf("expected PRNumber 83, got %d", ref.PRNumber)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestBuildForkRef -v`
Expected: FAIL with "undefined: buildForkRef"

**Step 3: Write minimal implementation**

Add to `internal/github/fork.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoForkPRFound indicates no PR exists for the given fork reference.
var ErrNoForkPRFound = fmt.Errorf("no open PR found for fork reference")

// prListResult represents a single PR from gh pr list output.
type prListResult struct {
	Number              int `json:"number"`
	HeadRefName         string `json:"headRefName"`
	HeadRepositoryOwner struct {
		Login string `json:"login"`
	} `json:"headRepositoryOwner"`
	HeadRepository struct {
		Name string `json:"name"`
	} `json:"headRepository"`
}

// buildForkRef constructs a ForkRef from resolved PR metadata.
func buildForkRef(username, branch, repoName string, prNumber int) *ForkRef {
	return &ForkRef{
		Username:   username,
		Branch:     branch,
		RepoURL:    fmt.Sprintf("https://github.com/%s/%s.git", username, repoName),
		RemoteName: fmt.Sprintf("fork-%s", username),
		PRNumber:   prNumber,
	}
}

// ResolveForkRef detects and resolves "username:branch" fork notation.
// Returns (*ForkRef, nil) for valid fork refs with an open PR.
// Returns (nil, nil) if ref is not fork notation (caller should use ref as-is).
// Returns (nil, error) if fork notation is used but resolution fails.
func ResolveForkRef(ctx context.Context, ref string) (*ForkRef, error) {
	username, branch, ok := ParseForkNotation(ref)
	if !ok {
		return nil, nil // Not fork notation
	}

	// Query GitHub for PRs with this head branch
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--head", branch,
		"--json", "number,headRefName,headRepositoryOwner,headRepository",
		"--limit", "100",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query PRs: %w", classifyGHError(err))
	}

	var prs []prListResult
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}

	// Find PR from the specified fork owner
	for _, pr := range prs {
		if strings.EqualFold(pr.HeadRepositoryOwner.Login, username) {
			return buildForkRef(
				pr.HeadRepositoryOwner.Login,
				pr.HeadRefName,
				pr.HeadRepository.Name,
				pr.Number,
			), nil
		}
	}

	return nil, fmt.Errorf("%w: %s:%s", ErrNoForkPRFound, username, branch)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestBuildForkRef -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/fork.go internal/github/fork_test.go
git commit -m "feat(github): add ResolveForkRef to query PR metadata for forks"
```

---

### Task 3: Create `git/remote.go` with AddRemote Function

**Files:**
- Create: `internal/git/remote.go`
- Create: `internal/git/remote_test.go`

**Step 1: Write the failing test**

Create `internal/git/remote_test.go`:

```go
package git

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAddRemote_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Verify remote was added
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git remote -v failed: %v", err)
	}
	if !strings.Contains(string(out), "fork-testuser") {
		t.Error("remote 'fork-testuser' not found in git remote -v output")
	}
}

func TestAddRemote_AlreadyExists(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Add remote first time
	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("first AddRemote failed: %v", err)
	}

	// Add same remote again should fail
	err = AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err == nil {
		t.Error("expected error when adding duplicate remote")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -run TestAddRemote -v`
Expected: FAIL with "undefined: AddRemote"

**Step 3: Write minimal implementation**

Create `internal/git/remote.go`:

```go
// Package git provides git operations including worktree management.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AddRemote adds a new git remote.
func AddRemote(repoDir, name, url string) error {
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to add remote '%s': %s", name, output)
		}
		return fmt.Errorf("failed to add remote '%s': %w", name, err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -run TestAddRemote -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/remote.go internal/git/remote_test.go
git commit -m "feat(git): add AddRemote function"
```

---

### Task 4: Add RemoveRemote Function

**Files:**
- Modify: `internal/git/remote.go`
- Modify: `internal/git/remote_test.go`

**Step 1: Write the failing test**

Add to `internal/git/remote_test.go`:

```go
func TestRemoveRemote_Success(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Add remote first
	err := AddRemote(repoDir, "fork-testuser", "https://github.com/testuser/repo.git")
	if err != nil {
		t.Fatalf("AddRemote failed: %v", err)
	}

	// Remove it
	err = RemoveRemote(repoDir, "fork-testuser")
	if err != nil {
		t.Fatalf("RemoveRemote failed: %v", err)
	}

	// Verify remote was removed
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	if strings.Contains(string(out), "fork-testuser") {
		t.Error("remote 'fork-testuser' should have been removed")
	}
}

func TestRemoveRemote_NotExists(t *testing.T) {
	repoDir := setupTestRepo(t)

	// Remove non-existent remote should fail
	err := RemoveRemote(repoDir, "nonexistent-remote")
	if err == nil {
		t.Error("expected error when removing non-existent remote")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -run TestRemoveRemote -v`
Expected: FAIL with "undefined: RemoveRemote"

**Step 3: Write minimal implementation**

Add to `internal/git/remote.go`:

```go
// RemoveRemote removes a git remote.
func RemoveRemote(repoDir, name string) error {
	cmd := exec.Command("git", "remote", "remove", name)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to remove remote '%s': %s", name, output)
		}
		return fmt.Errorf("failed to remove remote '%s': %w", name, err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -run TestRemoveRemote -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/remote.go internal/git/remote_test.go
git commit -m "feat(git): add RemoveRemote function"
```

---

### Task 5: Add FetchBranch Function

**Files:**
- Modify: `internal/git/remote.go`
- Modify: `internal/git/remote_test.go`

**Step 1: Write the failing test**

Add to `internal/git/remote_test.go`:

```go
func TestFetchBranch_InvalidRemote(t *testing.T) {
	repoDir := setupTestRepo(t)

	ctx := context.Background()
	err := FetchBranch(ctx, repoDir, "nonexistent-remote", "main")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
}
```

Note: Testing successful fetch requires a real remote, which is integration-test territory. We test the error case here.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -run TestFetchBranch -v`
Expected: FAIL with "undefined: FetchBranch"

**Step 3: Write minimal implementation**

Add to `internal/git/remote.go`:

```go
// FetchBranch fetches a specific branch from a remote.
func FetchBranch(ctx context.Context, repoDir, remote, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, branch)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch '%s' from '%s': %s", branch, remote, output)
		}
		return fmt.Errorf("failed to fetch '%s' from '%s': %w", branch, remote, err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -run TestFetchBranch -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/remote.go internal/git/remote_test.go
git commit -m "feat(git): add FetchBranch function"
```

---

### Task 6: Integrate Fork Resolution into main.go

**Files:**
- Modify: `cmd/acr/main.go`

**Step 1: Read current main.go worktree handling**

The worktree handling starts at line 146. We need to:
1. Add import for `github` package
2. Check for fork notation before creating worktree
3. If fork: resolve → add remote → fetch → create worktree → cleanup remote

**Step 2: Write the implementation**

Modify `cmd/acr/main.go` - add to imports:

```go
import (
	// ... existing imports ...
	"github.com/richhaase/agentic-code-reviewer/internal/github"
)
```

Replace the worktree handling block (lines 146-165) with:

```go
	// Handle worktree-based review
	var workDir string
	if worktreeBranch != "" {
		logger.Logf(terminal.StyleInfo, "Creating worktree for %s%s%s",
			terminal.Color(terminal.Bold), worktreeBranch, terminal.Color(terminal.Reset))

		// Check if this is fork notation (username:branch)
		var actualRef string
		var cleanupRemote func()

		forkRef, err := github.ResolveForkRef(ctx, worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}

		if forkRef != nil {
			// Fork flow: add remote, fetch, set ref
			logger.Logf(terminal.StyleInfo, "Resolved fork PR #%d from %s",
				forkRef.PRNumber, forkRef.Username)

			repoRoot, err := git.GetRoot()
			if err != nil {
				logger.Logf(terminal.StyleError, "Error getting repo root: %v", err)
				return exitCode(domain.ExitError)
			}

			// Add temporary remote
			if err := git.AddRemote(repoRoot, forkRef.RemoteName, forkRef.RepoURL); err != nil {
				logger.Logf(terminal.StyleError, "Error adding remote: %v", err)
				return exitCode(domain.ExitError)
			}
			cleanupRemote = func() {
				_ = git.RemoveRemote(repoRoot, forkRef.RemoteName)
			}

			// Fetch the branch
			logger.Logf(terminal.StyleDim, "Fetching %s from %s", forkRef.Branch, forkRef.RepoURL)
			if err := git.FetchBranch(ctx, repoRoot, forkRef.RemoteName, forkRef.Branch); err != nil {
				cleanupRemote()
				logger.Logf(terminal.StyleError, "Error fetching fork branch: %v", err)
				return exitCode(domain.ExitError)
			}

			actualRef = fmt.Sprintf("%s/%s", forkRef.RemoteName, forkRef.Branch)
		} else {
			// Normal branch
			actualRef = worktreeBranch
		}

		wt, err := git.CreateWorktree(actualRef)
		if err != nil {
			if cleanupRemote != nil {
				cleanupRemote()
			}
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}

		// Cleanup remote after worktree is created (worktree has the files, remote no longer needed)
		if cleanupRemote != nil {
			cleanupRemote()
		}

		defer func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}()

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		workDir = wt.Path
	}
```

**Step 3: Run tests to verify nothing broke**

Run: `make check`
Expected: All tests pass, no lint errors

**Step 4: Manual test with real fork PR**

Run: `go build -o bin/acr ./cmd/acr && bin/acr -B yunidbauza:feat/enable-pr-number-review --local -r 1`
Expected: Should successfully create worktree and run review

**Step 5: Commit**

```bash
git add cmd/acr/main.go
git commit -m "feat: integrate fork PR support into worktree handling

Detect username:branch notation, resolve via GitHub PR metadata,
add temporary remote, fetch branch, create worktree, cleanup remote.

Closes #84"
```

---

### Task 7: Run Full Test Suite and Lint

**Step 1: Run all quality checks**

Run: `make check`
Expected: All tests pass, no lint errors

**Step 2: Fix any issues found**

If lint or tests fail, fix the issues and re-run.

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: address lint/test issues from fork PR implementation"
```

---

### Task 8: Update Issue #84 with Implementation Complete

**Step 1: Update the GitHub issue**

Run:
```bash
gh issue comment 84 --body "Implementation complete on branch \`feat/support_fork_prs\`.

Changes:
- Added \`github.ParseForkNotation()\` and \`github.ResolveForkRef()\` to detect and resolve fork notation
- Added \`git.AddRemote()\`, \`git.RemoveRemote()\`, \`git.FetchBranch()\` for remote operations
- Updated \`main.go\` to orchestrate fork flow: resolve → add remote → fetch → create worktree → cleanup

Ready for review."
```

**Step 2: Push branch**

Run: `git push -u origin feat/support_fork_prs`
