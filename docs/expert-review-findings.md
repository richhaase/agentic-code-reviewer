# ACR Expert Review: Findings & Proposals

**Date**: 2026-02-10
**Reviewers**: Systems Expert, Software Architecture Expert, Automation Expert, UX Expert

---

## How to Read This Document

Each item is categorized as:
- **[ISSUE]** — A real problem that should be fixed
- **[PROPOSAL]** — An improvement idea worth considering
- **[QUALITY]** — Something done well (noted in the Quality Summary at the end)

Items are grouped by theme and roughly ordered by impact within each group.

---

## A. Architecture & Code Organization

### A1. [ISSUE] Git diff generated independently per reviewer
**Source**: Systems Expert, Software Expert
**Files**: `internal/agent/claude.go:52`, `internal/agent/gemini.go:49`

Claude and Gemini agents each call `GetGitDiff()` inside `ExecuteReview()`. With N reviewers using these agents, the same `git diff` command runs N times concurrently. This wastes subprocess resources, wall-clock time, and could cause git index contention. Codex avoids this by using its built-in `review --base` mode.

**Recommendation**: Generate the diff once in the runner and pass it to agents via `ReviewConfig`. Agents that need the raw diff (Claude, Gemini) use it directly; Codex ignores it.

---

### A2. [ISSUE] `runReview` is 300+ lines and growing unwieldy
**Source**: Software Expert
**Files**: `cmd/acr/main.go:135-434`

The `runReview` function handles terminal detection, context/signal setup, PR validation, worktree creation, fork detection, config loading, flag state construction, config resolution, base ref fetching/qualification, validation, exclude pattern merging, guidance resolution, and PR auto-detection — all in one function.

**Recommendation**: Extract logical phases into helper functions: `setupWorktree()`, `resolveConfig()`, `qualifyBaseRef()`. Keep `runReview` as the high-level orchestrator.

---

### A3. [ISSUE] Package-level variables for CLI state
**Source**: Software Expert
**Files**: `cmd/acr/main.go:23-47`, `review.go:19-20`, `pr_submit.go:29-31`

20+ package-level variables form implicit shared state. `executeReview` takes 13 parameters but also reads globals like `verbose` and `concurrency` directly. This makes data flow opaque and testing impossible.

**Recommendation**: Create a `ReviewContext` struct holding all resolved configuration and runtime state. Pass it explicitly to `executeReview` and PR submission functions.

---

### A4. [ISSUE] Claude and Gemini ExecuteReview have significant duplication
**Source**: Software Expert
**Files**: `internal/agent/claude.go:46-88`, `internal/agent/gemini.go:43-85`

~40 lines of near-identical structure: availability check, GetGitDiff, ref-file branching, prompt rendering, stdin composition, executeCommand. Only the CLI name, prompt constants, and flags differ.

**Recommendation**: Extract a helper like `executeDiffBasedReview(ctx, config, cliName, args, defaultPrompt, refFilePrompt)`. Codex is genuinely different and shouldn't use it.

---

### A5. [PROPOSAL] The `internal/agent` package is becoming a gravity well
**Source**: Software Expert
**Files**: `internal/agent/` (25+ files)

The agent package accumulates responsibilities beyond agent concerns: git diff operations (`diff.go`), remote ref fetching, temp file management (`reffile.go`), prompt templates, non-finding detection, auth detection, cohort management.

**Recommendation**: If the package continues to grow, extract `diff.go` into `internal/git/` (where `worktree.go` and `remote.go` already live). Keep `reffile.go` in agent but acknowledge it as infrastructure.

---

### A6. [ISSUE] `ExitCoder` and `StderrProvider` interfaces are vestigial
**Source**: Software Expert
**Files**: `internal/agent/agent.go:32-44`

These interfaces are defined but never used. `ExecuteReview` returns `*ExecutionResult` which provides `ExitCode()` and `Stderr()` directly. Dead code.

**Recommendation**: Remove both interfaces.

---

### A7. [ISSUE] Stale `doc.go`
**Source**: Software Expert
**Files**: `internal/agent/doc.go:69-74`

Mentions "Future Implementations" for Claude and Gemini (already implemented). References `AgentConfig` which doesn't exist (it's `ReviewConfig`).

**Recommendation**: Update or remove — the code is self-documenting.

---

## B. Reliability & Systems

### B1. [ISSUE] Unbounded stderr buffer
**Source**: Systems Expert
**Files**: `internal/agent/executor.go:52`

Stderr is captured into `bytes.Buffer{}` with no size limit. A misbehaving or misconfigured agent could produce excessive stderr, causing memory pressure. With N concurrent reviewers, worst case is N * unbounded buffers.

**Recommendation**: Use `io.LimitReader` on the stderr pipe or truncate the buffer after `Wait()` (e.g., cap at 1MB).

---

### B2. [ISSUE] Scanner max buffer is 100MB
**Source**: Systems Expert
**Files**: `internal/agent/codex_review_parser.go:15-16`

`scannerMaxLineSize = 100 * 1024 * 1024`. A single malformed line could cause 100MB allocation. With 5 concurrent reviewers, worst case is 500MB just for scanner buffers.

**Recommendation**: Reduce to 10MB. No legitimate single JSONL line for a code review finding should exceed this.

---

### B3. [PROPOSAL] No summarizer timeout
**Source**: Systems Expert
**Files**: `internal/summarizer/summarizer.go`

The summarizer uses the passed-in context but adds no timeout. A hung summarizer blocks indefinitely until SIGINT. The same applies to the FP filter phase.

**Recommendation**: Add a configurable summarizer/filter timeout, or reuse the reviewer timeout as default.

---

### B4. [PROPOSAL] No jitter in retry backoff
**Source**: Systems Expert
**Files**: `internal/runner/runner.go:153`

Exponential backoff has no jitter. When multiple reviewers fail simultaneously (e.g., rate limits), they all retry at the same times, creating thundering herd behavior.

**Recommendation**: Add random jitter: `delay + rand.Duration(0, delay/2)`.

---

### B5. [ISSUE] Deterministic failures waste retries
**Source**: Systems Expert
**Files**: `internal/agent/claude.go:52-55`

When `GetGitDiff` fails (e.g., base ref doesn't exist), all retries repeat the same failure. The runner doesn't distinguish transient vs permanent failures.

**Recommendation**: Classify errors as transient (timeout, rate limit) vs permanent (missing ref, parse error) and skip retries for permanent failures. This becomes less relevant if A1 (single diff generation) is implemented.

---

### B6. [PROPOSAL] Worktree cleanup on crash
**Source**: Systems Expert
**Files**: `internal/git/worktree.go`

SIGKILL or crashes leak worktrees in `.worktrees/`. They accumulate with random IDs.

**Recommendation**: Add startup cleanup via `git worktree prune` or age-based pruning.

---

## C. Testing & Quality

### C1. [ISSUE] `fpfilter` package nearly untested (10.9% coverage)
**Source**: Automation Expert
**Files**: `internal/fpfilter/filter.go`

The core false positive detection logic (`Filter()`, `Apply()`) has no unit tests. Only helper functions are tested. This is the most impactful untested code path.

**Recommendation**: Add unit tests for empty findings, threshold clamping, evaluation map building, and the fail-open error paths.

---

### C2. [ISSUE] `github` package low coverage (31.1%)
**Source**: Automation Expert
**Files**: `internal/github/pr.go`

Only `ParseCIChecks` and `classifyGHError` (pure functions) are tested. All `gh` CLI interaction functions are untested. The parsing/formatting logic around them could be extracted and tested.

---

### C3. [ISSUE] `feedback` package low coverage (16.7%)
**Source**: Automation Expert
**Files**: `internal/feedback/`

PR context fetching and summarization are largely untested.

---

### C4. [ISSUE] Security workflow `continue-on-error` makes vulnerabilities silent
**Source**: Automation Expert
**Files**: `.github/workflows/security.yml:32,42`

Both `govulncheck` and `gosec` use `continue-on-error: true`. Vulnerabilities never fail the build. The daily cron runs, finds issues, and passes silently.

**Recommendation**: Remove `continue-on-error` from `govulncheck` at minimum. For `gosec`, keep it informational on scheduled runs but fail on PR/push triggers.

---

### C5. [ISSUE] No format check in CI
**Source**: Automation Expert
**Files**: `.github/workflows/ci.yml`

CI runs test, lint, staticcheck, vet — but not `gofmt`. Unformatted code can merge.

**Recommendation**: Add a `fmt` job or switch to running `make check` (which includes fmt).

---

### C6. [ISSUE] No Dependabot/Renovate configuration
**Source**: Automation Expert

Go dependencies and GitHub Actions are never auto-updated. With security-sensitive tooling, this is a meaningful gap.

**Recommendation**: Add `.github/dependabot.yml` for gomod and github-actions ecosystems, weekly schedule.

---

### C7. [PROPOSAL] Add `go test -race` to CI
**Source**: Automation Expert

No race detection despite extensive goroutine usage (parallel reviewers, channels, atomics).

**Recommendation**: Add a `test-race` Makefile target and CI job.

---

### C8. [ISSUE] BATS tests only verify exit codes, not output
**Source**: Automation Expert
**Files**: `bats/tests/*.bats`

All integration tests confirm ACR doesn't crash but don't validate that findings are produced, formatted correctly, or that the summarizer runs.

**Recommendation**: Add output assertions for at least the smoke test (e.g., check for "Findings:" header).

---

### C9. [ISSUE] No coverage reporting or enforcement in CI
**Source**: Automation Expert

Coverage is 56% overall with no gate or trend tracking. The baseline could erode without enforcement.

**Recommendation**: Add `go test -coverprofile` to CI and set a minimum threshold (e.g., 50%).

---

### C10. [PROPOSAL] Gosec exclusion drift between CI and lint config
**Source**: Automation Expert
**Files**: `.github/workflows/security.yml`, `.golangci.yml`

The CI workflow and lint config have different gosec exclusion sets. This creates confusion about which rules are actually enforced.

**Recommendation**: Consolidate into a single source of truth or document the differences.

---

## D. CLI User Experience

### D1. [ISSUE] Empty diff proceeds to LLM review
**Source**: Systems Expert, UX Expert
**Files**: `internal/agent/diff.go:238-241`

When the diff is empty, the tool sends "(No changes detected)" to the LLM agent, which responds (potentially with findings about nothing), wasting time and API costs.

**Recommendation**: Detect empty diff early and exit with LGTM or "No changes detected. Nothing to review."

---

### D2. [ISSUE] Spinner shows no elapsed time
**Source**: UX Expert
**Files**: `internal/terminal/spinner.go:59-66`

The spinner shows "Running reviewers (3/5)" but no elapsed time. For reviews that take 5-10 minutes, users can't estimate remaining time or detect if things are stuck. Same issue with PhaseSpinner (summarizer, FP filter phases).

**Recommendation**: Add elapsed time: "Running reviewers (3/5, 2m 15s)".

---

### D3. [ISSUE] Long description says "Run codex review" but tool supports 3 agents
**Source**: UX Expert
**Files**: `cmd/acr/main.go:57`

The `Long` description is Codex-centric despite supporting codex, claude, and gemini.

**Recommendation**: Change to "Run parallel LLM-powered code reviews" or similar agent-agnostic phrasing.

---

### D4. [PROPOSAL] Add `-p` short flag for `--pr`
**Source**: UX Expert
**Files**: `cmd/acr/main.go:97-98`

`--pr` has no short form despite being one of the most common usage patterns. `acr -p 123` is much more ergonomic.

---

### D5. [ISSUE] Default action on PR submission prompt is "Request changes"
**Source**: UX Expert
**Files**: `cmd/acr/pr_submit.go:216-225`

Pressing Enter defaults to the most destructive option. The prompt doesn't indicate the default.

**Recommendation**: Either make "Comment" the default (safer) or visually indicate the default: "[R]equest changes (default)".

---

### D6. [PROPOSAL] Add `--format json` for machine-readable output
**Source**: UX Expert

No way to get structured output for integration with other tools, dashboards, or CI systems.

**Recommendation**: Add `--format json` / `--output-format json` that emits findings as JSON to stdout.

---

### D7. [PROPOSAL] Add `--dry-run` flag
**Source**: UX Expert

No way to see what acr would do without running reviews. A `--dry-run` that shows resolved config, diff size, and agent selection would help verify setup in CI.

---

### D8. [PROPOSAL] `acr config` subcommand
**Source**: UX Expert

Users can't see resolved configuration or generate starter configs.

**Recommendation**: `acr config show` (resolved config), `acr config init` (starter .acr.yaml), `acr config validate` (check without reviewing).

---

### D9. [ISSUE] No tool attribution in GitHub comments
**Source**: UX Expert
**Files**: `internal/runner/report.go`

GitHub comments start with `## Findings` with no attribution. PR reviewers can't tell the comment's source or version.

**Recommendation**: Add a small footer: "Posted by [acr](repo-url) vX.Y.Z".

---

### D10. [ISSUE] Silent env var parsing failures
**Source**: UX Expert
**Files**: `internal/config/config.go:435-440`

If `ACR_REVIEWERS` is set to a non-integer value (e.g., "five"), it's silently ignored.

**Recommendation**: Log a warning: "Warning: ACR_REVIEWERS='five' is not a valid integer, ignoring."

---

### D11. [ISSUE] Redundant "Error:" prefix in error messages
**Source**: UX Expert
**Files**: `cmd/acr/main.go:205`

`logger.Logf(terminal.StyleError, "Error: %v", err)` — the `StyleError` already renders with a red `[!]` prefix. The message reads "[!] Error: not inside a git repository..."

**Recommendation**: Drop the "Error:" prefix from `StyleError`-styled messages.

---

### D12. [PROPOSAL] Inline PR comments on specific lines
**Source**: UX Expert

Currently all findings go into one PR comment. For findings with file:line information, inline comments would place feedback exactly where the code is — the pattern human reviewers use.

---

### D13. [PROPOSAL] `reviewer_agent` vs `reviewer_agents` config confusion
**Source**: UX Expert
**Files**: `internal/config/config.go:63-64`

Both `reviewer_agent` (string) and `reviewer_agents` (array) are supported, with the array silently taking precedence. Users may not realize one overrides the other.

**Recommendation**: Deprecate `reviewer_agent` with a warning when both are set.

---

### D14. [PROPOSAL] Group flags into categories in help output
**Source**: UX Expert

22+ flags in a flat list. Cobra supports flag groups which would improve scannability: Review settings, Agent settings, PR integration, Filtering, Advanced.

---

### D15. [PROPOSAL] Show phase transitions more prominently
**Source**: UX Expert

The flow (reviewers -> summarizer -> FP filter -> report) could show phase numbers: "Phase 1/4: Running reviewers" etc.

---

## E. Automation & DevOps

### E1. [ISSUE] Stale Homebrew cask caveats
**Source**: Automation Expert
**Files**: `.goreleaser.yaml:129`

Caveats say "It requires the 'codex' CLI" but ACR now supports Claude and Gemini as alternatives.

---

### E2. [PROPOSAL] Add PR and issue templates
**Source**: Automation Expert

No `.github/PULL_REQUEST_TEMPLATE.md` or issue templates. This makes contributor PRs less consistent.

---

### E3. [PROPOSAL] Add CONTRIBUTING.md
**Source**: Automation Expert

README Development section lists make targets but no instructions for required Go version, BATS eval tests, LLM credential setup, or release process.

---

### E4. [PROPOSAL] Automated changelog generation
**Source**: Automation Expert

CHANGELOG is manually maintained. Commits follow conventional commit format, so tools like `git-cliff` or `release-please` could automate this.

---

### E5. [ISSUE] Inconsistent logging: `log.Printf` vs `terminal.Logger`
**Source**: Software Expert
**Files**: `internal/summarizer/summarizer.go:138`, `internal/fpfilter/filter.go:133`

Some packages use `log.Printf` (raw stderr with timestamp) while the runner uses the styled `terminal.Logger`. The output looks inconsistent in the terminal.

**Recommendation**: Use `terminal.Logger` consistently, or accept `*terminal.Logger` as a dependency in summarizer/fpfilter.

---

---

## Quality Summary

Things the experts unanimously praised:

1. **Clean dependency graph** — No circular dependencies. `domain` and `terminal` are true leaf packages.
2. **Process group isolation** — `Setpgid` + `Kill(-pid)` prevents orphaned LLM CLI processes.
3. **Streaming parser design** — Memory-efficient finding-by-finding parsing in the runner.
4. **RecoverableParseError pattern** — Elegant error classification for streaming parse errors.
5. **Registry/factory pattern** — Adding a new agent = implement interfaces + add one map entry.
6. **Three-tier config resolution** — Clean precedence with pointer-based "set vs unset" detection.
7. **Config typo detection** — Levenshtein-based "did you mean?" for unknown YAML keys.
8. **Fail-open FP filter** — Errors pass all findings through rather than silently dropping.
9. **Auth failure detection** — Per-agent exit code + stderr pattern matching with actionable hints.
10. **Self-review detection** — Fails closed (assumes self-review when uncertain), preventing accidental self-approvals.
11. **CI status check before approval** — Prevents rubber-stamp approvals on broken builds.
12. **Comprehensive GoReleaser config** — Cross-platform builds, signing, notarization, Homebrew tap.
13. **Consistent error wrapping** — `fmt.Errorf("...: %w", err)` throughout.
14. **Atomic progress counter** — Correct synchronization for spinner progress.
15. **Compile-time interface checks** — `var _ Agent = (*CodexAgent)(nil)` on every implementation.
