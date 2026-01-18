#!/usr/bin/env bats
# smoke.bats - Verify ACR runs successfully with default configuration

load '../lib/test_helper'

# All tests run against the ACR repo itself (current directory)
# Success = exit 0 (no findings) or exit 1 (findings found)
# Failure = exit 2 (error)

@test "codex reviewer (default)" {
    run "$ACR_BIN" --local --reviewers 1
    [[ "$status" -eq 0 || "$status" -eq 1 ]]
}
