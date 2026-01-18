#!/usr/bin/env bats
# custom-summarizer.bats - Test ACR with different summarizer agents (experimental)

load '../lib/test_helper'

# All tests run against the ACR repo itself (current directory)
# Success = exit 0 (no findings) or exit 1 (findings found)
# Failure = exit 2 (error)

# These tests use 3 codex reviewers with different summarizer agents

@test "codex reviewers with claude summarizer" {
    run "$ACR_BIN" --local --reviewers 3 --reviewer-agent codex --summarizer-agent claude
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "codex reviewers with gemini summarizer" {
    run "$ACR_BIN" --local --reviewers 3 --reviewer-agent codex --summarizer-agent gemini
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}
