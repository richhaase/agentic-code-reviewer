# CI Support via API-Direct Agents

## Problem

ACR shells out to CLI tools (`codex`, `claude`, `gemini`) that manage their own authentication via interactive login flows. This makes ACR unusable in CI environments where you can't run `claude login` interactively. Users consistently ask for CI support, particularly GitHub Actions.

## Solution

Two changes:

1. **API-direct agent implementations** — new Agent interface implementations that call LLM provider APIs directly via HTTP, requiring only an API key in an environment variable. No CLI tools needed.
2. **GitHub Action** — a composite action in `action/` that installs ACR and runs it on PRs.

## Agent Resolution

When ACR creates an agent (e.g., `"claude"`), the factory uses this precedence:

1. **API key in env** → use API-direct agent (no CLI needed)
2. **No API key, CLI installed** → use existing CLI agent (current behavior, unchanged)
3. **Neither** → error with helpful message (e.g., "Set ANTHROPIC_API_KEY or install the claude CLI")

This is fully backwards-compatible. Existing users see no change.

| Agent Name | API Key Env Var | API Endpoint |
|------------|----------------|--------------|
| `claude` | `ANTHROPIC_API_KEY` | `POST https://api.anthropic.com/v1/messages` |
| `codex` | `OPENAI_API_KEY` | `POST https://api.openai.com/v1/chat/completions` |
| `gemini` | `GEMINI_API_KEY` | `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` |

### Auth Headers

| Provider | Header |
|----------|--------|
| Anthropic | `x-api-key: $KEY` |
| OpenAI | `Authorization: Bearer $KEY` |
| Google | `x-goog-api-key: $KEY` |

## Default Models

| Agent | Default Model | Env Override |
|-------|--------------|--------------|
| Claude | `claude-sonnet-4-6` | `ACR_ANTHROPIC_MODEL` |
| Codex/OpenAI | `gpt-5.4` | `ACR_OPENAI_MODEL` |
| Gemini | `gemini-3.0-flash` | `ACR_GOOGLE_MODEL` |

Also configurable via `.acr.yaml`:

```yaml
models:
  claude: claude-sonnet-4-6
  codex: gpt-5.4
  gemini: gemini-3.0-flash
```

## API Agent Implementation

Each API agent implements the existing `Agent` interface:

```go
type AnthropicAPIAgent struct {
    apiKey string
    model  string
}

func (a *AnthropicAPIAgent) Name() string              { return "claude" }
func (a *AnthropicAPIAgent) IsAvailable() error         { /* check apiKey non-empty */ }
func (a *AnthropicAPIAgent) ExecuteReview(ctx, config)  { /* single API call */ }
func (a *AnthropicAPIAgent) ExecuteSummary(ctx, prompt, input) { /* single API call */ }
```

Each review/summary is a single HTTP request:
- Build a messages payload (system prompt + user message with diff)
- POST to provider API
- Extract text from response
- Return as `ExecutionResult` (wrapping response text in `bytes.Reader`)

The existing parsers (`*_review_parser.go`, `*_summary_parser.go`) are reused unchanged — the API agents produce the same output format the CLIs do.

### Context and Cancellation

Use `http.NewRequestWithContext(ctx, ...)` so timeouts and cancellation work identically to the CLI path.

### Error Mapping

| HTTP Status | Mapped Behavior |
|-------------|----------------|
| 401/403 | Auth failure (skip retries, show hint) |
| 429 | Rate limited (retryable via runner retry logic) |
| 5xx | Transient error (retryable) |

### Shared HTTP Helper

A thin `internal/agent/httpclient.go` provides shared utilities:
- Build request with headers
- Check response status
- Extract text content from provider-specific response formats

No external HTTP/SDK dependencies. Uses Go's `net/http` + `encoding/json`.

## GitHub Action

### Structure

```
action/
  action.yml          # Composite action definition
  install-acr.sh      # Download ACR binary from GitHub Releases
```

### Inputs

| Input | Default | Description |
|-------|---------|-------------|
| `fail-on-findings` | `false` | Fail the check if findings exist |
| `acr-version` | `latest` | ACR version to install |

No agent/reviewer/timeout inputs. Review behavior comes from `.acr.yaml` — single source of truth, no config duplication between the action and ACR config.

### Action Flow

1. Download ACR binary (from GitHub Releases, based on `acr-version` + runner OS/arch)
2. Detect PR number from `$GITHUB_EVENT_PATH`
3. Run `acr --pr <number> --yes`
4. If `fail-on-findings` is false, convert exit code 1 (findings) to 0
5. Set outputs (`findings-count`, `exit-code`)

### Example Consumer Workflow

```yaml
name: Code Review
on: [pull_request]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - uses: richhaase/agentic-code-reviewer/action@v1
        with:
          fail-on-findings: false
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

The `gh` CLI is pre-installed on GitHub runners. `GITHUB_TOKEN` is automatically available.

## What Changes

### New Files

- `internal/agent/anthropic_api.go` — Anthropic API agent
- `internal/agent/openai_api.go` — OpenAI API agent
- `internal/agent/google_api.go` — Google API agent
- `internal/agent/httpclient.go` — Shared HTTP helper
- `action/action.yml` — GitHub Action definition
- `action/install-acr.sh` — ACR installer script

### Modified Files

- `internal/agent/factory.go` — API-key-then-CLI resolution logic
- `internal/config/config.go` — Model configuration options

### Unchanged

- All parsers (`*_review_parser.go`, `*_summary_parser.go`)
- All domain types (`internal/domain/`)
- Runner/orchestration (`internal/runner/`)
- Summarizer (`internal/summarizer/`)
- FP filter (`internal/fpfilter/`)
- Feedback (`internal/feedback/`)
- GitHub integration (`internal/github/`)
- Terminal UI (`internal/terminal/`)
- CLI entry point (`cmd/acr/`)

## Testing

- Unit tests for each API agent (mock HTTP responses)
- Unit tests for factory resolution logic (API key present → API agent; CLI available → CLI agent; neither → error)
- Unit tests for HTTP helper (response parsing, error mapping)
- Integration test in CI that runs ACR with API keys against a test PR
