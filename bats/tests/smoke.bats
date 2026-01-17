#!/usr/bin/env bats
# smoke.bats - Verify ACR runs successfully with different agents and prompts

load '../lib/test_helper'

# All tests run against the ACR repo itself (current directory)
# Success = exit 0 (no findings) or exit 1 (findings found)
# Failure = exit 2 (error)

PROMPT_FILE="$BATS_TEST_DIRNAME/../fixtures/test-prompt.md"

@test "codex reviewer (default)" {
    run "$ACR_BIN" --local --reviewers 1
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "claude reviewer" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent claude
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "gemini reviewer" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent gemini
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "codex reviewer with custom prompt" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent codex --prompt-file "$PROMPT_FILE"
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "claude reviewer with custom prompt" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent claude --prompt-file "$PROMPT_FILE"
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "gemini reviewer with custom prompt" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent gemini --prompt-file "$PROMPT_FILE"
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}
