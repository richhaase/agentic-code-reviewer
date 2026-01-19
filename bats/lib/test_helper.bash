#!/usr/bin/env bash
# test_helper.bash - Common setup/teardown for ACR eval tests

# Resolve ACR binary path
ACR_BIN="${ACR_BIN:-$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/acr}"

if [[ ! -x "$ACR_BIN" ]]; then
    echo "ACR binary not found at $ACR_BIN - run 'make build' first" >&2
    exit 1
fi

export ACR_BIN

# Create a temporary workspace for the test
setup_workspace() {
    EVAL_WORKSPACE="$(mktemp -d)"
    export EVAL_WORKSPACE
    cd "$EVAL_WORKSPACE" || exit 1
}

# Clean up the temporary workspace
teardown_workspace() {
    if [[ -n "$EVAL_WORKSPACE" && -d "$EVAL_WORKSPACE" ]]; then
        rm -rf "$EVAL_WORKSPACE"
    fi
}

# Clone a repository into the workspace
# Usage: clone_repo "owner/repo" [ref]
clone_repo() {
    local repo="$1"
    local ref="${2:-main}"

    # Try gh first (handles auth), fall back to git clone
    if ! gh repo clone "$repo" repo -- --depth 1 --branch "$ref" 2>/dev/null; then
        if ! git clone --depth 1 --branch "$ref" "https://github.com/$repo.git" repo 2>&1; then
            echo "Failed to clone $repo (ref: $ref) - check repo exists and ref is valid" >&2
            exit 1
        fi
    fi

    cd repo || exit 1
}

# Clone and checkout a PR
# Usage: clone_pr "owner/repo" pr_number
clone_pr() {
    local repo="$1"
    local pr_number="$2"

    # Try gh first (handles auth), fall back to git clone
    if ! gh repo clone "$repo" repo -- --depth 1 2>/dev/null; then
        if ! git clone --depth 1 "https://github.com/$repo.git" repo 2>&1; then
            echo "Failed to clone $repo - check repo exists and you have access" >&2
            exit 1
        fi
    fi

    cd repo || exit 1

    if ! gh pr checkout "$pr_number" 2>&1; then
        echo "Failed to checkout PR #$pr_number - check PR exists and gh is authenticated" >&2
        exit 1
    fi
}
