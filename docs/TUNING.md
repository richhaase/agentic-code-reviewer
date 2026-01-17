# Agent Prompt Tuning

## Goal

Tune agent prompts (especially Claude) to produce high-signal code review findings. Target: fewer than 10 findings per review, focused on bugs and real issues rather than style nitpicks.

## Problem

Claude produces ~5x more findings than Codex, with many false positives and low-value nitpicks. Reviews should surface actionable bugs, not overwhelm with noise.

## Approach

1. Run ACR against a test branch with each agent (codex, claude, gemini)
2. Log results in `docs/prompt-tuning-log.md`
3. Analyze finding quality: bugs vs style, actionable vs noise
4. Adjust prompts in `internal/agent/prompts.go`
5. Re-run and compare

## Prompt Location

Agent-specific default prompts: `internal/agent/prompts.go`

## Quality Criteria

Good findings:
- Bugs, logic errors, security issues
- Missing error handling that matters
- Race conditions, resource leaks

Skip:
- Style preferences
- Test file commentary
- Log/debug file nitpicks
- "Consider adding..." suggestions

## Using the Log

The tuning log (`prompt-tuning-log.md`) contains raw run data. Compare finding counts and notes across runs to measure progress. Baseline is the first codex run.
