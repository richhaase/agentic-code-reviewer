#!/bin/bash
#
# Ralph Wiggum Loop for ACR
# Usage:
#   ./loop.sh <prd.json>                # Run until all stories complete
#   ./loop.sh <prd.json> 5              # Run max 5 iterations
#   ./loop.sh <prd.json> --dry-run      # Show what would run without executing
#
# Example:
#   ./loop.sh specs/fp-filter.prd.json
#   ./loop.sh specs/fp-filter.prd.json 3
#

set -euo pipefail

# Check for PRD argument
if [[ $# -lt 1 ]]; then
    echo "Usage: ./loop.sh <prd.json> [max-iterations|--dry-run]"
    echo ""
    echo "Examples:"
    echo "  ./loop.sh specs/fp-filter.prd.json"
    echo "  ./loop.sh specs/fp-filter.prd.json 5"
    echo "  ./loop.sh specs/fp-filter.prd.json --dry-run"
    exit 1
fi

# Configuration
PRD_FILE="$1"
PROGRESS_FILE="${PRD_FILE%.json}.progress.txt"
PROMPT_FILE="PROMPT_build.md"
MAX_ITERATIONS="${2:-0}"  # 0 = unlimited
ITERATION=0

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[ralph]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[ralph]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[ralph]${NC} $1"
}

log_error() {
    echo -e "${RED}[ralph]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    if [[ ! -f "$PRD_FILE" ]]; then
        log_error "PRD file not found: $PRD_FILE"
        exit 1
    fi

    if [[ ! -f "$PROMPT_FILE" ]]; then
        log_error "Prompt file not found: $PROMPT_FILE"
        exit 1
    fi

    if ! command -v claude &> /dev/null; then
        log_error "claude CLI not found. Install it first."
        exit 1
    fi

    if ! command -v jq &> /dev/null; then
        log_error "jq not found. Install it with: brew install jq"
        exit 1
    fi
}

# Count incomplete stories
count_incomplete() {
    jq '[.userStories[] | select(.passes == false)] | length' "$PRD_FILE"
}

# Count complete stories
count_complete() {
    jq '[.userStories[] | select(.passes == true)] | length' "$PRD_FILE"
}

# Get total story count
count_total() {
    jq '.userStories | length' "$PRD_FILE"
}

# Show current status
show_status() {
    local complete=$(count_complete)
    local total=$(count_total)
    local incomplete=$(count_incomplete)

    log_info "Progress: $complete/$total stories complete ($incomplete remaining)"

    if [[ $incomplete -gt 0 ]]; then
        log_info "Next stories:"
        jq -r '.userStories[] | select(.passes == false) | "  - [\(.id)] \(.title)"' "$PRD_FILE" | head -3
    fi
}

# Main loop
main() {
    check_prerequisites

    log_info "Starting Ralph Wiggum loop"
    log_info "PRD: $PRD_FILE"
    log_info "Prompt: $PROMPT_FILE"
    [[ $MAX_ITERATIONS -gt 0 ]] && log_info "Max iterations: $MAX_ITERATIONS"
    echo ""

    show_status
    echo ""

    while true; do
        # Check if all stories are complete
        local incomplete=$(count_incomplete)
        if [[ $incomplete -eq 0 ]]; then
            log_success "All stories complete!"
            show_status
            exit 0
        fi

        # Check iteration limit
        if [[ $MAX_ITERATIONS -gt 0 && $ITERATION -ge $MAX_ITERATIONS ]]; then
            log_warn "Reached max iterations ($MAX_ITERATIONS)"
            show_status
            exit 0
        fi

        ITERATION=$((ITERATION + 1))
        log_info "=== Iteration $ITERATION ==="

        # Run Claude with the build prompt
        # Using -p for print mode (non-interactive)
        # --dangerously-skip-permissions for autonomous operation
        log_info "Running Claude Code..."

        # Prepend PRD context to the prompt
        FULL_PROMPT="# Current PRD: $PRD_FILE
# Progress File: $PROGRESS_FILE

$(cat "$PROMPT_FILE")"

        if echo "$FULL_PROMPT" | claude -p \
            --dangerously-skip-permissions \
            --verbose; then
            log_success "Iteration $ITERATION completed"
        else
            log_warn "Iteration $ITERATION exited with non-zero status"
        fi

        # Push changes if any
        if [[ -n "$(git status --porcelain)" ]]; then
            log_info "Uncommitted changes detected, Claude should have committed..."
        fi

        # Brief pause between iterations
        sleep 2

        echo ""
        show_status
        echo ""
    done
}

# Handle dry-run
if [[ "${2:-}" == "--dry-run" ]]; then
    check_prerequisites
    log_info "[DRY RUN] Would run Ralph loop with:"
    log_info "  PRD: $PRD_FILE"
    log_info "  Progress: $PROGRESS_FILE"
    log_info "  Prompt: $PROMPT_FILE"
    echo ""
    show_status
    exit 0
fi

# Handle Ctrl+C gracefully
trap 'echo ""; log_warn "Interrupted by user"; show_status; exit 130' INT

main "$@"
