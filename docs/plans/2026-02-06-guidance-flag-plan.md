# Guidance Flag Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `--prompt`/`--prompt-file` with `--guidance`/`--guidance-file` that appends steering context to built-in prompts via a `{{guidance}}` placeholder.

**Architecture:** Guidance text is resolved via the existing config precedence system (flags > env > config > defaults), then passed through the pipeline as a string. Each agent's default prompt template contains a `{{guidance}}` placeholder that `RenderPrompt()` either fills or strips. Codex uses its built-in `codex exec review` command and pipes guidance via stdin when provided.

**Tech Stack:** Go 1.24, Cobra CLI, YAML config

**Design doc:** `docs/plans/2026-02-06-guidance-flag-design.md`

---

### Task 1: Add RenderPrompt and update prompt templates

**Files:**
- Modify: `internal/agent/prompts.go`

**Step 1: Write the failing test**

Create `internal/agent/prompts_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

func TestRenderPrompt(t *testing.T) {
	tests := []struct {
		name     string
		template string
		guidance string
		want     string
	}{
		{
			name:     "empty guidance strips placeholder",
			template: "Review this diff.{{guidance}}\n\nOutput: file:line: desc",
			guidance: "",
			want:     "Review this diff.\n\nOutput: file:line: desc",
		},
		{
			name:     "non-empty guidance injects section",
			template: "Review this diff.{{guidance}}\n\nOutput: file:line: desc",
			guidance: "Focus on auth issues.",
			want:     "Review this diff.\n\nAdditional context:\nFocus on auth issues.\n\nOutput: file:line: desc",
		},
		{
			name:     "no placeholder in template is no-op",
			template: "Review this diff.",
			guidance: "Focus on auth issues.",
			want:     "Review this diff.",
		},
		{
			name:     "multiline guidance preserved",
			template: "Review.{{guidance}}\nDone.",
			guidance: "Line one.\nLine two.",
			want:     "Review.\n\nAdditional context:\nLine one.\nLine two.\nDone.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderPrompt(tt.template, tt.guidance)
			if got != tt.want {
				t.Errorf("RenderPrompt() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestDefaultPrompts_ContainPlaceholder(t *testing.T) {
	prompts := map[string]string{
		"DefaultClaudePrompt":        DefaultClaudePrompt,
		"DefaultClaudeRefFilePrompt": DefaultClaudeRefFilePrompt,
		"DefaultGeminiPrompt":        DefaultGeminiPrompt,
		"DefaultGeminiRefFilePrompt": DefaultGeminiRefFilePrompt,
	}
	for name, p := range prompts {
		if !strings.Contains(p, "{{guidance}}") {
			t.Errorf("%s missing {{guidance}} placeholder", name)
		}
	}
}

func TestRenderPrompt_DefaultPrompts_NoGuidance(t *testing.T) {
	// Verify that rendering default prompts with empty guidance produces
	// clean output with no placeholder artifacts
	prompts := map[string]string{
		"Claude":        DefaultClaudePrompt,
		"ClaudeRefFile": DefaultClaudeRefFilePrompt,
		"Gemini":        DefaultGeminiPrompt,
		"GeminiRefFile": DefaultGeminiRefFilePrompt,
	}
	for name, p := range prompts {
		rendered := RenderPrompt(p, "")
		if strings.Contains(rendered, "{{guidance}}") {
			t.Errorf("%s: rendered prompt still contains placeholder", name)
		}
		if strings.Contains(rendered, "Additional context:") {
			t.Errorf("%s: rendered prompt contains guidance header with empty guidance", name)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestRenderPrompt|TestDefaultPrompts" -v`
Expected: FAIL — `RenderPrompt` undefined, prompts missing placeholder

**Step 3: Implement RenderPrompt and add placeholders to prompts**

In `internal/agent/prompts.go`, add the `RenderPrompt` function and a `"strings"` import:

```go
import "strings"

// RenderPrompt substitutes the {{guidance}} placeholder in a prompt template.
// If guidance is empty, the placeholder is stripped cleanly.
// If guidance is non-empty, it is injected as an "Additional context:" section.
func RenderPrompt(template, guidance string) string {
	if guidance == "" {
		return strings.ReplaceAll(template, "{{guidance}}", "")
	}
	section := "\n\nAdditional context:\n" + guidance
	return strings.ReplaceAll(template, "{{guidance}}", section)
}
```

Update the 4 prompt constants to add `{{guidance}}` at the end:

- `DefaultClaudePrompt`: append `{{guidance}}` after `Output format: file:line: description`
- `DefaultClaudeRefFilePrompt`: append `{{guidance}}` after `Output format: file:line: description`
- `DefaultGeminiPrompt`: append `{{guidance}}` after `Review the changes now and output your findings.`
- `DefaultGeminiRefFilePrompt`: append `{{guidance}}` after `Review the changes now and output your findings.`

Delete `DefaultCodexRefFilePrompt` entirely (Codex built-in review handles diffs internally).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run "TestRenderPrompt|TestDefaultPrompts" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/prompts.go internal/agent/prompts_test.go
git commit -m "feat: add RenderPrompt and {{guidance}} placeholder to prompt templates"
```

---

### Task 2: Update config types and ResolveGuidance

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go` (replacing existing `TestResolvePrompt` and related tests):

```go
func TestResolveGuidance(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		envState  EnvState
		flagState FlagState
		flagVals  ResolvedConfig
		configDir string
		want      string
		wantErr   bool
	}{
		{
			name: "flag guidance wins",
			flagState: FlagState{GuidanceSet: true},
			flagVals:  ResolvedConfig{Guidance: "from flag"},
			want:      "from flag",
		},
		{
			name: "flag guidance-file wins over env",
			flagState: FlagState{GuidanceFileSet: true},
			flagVals:  ResolvedConfig{GuidanceFile: ""}, // will be set to temp file in test
			envState:  EnvState{Guidance: "from env", GuidanceSet: true},
			want:      "", // placeholder — real test uses temp file
		},
		{
			name:     "env ACR_GUIDANCE wins over config",
			envState: EnvState{Guidance: "from env", GuidanceSet: true},
			cfg:      &Config{GuidanceFile: strPtr("guidance.md")},
			want:     "from env",
		},
		{
			name: "nothing set returns empty",
			want: "",
		},
	}
	// ... (table-driven execution)
}
```

The full test will be written by the implementer following the pattern of the existing `TestResolvePrompt` (lines 723-899 in config_test.go). Key cases:

1. `--guidance` flag text → return text
2. `--guidance-file` flag path → read file, return contents
3. `ACR_GUIDANCE` env → return text
4. `ACR_GUIDANCE_FILE` env → read file, return contents
5. Config `guidance_file` → read file (relative to configDir), return contents
6. Nothing set → return empty string
7. File read errors → return error
8. Relative path resolved against configDir
9. Absolute path used as-is

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestResolveGuidance -v`
Expected: FAIL — `ResolveGuidance` undefined

**Step 3: Update config types and implement ResolveGuidance**

In `internal/config/config.go`:

1. **Config struct**: Remove `ReviewPrompt *string` and `ReviewPromptFile *string`. Add `GuidanceFile *string \`yaml:"guidance_file"\``.

2. **ResolvedConfig**: Remove `ReviewPrompt string` and `ReviewPromptFile string`. Add `Guidance string` and `GuidanceFile string`.

3. **FlagState**: Remove `ReviewPromptSet` and `ReviewPromptFileSet`. Add `GuidanceSet bool` and `GuidanceFileSet bool`.

4. **EnvState**: Remove `ReviewPrompt string`, `ReviewPromptSet bool`, `ReviewPromptFile string`, `ReviewPromptFileSet bool`. Add `Guidance string`, `GuidanceSet bool`, `GuidanceFile string`, `GuidanceFileSet bool`.

5. **LoadEnvState()**: Remove `ACR_REVIEW_PROMPT` and `ACR_REVIEW_PROMPT_FILE` handling. Add:
   ```go
   if v := os.Getenv("ACR_GUIDANCE"); v != "" {
       state.Guidance = v
       state.GuidanceSet = true
   }
   if v := os.Getenv("ACR_GUIDANCE_FILE"); v != "" {
       state.GuidanceFile = v
       state.GuidanceFileSet = true
   }
   ```

6. **Resolve()**: Remove the `ReviewPrompt`/`ReviewPromptFile` resolution blocks from all three tiers (config, env, flags). Guidance is NOT resolved here — it's resolved by `ResolveGuidance()` which is called separately (same pattern as the old `ResolvePrompt`).

7. **knownTopLevelKeys**: Remove `"review_prompt"` and `"review_prompt_file"`. Add `"guidance_file"`.

8. **Delete** the entire `ResolvePrompt()` function.

9. **Add** `ResolveGuidance()`:
   ```go
   func ResolveGuidance(cfg *Config, envState EnvState, flagState FlagState, flagValues ResolvedConfig, configDir string) (string, error) {
       // 1. --guidance flag
       if flagState.GuidanceSet && flagValues.Guidance != "" {
           return flagValues.Guidance, nil
       }
       // 2. --guidance-file flag
       if flagState.GuidanceFileSet && flagValues.GuidanceFile != "" {
           content, err := os.ReadFile(flagValues.GuidanceFile)
           if err != nil {
               return "", fmt.Errorf("failed to read guidance file %q: %w", flagValues.GuidanceFile, err)
           }
           return string(content), nil
       }
       // 3. ACR_GUIDANCE env
       if envState.GuidanceSet && envState.Guidance != "" {
           return envState.Guidance, nil
       }
       // 4. ACR_GUIDANCE_FILE env
       if envState.GuidanceFileSet && envState.GuidanceFile != "" {
           content, err := os.ReadFile(envState.GuidanceFile)
           if err != nil {
               return "", fmt.Errorf("failed to read guidance file %q: %w", envState.GuidanceFile, err)
           }
           return string(content), nil
       }
       // 5. Config guidance_file
       if cfg != nil && cfg.GuidanceFile != nil && *cfg.GuidanceFile != "" {
           guidancePath := *cfg.GuidanceFile
           if !filepath.IsAbs(guidancePath) && configDir != "" {
               guidancePath = filepath.Join(configDir, guidancePath)
           }
           content, err := os.ReadFile(guidancePath)
           if err != nil {
               return "", fmt.Errorf("failed to read guidance file %q: %w", *cfg.GuidanceFile, err)
           }
           return string(content), nil
       }
       // 6. No guidance
       return "", nil
   }
   ```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestResolveGuidance -v`
Expected: PASS

**Step 5: Update remaining config tests**

- Delete or rewrite `TestResolvePrompt`, `TestResolvePrompt_Precedence`, `TestResolvePrompt_ConfigFileRelativePath`, `TestResolvePrompt_ConfigFileAbsolutePath` to test `ResolveGuidance` instead.
- Update `TestLoadFromPathWithWarnings_NoWarningsForValidConfig` to use `guidance_file` instead of `review_prompt`/`review_prompt_file`.
- Update any tests referencing `ReviewPrompt`/`ReviewPromptFile` in `Config`, `ResolvedConfig`, `FlagState`, or `EnvState`.

**Step 6: Run full config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: replace ResolvePrompt with ResolveGuidance in config"
```

---

### Task 3: Update ReviewConfig and runner.Config

**Files:**
- Modify: `internal/agent/config.go`
- Modify: `internal/runner/runner.go`

**Step 1: Update ReviewConfig**

In `internal/agent/config.go`, rename `CustomPrompt string` to `Guidance string` and update the comment:

```go
// Guidance is optional steering context appended to the agent's default prompt.
// If empty, the agent uses its default prompt as-is.
Guidance string
```

**Step 2: Update runner.Config**

In `internal/runner/runner.go`, rename `CustomPrompt string` to `Guidance string` in the `Config` struct (line 31).

**Step 3: Update runner.go usage**

In `runReviewer()` (line 194), change:
```go
CustomPrompt: r.config.CustomPrompt,
```
to:
```go
Guidance: r.config.Guidance,
```

**Step 4: Run runner tests**

Run: `go test ./internal/runner/ -v`
Expected: PASS (runner tests don't set CustomPrompt)

**Step 5: Commit**

```bash
git add internal/agent/config.go internal/runner/runner.go
git commit -m "refactor: rename CustomPrompt to Guidance in ReviewConfig and runner.Config"
```

---

### Task 4: Update Claude agent

**Files:**
- Modify: `internal/agent/claude.go`
- Modify: `internal/agent/claude_test.go`

**Step 1: Delete BuildRefFilePrompt and update tests**

In `claude_test.go`, delete `TestBuildRefFilePrompt` (lines 102-162).

**Step 2: Simplify ExecuteReview**

In `claude.go`, replace the `ExecuteReview` method. The new logic:

- Determine ref-file mode (same threshold check)
- If ref-file: `prompt = fmt.Sprintf(DefaultClaudeRefFilePrompt, absPath)` then `prompt = RenderPrompt(prompt, config.Guidance)`
- If standard: `prompt = RenderPrompt(DefaultClaudePrompt, config.Guidance)` then `prompt = BuildPromptWithDiff(prompt, diff)`
- Delete `BuildRefFilePrompt()` function

Key change: No more `if config.CustomPrompt != ""` / `else` branching. Always use the default template with `RenderPrompt`.

Note: For ref-file prompts, the `%s` format specifier must be resolved with `fmt.Sprintf` BEFORE calling `RenderPrompt`, since `RenderPrompt` operates on the fully-formed template string.

**Step 3: Run agent tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/claude.go internal/agent/claude_test.go
git commit -m "refactor: simplify Claude agent to use RenderPrompt with guidance"
```

---

### Task 5: Update Gemini agent

**Files:**
- Modify: `internal/agent/gemini.go`
- Modify: `internal/agent/gemini_test.go`

**Step 1: Delete BuildGeminiRefFilePrompt and update tests**

In `gemini_test.go`, delete `TestBuildGeminiRefFilePrompt` (lines 102-162).

**Step 2: Simplify ExecuteReview**

Same pattern as Claude (Task 4):

- Ref-file: `prompt = fmt.Sprintf(DefaultGeminiRefFilePrompt, absPath)` then `prompt = RenderPrompt(prompt, config.Guidance)`
- Standard: `prompt = RenderPrompt(DefaultGeminiPrompt, config.Guidance)` then `prompt = BuildPromptWithDiff(prompt, diff)`
- Delete `BuildGeminiRefFilePrompt()` function

**Step 3: Run agent tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/gemini.go internal/agent/gemini_test.go
git commit -m "refactor: simplify Gemini agent to use RenderPrompt with guidance"
```

---

### Task 6: Update Codex agent

**Files:**
- Modify: `internal/agent/codex.go`
- Modify: `internal/agent/codex_test.go`

**Step 1: Delete BuildCodexRefFilePrompt and update tests**

In `codex_test.go`, delete `TestBuildCodexRefFilePrompt` (lines 102-162).

**Step 2: Rewrite ExecuteReview**

Replace the entire method. The new logic:

```go
func (c *CodexAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
    if err := c.IsAvailable(); err != nil {
        return nil, err
    }

    var stdin io.Reader
    args := []string{"exec", "--json", "--color", "never", "review", "--base", config.BaseRef}

    if config.Guidance != "" {
        // Pipe guidance via stdin to codex exec review
        args = append(args, "-")
        stdin = bytes.NewReader([]byte(config.Guidance))
    }

    return executeCommand(ctx, executeOptions{
        Command: "codex",
        Args:    args,
        Stdin:   stdin,
        WorkDir: config.WorkDir,
    })
}
```

Key changes:
- Always uses `codex exec review` (never `codex exec -` with full prompt)
- No diff fetching (Codex built-in review handles that)
- No ref-file mode (Codex handles it internally)
- No `BuildCodexRefFilePrompt` — delete the function
- Delete `DefaultCodexRefFilePrompt` from `prompts.go` (already done in Task 1)
- Remove unused imports (`fmt`, `os`, `strings` if no longer needed)

**Step 3: Simplify ExecuteSummary**

Remove unused `strings` import if the only reference was in deleted code. The summary path is unchanged — Codex summary doesn't use guidance.

**Step 4: Run agent tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/codex.go internal/agent/codex_test.go internal/agent/prompts.go
git commit -m "refactor: simplify Codex agent to always use built-in review with optional guidance"
```

---

### Task 7: Update CLI flags and wiring in main.go and review.go

**Files:**
- Modify: `cmd/acr/main.go`
- Modify: `cmd/acr/review.go`

**Step 1: Update main.go**

1. Replace `prompt` and `promptFile` vars (lines 31-32) with:
   ```go
   guidance     string
   guidanceFile string
   ```

2. Delete the two `--prompt`/`--prompt-file` flag definitions (lines 87-90).

3. Add new flag definitions (after the `--no-fetch` flag):
   ```go
   rootCmd.Flags().StringVar(&guidance, "guidance", "",
       "Steering context appended to the review prompt (env: ACR_GUIDANCE)")
   rootCmd.Flags().StringVar(&guidanceFile, "guidance-file", "",
       "Path to file containing review guidance (env: ACR_GUIDANCE_FILE)")
   ```

4. Update `flagState` (lines 327-342): Replace `ReviewPromptSet`/`ReviewPromptFileSet` with:
   ```go
   GuidanceSet:     cmd.Flags().Changed("guidance"),
   GuidanceFileSet: cmd.Flags().Changed("guidance-file"),
   ```

5. Update `flagValues` (lines 352-367): Replace `ReviewPrompt`/`ReviewPromptFile` with:
   ```go
   Guidance:     guidance,
   GuidanceFile: guidanceFile,
   ```

6. Replace the `ResolvePrompt` call (lines 411-416) with:
   ```go
   resolvedGuidance, err := config.ResolveGuidance(cfg, envState, flagState, flagValues, configDir)
   if err != nil {
       logger.Logf(terminal.StyleError, "Failed to resolve guidance: %v", err)
       return exitCode(domain.ExitError)
   }
   ```

7. Update the `executeReview` call (line 432): replace `customPrompt` with `resolvedGuidance`.

**Step 2: Update review.go**

1. Change `executeReview` signature (line 18): rename `customPrompt string` to `guidance string`.

2. Update `runner.Config` construction (lines 73-83): replace `CustomPrompt: customPrompt` with `Guidance: guidance`.

**Step 3: Build and run all tests**

Run: `go build ./cmd/acr/ && go test ./... -v`
Expected: All compile and pass

**Step 4: Commit**

```bash
git add cmd/acr/main.go cmd/acr/review.go
git commit -m "feat: replace --prompt/--prompt-file flags with --guidance/--guidance-file"
```

---

### Task 8: Full quality check

**Step 1: Run make check**

Run: `make check`
Expected: All pass (fmt, lint, vet, staticcheck, tests)

**Step 2: Fix any issues found**

Address lint, vet, or staticcheck warnings. Common issues:
- Unused imports from deleted code paths
- Unused variables
- Unreachable code

**Step 3: Commit fixes if needed**

```bash
git add -A
git commit -m "fix: address lint/vet findings from guidance refactor"
```

---

### Task 9: Manual smoke test

**Step 1: Build**

Run: `go build -o bin/acr ./cmd/acr/`

**Step 2: Test --guidance flag**

Run: `bin/acr --guidance "Focus on security issues" --local -r 1 --base HEAD~1`
Expected: Review runs with guidance appended to default prompt

**Step 3: Test --guidance-file flag**

Create a temp file `test-guidance.md` with guidance text, then:
Run: `bin/acr --guidance-file test-guidance.md --local -r 1 --base HEAD~1`
Expected: Review runs with file contents as guidance

**Step 4: Test no guidance (default behavior unchanged)**

Run: `bin/acr --local -r 1 --base HEAD~1`
Expected: Review runs with default prompt, no guidance artifacts

**Step 5: Test Codex specifically**

Run: `bin/acr --guidance "Focus on error handling" --local -r 1 -a codex --base HEAD~1`
Expected: Codex uses `codex exec review --base ... -` with guidance on stdin

---

## Summary of all files touched

| File | Action |
|------|--------|
| `internal/agent/prompts.go` | Add `RenderPrompt()`, add `{{guidance}}` to 4 prompts, delete `DefaultCodexRefFilePrompt` |
| `internal/agent/prompts_test.go` | New file: `TestRenderPrompt`, `TestDefaultPrompts_ContainPlaceholder`, `TestRenderPrompt_DefaultPrompts_NoGuidance` |
| `internal/agent/config.go` | Rename `CustomPrompt` → `Guidance` |
| `internal/agent/claude.go` | Delete `BuildRefFilePrompt()`, simplify `ExecuteReview` |
| `internal/agent/claude_test.go` | Delete `TestBuildRefFilePrompt` |
| `internal/agent/gemini.go` | Delete `BuildGeminiRefFilePrompt()`, simplify `ExecuteReview` |
| `internal/agent/gemini_test.go` | Delete `TestBuildGeminiRefFilePrompt` |
| `internal/agent/codex.go` | Delete `BuildCodexRefFilePrompt()`, rewrite `ExecuteReview` |
| `internal/agent/codex_test.go` | Delete `TestBuildCodexRefFilePrompt` |
| `internal/config/config.go` | Update types, delete `ResolvePrompt()`, add `ResolveGuidance()`, update known keys |
| `internal/config/config_test.go` | Rewrite prompt tests as guidance tests |
| `internal/runner/runner.go` | Rename `CustomPrompt` → `Guidance` in Config struct and usage |
| `cmd/acr/main.go` | Replace prompt flags/vars with guidance flags/vars, update wiring |
| `cmd/acr/review.go` | Rename `customPrompt` param → `guidance`, update runner.Config |
