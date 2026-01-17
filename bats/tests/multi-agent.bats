#!/usr/bin/env bats
# multi-agent.bats - Test ACR with different reviewer agents (experimental)

load '../lib/test_helper'

# All tests run against the ACR repo itself (current directory)
# Success = exit 0 (no findings) or exit 1 (findings found)
# Failure = exit 2 (error)

@test "claude reviewer" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent claude
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}

@test "gemini reviewer" {
    run "$ACR_BIN" --local --reviewers 1 --reviewer-agent gemini
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}
