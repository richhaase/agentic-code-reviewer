# Design: Replace --prompt with --guidance steering mechanism

**Issue:** #105
**Date:** 2026-02-06

## Summary

Replace the `--prompt`/`--prompt-file` full-override mechanism with a `--guidance`/`--guidance-file` append mechanism. Guidance is injected into built-in prompt templates via a `{{guidance}}` placeholder, preserving the tuned output format, skip rules, and focus areas while letting users steer reviews with domain context, focus areas, and conventions.

Breaking change: `--prompt`, `--prompt-file`, `ACR_REVIEW_PROMPT`, `ACR_REVIEW_PROMPT_FILE`, `review_prompt`, and `review_prompt_file` are all removed.

## Flag & Config Surface

### Remove

- `--prompt`, `--prompt-file` CLI flags
- `ACR_REVIEW_PROMPT`, `ACR_REVIEW_PROMPT_FILE` env vars
- `review_prompt`, `review_prompt_file` in `.acr.yaml`
- `ResolvePrompt()` function in `config.go`
- `CustomPrompt` field from `runner.Config` and `agent.ReviewConfig`

### Add

- `--guidance` CLI flag (string) -- inline guidance text
- `--guidance-file` CLI flag (string) -- path to guidance file
- `ACR_GUIDANCE`, `ACR_GUIDANCE_FILE` env vars
- `guidance_file` in `.acr.yaml` (no inline `guidance` in YAML config)

### Precedence

1. `--guidance` flag
2. `--guidance-file` flag
3. `ACR_GUIDANCE` env var
4. `ACR_GUIDANCE_FILE` env var
5. `guidance_file` config field
6. Empty string (no guidance)

The resolved value is a plain string that flows through the pipeline as `Guidance string`.

## Prompt Template Changes

Each default prompt constant gets a `{{guidance}}` placeholder. A single function handles substitution:

```go
// In internal/agent/prompts.go
func RenderPrompt(template, guidance string) string {
    if guidance == "" {
        return strings.ReplaceAll(template, "{{guidance}}", "")
    }
    section := "\n\nAdditional context:\n" + guidance
    return strings.ReplaceAll(template, "{{guidance}}", section)
}
```

When guidance is empty, the placeholder disappears cleanly. No orphaned headers, no extra whitespace.

### Placeholder positions

| Constant | Position |
|----------|----------|
| `DefaultClaudePrompt` | After "Output format: file:line: description" |
| `DefaultClaudeRefFilePrompt` | After output format line |
| `DefaultGeminiPrompt` | After "Review the changes now..." |
| `DefaultGeminiRefFilePrompt` | Same position |
| `DefaultCodexRefFilePrompt` | Deleted (see Codex section) |
| Codex default mode | No template -- guidance piped via stdin |

## Agent Execution Changes

### Claude and Gemini

Their `ExecuteReview` methods simplify from custom-vs-default branching to:

```go
prompt = RenderPrompt(DefaultXxxPrompt, config.Guidance)
```

Ref-file mode likewise:

```go
prompt = RenderPrompt(DefaultXxxRefFilePrompt, config.Guidance)
```

The `BuildRefFilePrompt`, `BuildCodexRefFilePrompt`, and `BuildGeminiRefFilePrompt` helper functions are deleted.

### Codex

Two modes, both using `codex exec review`:

1. **No guidance**: `codex exec review --base X --json --color never` (unchanged)
2. **Guidance provided**: `codex exec review --base X --json --color never -` with guidance piped via stdin

The `codex exec --json -` custom prompt code path (which piped an entire prompt + diff) is removed entirely. Codex always uses its built-in `codex exec review` command, which produces higher quality reviews than custom prompts.

`DefaultCodexRefFilePrompt` is deleted since Codex's built-in review handles diff reading internally.

### ReviewConfig struct

`CustomPrompt string` becomes `Guidance string`.

## Config & Resolution Changes

### Config struct

```go
type Config struct {
    // ... existing fields ...
    GuidanceFile *string `yaml:"guidance_file"`
    // ReviewPrompt and ReviewPromptFile deleted
}
```

### ResolvedConfig

Remove `ReviewPrompt`/`ReviewPromptFile`, add `Guidance string`.

### FlagState

Remove `ReviewPromptSet`/`ReviewPromptFileSet`, add `GuidanceSet`/`GuidanceFileSet`.

### EnvState

Remove four `ReviewPrompt*` fields, add `Guidance`/`GuidanceSet` and `GuidanceFile`/`GuidanceFileSet`. `LoadEnvState()` reads `ACR_GUIDANCE` and `ACR_GUIDANCE_FILE`.

### ResolveGuidance()

Replaces `ResolvePrompt()`. Same cascading structure with file reading. Config only has `guidance_file` (no inline), so step 5 always reads a file. Relative paths resolved against config dir.

### Known keys

Update `knownTopLevelKeys`: remove `review_prompt`/`review_prompt_file`, add `guidance_file`.

## Deletions

- `BuildRefFilePrompt()` in `claude.go`
- `BuildCodexRefFilePrompt()` in `codex.go`
- `BuildGeminiRefFilePrompt()` in `gemini.go`
- `DefaultCodexRefFilePrompt` constant in `prompts.go`
- `ResolvePrompt()` in `config.go`
- Codex `codex exec --json -` custom prompt code path

## Test Changes

- `config_test.go` -- replace `review_prompt`/`review_prompt_file` tests with `guidance_file` equivalents. `ResolvePrompt` tests become `ResolveGuidance` tests.
- Agent tests -- remove `BuildRefFilePrompt` tests. Add `RenderPrompt()` tests: empty guidance, non-empty guidance, placeholder absent.
- `codex_test.go` -- replace custom prompt mode tests with guidance-via-stdin tests.
- `runner_test.go` -- `CustomPrompt` references become `Guidance`.

## Packages not affected

Summarizer, FP filter, feedback, filter, domain, github, git, terminal, report rendering.
