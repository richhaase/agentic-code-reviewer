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

    gh repo clone "$repo" repo -- --depth 1 --branch "$ref" 2>/dev/null || \
        git clone --depth 1 --branch "$ref" "https://github.com/$repo.git" repo

    cd repo || exit 1
}

# Clone and checkout a PR
# Usage: clone_pr "owner/repo" pr_number
clone_pr() {
    local repo="$1"
    local pr_number="$2"

    gh repo clone "$repo" repo -- --depth 1 2>/dev/null || \
        git clone --depth 1 "https://github.com/$repo.git" repo

    cd repo || exit 1
    gh pr checkout "$pr_number"
}
