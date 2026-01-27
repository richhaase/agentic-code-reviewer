# `--pr` Flag Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `--pr <number>` flag that fetches a PR branch into a temp worktree and runs code review on it.

**Architecture:** Extend the existing worktree infrastructure. Add two GitHub helper functions to fetch PR metadata, then create a new git function that combines fetching the PR ref with worktree creation. The CLI integrates these with mutual exclusivity validation and auto-detection of base ref.

**Tech Stack:** Go, `gh` CLI, `git` commands, cobra flags

---

## Task 1: Add `GetPRBranch` function to GitHub package

**Files:**
- Modify: `internal/github/pr.go` (add after line 44)
- Test: `internal/github/pr_test.go`

**Step 1: Write the failing test**

Add to `internal/github/pr_test.go`:

```go
func TestParsePRViewJSON_ValidResponse(t *testing.T) {
	json := `{"headRefName": "feature-branch", "baseRefName": "main"}`

	head, base, err := parsePRViewJSON([]byte(json))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "feature-branch" {
		t.Errorf("expected head 'feature-branch', got %q", head)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got %q", base)
	}
}

func TestParsePRViewJSON_InvalidJSON(t *testing.T) {
	_, _, err := parsePRViewJSON([]byte(`not valid json`))

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePRViewJSON_MissingFields(t *testing.T) {
	json := `{"headRefName": "feature-branch"}`

	head, base, err := parsePRViewJSON([]byte(json))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "feature-branch" {
		t.Errorf("expected head 'feature-branch', got %q", head)
	}
	if base != "" {
		t.Errorf("expected empty base, got %q", base)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/github/... -run TestParsePRViewJSON`
Expected: FAIL with "undefined: parsePRViewJSON"

**Step 3: Write minimal implementation**

Add to `internal/github/pr.go` after line 44:

```go
// prViewResponse represents the JSON response from gh pr view.
type prViewResponse struct {
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
}

// parsePRViewJSON parses the JSON output from gh pr view.
func parsePRViewJSON(data []byte) (head, base string, err error) {
	var resp prViewResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", fmt.Errorf("failed to parse PR view response: %w", err)
	}
	return resp.HeadRefName, resp.BaseRefName, nil
}

// GetPRBranch returns the head branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBranch(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "headRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	head, _, err := parsePRViewJSON(out)
	return head, err
}

// GetPRBaseRef returns the base branch name for a PR number.
// Returns ErrNoPRFound if no PR exists, ErrAuthFailed if authentication failed.
func GetPRBaseRef(ctx context.Context, prNumber string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", prNumber, "--json", "baseRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", classifyGHError(err)
	}
	_, base, err := parsePRViewJSON(out)
	return base, err
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/github/... -run TestParsePRViewJSON`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/pr.go internal/github/pr_test.go
git commit -m "feat(github): add GetPRBranch and GetPRBaseRef functions"
```

---

## Task 2: Add `CreateWorktreeFromPR` function to git package

**Files:**
- Modify: `internal/git/worktree.go` (add at end)
- Test: `internal/git/worktree_test.go`

**Step 1: Write the failing test**

Add to `internal/git/worktree_test.go`:

```go
func TestFetchPRRef_CommandFormat(t *testing.T) {
	// This tests the command building logic, not actual git execution
	// We verify the function exists and has correct signature
	repoDir := setupTestRepo(t)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("failed to change to repo dir: %v", err)
	}
	defer os.Chdir(origDir)

	// fetchPRRef will fail because there's no remote, but we can verify
	// the function exists and returns an appropriate error
	err = fetchPRRef(repoDir, "123", "test-branch")
	if err == nil {
		t.Error("expected error when fetching from non-existent remote")
	}
	// Error should mention the fetch failure, not a function signature issue
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("expected fetch-related error, got: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/git/... -run TestFetchPRRef`
Expected: FAIL with "undefined: fetchPRRef"

**Step 3: Write minimal implementation**

Add to `internal/git/worktree.go` at the end:

```go
// fetchPRRef fetches a PR ref from origin into a local branch.
func fetchPRRef(repoRoot, prNumber, branch string) error {
	// Fetch PR head into local branch: git fetch origin pull/N/head:branch
	refSpec := fmt.Sprintf("pull/%s/head:%s", prNumber, branch)
	cmd := exec.Command("git", "fetch", "origin", refSpec)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return fmt.Errorf("failed to fetch PR #%s (%s): %w", prNumber, output, err)
		}
		return fmt.Errorf("failed to fetch PR #%s: %w", prNumber, err)
	}
	return nil
}

// CreateWorktreeFromPR fetches a PR and creates a worktree for it.
// The branchName parameter is the name to use for the local branch.
// The caller is responsible for calling Remove() on the returned Worktree.
func CreateWorktreeFromPR(repoRoot, prNumber, branchName string) (*Worktree, error) {
	// Fetch the PR ref into a local branch
	if err := fetchPRRef(repoRoot, prNumber, branchName); err != nil {
		return nil, err
	}

	// Use existing CreateWorktree to create the worktree
	// But we need to run from the repo root context
	commonDir, err := GetCommonDir()
	if err != nil {
		return nil, err
	}

	if err := ensureWorktreesExcluded(commonDir); err != nil {
		return nil, err
	}

	// Generate unique ID
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("failed to generate worktree ID: %w", err)
	}
	worktreeID := hex.EncodeToString(idBytes)

	safeBranch := strings.ReplaceAll(branchName, "/", "-")
	worktreeName := fmt.Sprintf("review-pr%s-%s-%s", prNumber, safeBranch, worktreeID)
	worktreesDir := filepath.Join(repoRoot, ".worktrees")
	worktreePath := filepath.Join(worktreesDir, worktreeName)

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", worktreePath, branchName)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return nil, fmt.Errorf("failed to create worktree for PR #%s (%s): %w", prNumber, output, err)
		}
		return nil, fmt.Errorf("failed to create worktree for PR #%s: %w", prNumber, err)
	}

	return &Worktree{
		Path:     worktreePath,
		repoRoot: repoRoot,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/git/... -run TestFetchPRRef`
Expected: PASS (error about remote, not undefined function)

**Step 5: Commit**

```bash
git add internal/git/worktree.go internal/git/worktree_test.go
git commit -m "feat(git): add CreateWorktreeFromPR function"
```

---

## Task 3: Add `--pr` flag to CLI

**Files:**
- Modify: `cmd/acr/main.go`

**Step 1: Add the flag variable**

In `cmd/acr/main.go`, add to the var block (around line 39):

```go
	prNumber        string
```

**Step 2: Add the flag definition**

In `cmd/acr/main.go`, add after the `worktree-branch` flag (around line 85):

```go
	rootCmd.Flags().StringVar(&prNumber, "pr", "",
		"Review a PR by number (fetches into temp worktree)")
```

**Step 3: Run tests to verify nothing broke**

Run: `go build ./cmd/acr && ./bin/acr --help | grep -A1 "\-\-pr"`
Expected: Shows the new flag in help output

**Step 4: Commit**

```bash
git add cmd/acr/main.go
git commit -m "feat(cli): add --pr flag definition"
```

---

## Task 4: Add validation and PR worktree logic

**Files:**
- Modify: `cmd/acr/main.go`

**Step 1: Add import for github package**

In `cmd/acr/main.go`, add to imports:

```go
	"github.com/richhaase/agentic-code-reviewer/internal/github"
```

**Step 2: Add mutual exclusivity check and PR worktree logic**

In `cmd/acr/main.go`, in `runReview` function, replace the worktree handling block (lines 140-159) with:

```go
	// Handle worktree-based review
	var workDir string

	// Validate mutual exclusivity
	if prNumber != "" && worktreeBranch != "" {
		logger.Log("--pr and --worktree-branch are mutually exclusive", terminal.StyleError)
		return exitCode(domain.ExitError)
	}

	// Handle PR-based review
	if prNumber != "" {
		if err := github.CheckGHAvailable(); err != nil {
			logger.Logf(terminal.StyleError, "--pr requires gh CLI: %v", err)
			return exitCode(domain.ExitError)
		}

		logger.Logf(terminal.StyleInfo, "Fetching PR %s#%s%s",
			terminal.Color(terminal.Bold), prNumber, terminal.Color(terminal.Reset))

		// Get PR branch name
		branchName, err := github.GetPRBranch(ctx, prNumber)
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}

		// Auto-detect base ref if not explicitly set
		if !cmd.Flags().Changed("base") {
			if detectedBase, err := github.GetPRBaseRef(ctx, prNumber); err == nil && detectedBase != "" {
				baseRef = detectedBase
				logger.Logf(terminal.StyleDim, "Auto-detected base: %s", baseRef)
			}
		}

		// Get repo root for worktree creation
		repoRoot, err := git.GetRoot()
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}

		wt, err := git.CreateWorktreeFromPR(repoRoot, prNumber, branchName)
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
		}
		defer func() {
			logger.Log("Cleaning up worktree", terminal.StyleDim)
			_ = wt.Remove()
		}()

		logger.Logf(terminal.StyleSuccess, "Worktree ready %s(%s)%s",
			terminal.Color(terminal.Dim), wt.Path, terminal.Color(terminal.Reset))
		workDir = wt.Path
	} else if worktreeBranch != "" {
		logger.Logf(terminal.StyleInfo, "Creating worktree for %s%s%s",
			terminal.Color(terminal.Bold), worktreeBranch, terminal.Color(terminal.Reset))

		wt, err := git.CreateWorktree(worktreeBranch)
		if err != nil {
			logger.Logf(terminal.StyleError, "Error: %v", err)
			return exitCode(domain.ExitError)
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

**Step 3: Run build to verify it compiles**

Run: `go build ./cmd/acr`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add cmd/acr/main.go
git commit -m "feat(cli): integrate --pr flag with worktree creation"
```

---

## Task 5: Run full test suite and lint

**Step 1: Run all tests**

Run: `make test`
Expected: All tests pass

**Step 2: Run lint**

Run: `make lint`
Expected: No lint errors

**Step 3: Run full check**

Run: `make check`
Expected: All checks pass

**Step 4: Final commit if any fixes needed**

If fixes were needed:
```bash
git add -A
git commit -m "fix: address lint and test issues"
```

---

## Task 6: Manual testing

**Test scenarios to verify:**

1. **Happy path:** `acr --pr <real-pr-number> --local`
   - Should fetch PR, create worktree, run review, cleanup

2. **Non-existent PR:** `acr --pr 999999 --local`
   - Should show "PR not found" error

3. **Base override:** `acr --pr <number> -b main --local`
   - Should use `main` instead of auto-detected base

4. **Mutual exclusivity:** `acr --pr 123 --worktree-branch foo`
   - Should error with "mutually exclusive" message

5. **No gh CLI:** (temporarily rename gh) `acr --pr 123`
   - Should error with "--pr requires gh CLI"

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add `GetPRBranch`/`GetPRBaseRef` | `internal/github/pr.go`, `pr_test.go` |
| 2 | Add `CreateWorktreeFromPR` | `internal/git/worktree.go`, `worktree_test.go` |
| 3 | Add `--pr` flag definition | `cmd/acr/main.go` |
| 4 | Add validation and integration | `cmd/acr/main.go` |
| 5 | Run test suite and lint | - |
| 6 | Manual testing | - |
