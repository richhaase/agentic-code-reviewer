#!/usr/bin/env python3
# Usage: code_review.py [-r N] [-b BASE] [-t SECS] [-R N] [-v] [-l|--local] [-B WORKTREE_BRANCH] [-y|--yes] [-n|--no]
# Env: REVIEW_REVIEWERS, REVIEW_WORKERS, REVIEW_TIMEOUT, REVIEW_BASE_REF, REVIEW_RETRIES
# Exit: 0=no findings, 1=findings, 2=error, 130=interrupted

import argparse
import asyncio
import json
import os
import shlex
import shutil
import signal
import subprocess
import sys
import textwrap
import time
import uuid
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Callable, Generator, List, Optional, Tuple, TypedDict

# ─────────────────────────────────────────────────────────────────────────────
# Constants
# ─────────────────────────────────────────────────────────────────────────────

DEFAULT_REVIEWERS = int(os.environ.get("REVIEW_REVIEWERS", os.environ.get("REVIEW_WORKERS", 5)))
DEFAULT_TIMEOUT = int(os.environ.get("REVIEW_TIMEOUT", 300))
DEFAULT_BASE_REF = os.environ.get("REVIEW_BASE_REF", "main")
DEFAULT_RETRIES = int(os.environ.get("REVIEW_RETRIES", 1))
MAX_REPORT_WIDTH = 90
SPINNER_INTERVAL = 0.2
MAX_RAW_OUTPUT_LINES = 10

# Exit codes (grep-style)
EXIT_NO_FINDINGS = 0
EXIT_FINDINGS = 1
EXIT_ERROR = 2
EXIT_INTERRUPTED = 130


# ─────────────────────────────────────────────────────────────────────────────
# Terminal formatting utilities
# ─────────────────────────────────────────────────────────────────────────────


class Colors:
    RESET = "\033[0m"
    BOLD = "\033[1m"
    DIM = "\033[2m"
    CYAN = "\033[36m"
    GREEN = "\033[32m"
    YELLOW = "\033[33m"
    RED = "\033[31m"
    MAGENTA = "\033[35m"
    WHITE = "\033[97m"
    BLUE = "\033[34m"

    @classmethod
    def disable(cls) -> None:
        for attr in dir(cls):
            if attr.isupper() and not attr.startswith("_"):
                setattr(cls, attr, "")


# Braille spinner frames for smoother animation
SPINNER_FRAMES = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"


def log_styled(
    msg: str,
    style: str = "info",
    prefix: str = "review",
    clear_line: bool = True,
) -> None:
    """Print a styled log message to stderr."""
    c = Colors
    styles = {
        "info": c.CYAN,
        "success": c.GREEN,
        "warning": c.YELLOW,
        "error": c.RED,
        "dim": c.DIM,
        "phase": c.MAGENTA + c.BOLD,
    }
    color = styles.get(style, c.CYAN)

    if clear_line and sys.stderr.isatty():
        sys.stderr.write("\r" + " " * 100 + "\r")

    tag = f"{c.DIM}[{c.RESET}{color}{prefix}{c.RESET}{c.DIM}]{c.RESET}"
    sys.stderr.write(f"{tag} {msg}\n")
    sys.stderr.flush()


def get_terminal_width() -> int:
    try:
        return os.get_terminal_size().columns
    except OSError:
        return 80


def get_ruler(width: int, char: str = "─") -> str:
    return f"{Colors.DIM}{char * width}{Colors.RESET}"


def format_duration(seconds: float) -> str:
    """Format duration in human-readable form."""
    if seconds < 60:
        return f"{seconds:.1f}s"
    minutes = int(seconds // 60)
    secs = seconds % 60
    return f"{minutes}m {secs:.1f}s"


def wrap_text(
    text: str, width: int, initial_indent: str = "", subsequent_indent: str = ""
) -> str:
    """Wrap text to width with proper indentation."""
    return textwrap.fill(
        text,
        width=width,
        initial_indent=initial_indent,
        subsequent_indent=subsequent_indent or initial_indent,
        break_long_words=False,
        break_on_hyphens=False,
    )


# ─────────────────────────────────────────────────────────────────────────────
# Data structures
# ─────────────────────────────────────────────────────────────────────────────


@dataclass
class Finding:
    text: str
    iteration: int


@dataclass
class ReviewerResult:
    reviewer_id: int
    findings: List[Finding]
    exit_code: int
    parse_errors: int
    timed_out: bool
    duration_seconds: float


class FindingGroup(TypedDict):
    title: str
    summary: str
    messages: List[str]
    reviewer_count: int
    sources: List[int]


class GroupedFindings(TypedDict, total=False):
    findings: List[FindingGroup]
    info: List[FindingGroup]


class AggregatedFinding(TypedDict):
    text: str
    reviewers: List[int]


# ─────────────────────────────────────────────────────────────────────────────
# Shared state for async coordination
# ─────────────────────────────────────────────────────────────────────────────


@dataclass
class ReviewState:
    """Shared state for coordinating async tasks."""

    completed: int = 0
    total_reviewers: int = 0
    interrupted: bool = False
    spinner_stop: asyncio.Event = field(default_factory=asyncio.Event)
    verbose: bool = False
    tasks: List[asyncio.Task] = field(default_factory=list)

    def log(self, msg: str, style: str = "info") -> None:
        """Log a styled message, clearing spinner line first if needed."""
        log_styled(msg, style=style)


# ─────────────────────────────────────────────────────────────────────────────
# Core functions
# ─────────────────────────────────────────────────────────────────────────────


def aggregate_findings(findings: List[Finding]) -> List[AggregatedFinding]:
    """Aggregate findings by text, tracking which reviewers found each."""
    seen: dict[str, List[int]] = {}
    for f in findings:
        normalized = f.text.strip()
        if normalized:
            if normalized not in seen:
                seen[normalized] = []
            if f.iteration not in seen[normalized]:
                seen[normalized].append(f.iteration)
    return [
        AggregatedFinding(text=text, reviewers=sorted(reviewers))
        for text, reviewers in seen.items()
    ]


def check_dependencies() -> bool:
    """Check that required external tools are available."""
    if shutil.which("codex") is None:
        log_styled("'codex' not found in PATH", style="error")
        return False
    return True


def check_gh_available() -> bool:
    if shutil.which("gh") is None:
        log_styled("'gh' not found in PATH", style="error")
        return False
    return True


def get_current_pr_number(branch: str | None = None) -> str | None:
    """Return the PR number for the given branch (or current branch), or None if not found."""
    cmd = ["gh", "pr", "view"]
    if branch:
        cmd.append(branch)
    cmd.extend(["--json", "number", "--jq", ".number"])
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None
    pr_number = result.stdout.strip()
    return pr_number or None


def post_pr_comment(pr_number: str, body: str) -> Tuple[bool, str]:
    """Post a comment to a PR. Returns (success, error_message)."""
    result = subprocess.run(
        ["gh", "pr", "comment", pr_number, "--body-file", "-"],
        input=body,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "unknown error"
        return False, stderr
    return True, ""


def approve_pr(pr_number: str, body: str) -> Tuple[bool, str]:
    """Approve a PR with the given body. Returns (success, error_message)."""
    result = subprocess.run(
        ["gh", "pr", "review", pr_number, "--approve", "--body-file", "-"],
        input=body,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "unknown error"
        return False, stderr
    return True, ""


@dataclass
class CIStatus:
    """Result of checking CI status for a PR."""

    all_passed: bool
    pending: List[str]
    failed: List[str]
    error: Optional[str] = None


def check_ci_status(pr_number: str) -> CIStatus:
    """Check CI status for a PR. Returns CIStatus with check details."""
    result = subprocess.run(
        ["gh", "pr", "checks", pr_number, "--json", "name,bucket"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip() or "unknown error"
        return CIStatus(all_passed=False, pending=[], failed=[], error=stderr)

    try:
        checks = json.loads(result.stdout)
    except json.JSONDecodeError:
        return CIStatus(
            all_passed=False, pending=[], failed=[], error="Failed to parse CI status"
        )

    if not checks:
        # No CI checks configured - allow approval
        return CIStatus(all_passed=True, pending=[], failed=[])

    pending: List[str] = []
    failed: List[str] = []

    for check in checks:
        name = check.get("name", "unknown")
        bucket = check.get("bucket", "").lower()

        # bucket values: pass, fail, pending, skipping, cancel
        if bucket == "pending":
            pending.append(name)
        elif bucket not in ("pass", "skipping"):
            # fail, cancel, or unknown
            failed.append(name)

    all_passed = len(pending) == 0 and len(failed) == 0
    return CIStatus(all_passed=all_passed, pending=pending, failed=failed)


@dataclass
class PRAction:
    """Configuration for a PR action (comment or approval)."""

    body: str
    preview_label: str  # e.g., "PR comment preview" or "Approval comment preview"
    prompt_template: str  # e.g., "Post findings to PR #{pr}?" or "Approve PR #{pr}?"
    success_template: str  # e.g., "Posted findings to PR #{pr}."
    skip_message: str  # e.g., "Skipped posting findings."
    execute: "Callable[[str, str], Tuple[bool, str]]"  # fn(pr_number, body) -> (ok, err)


def confirm_and_execute_pr_action(
    action: PRAction,
    state: ReviewState,
    local_mode: bool,
    local_skip_message: str,
    auto_yes: bool = False,
    auto_no: bool = False,
    branch: str | None = None,
) -> Tuple[bool, Optional[str]]:
    """
    Preview, confirm, and execute a PR action.

    Returns (executed, error_message).
    - (True, None) = action executed successfully
    - (False, None) = skipped (local mode, no PR, user declined, or auto_no)
    - (False, "error") = failed with error message
    """
    c = Colors
    if local_mode:
        state.log(local_skip_message, style="dim")
        return False, None

    if auto_no:
        state.log(action.skip_message, style="dim")
        return False, None

    # Preview
    print("")
    log_styled(f"{c.BOLD}{action.preview_label}{c.RESET}", style="phase", clear_line=False)
    print("")
    width = min(get_terminal_width(), MAX_REPORT_WIDTH)
    divider = get_ruler(width, "━")
    print(divider)
    print(action.body)
    print(divider)

    if not check_gh_available():
        return False, "gh not available"

    pr_number = get_current_pr_number(branch)
    if not pr_number:
        branch_desc = f"branch '{branch}'" if branch else "current branch"
        state.log(f"No open PR found for {branch_desc}.", style="warning")
        return False, None

    # Confirm (or auto-confirm with -y)
    if auto_yes:
        confirmed = True
    else:
        print("")
        try:
            prompt = action.prompt_template.format(pr=f"{c.BOLD}#{pr_number}{c.RESET}")
            response = input(f"{c.CYAN}?{c.RESET} {prompt} {c.DIM}[y/N]:{c.RESET} ").strip().lower()
        except EOFError:
            response = ""
        confirmed = response in ("y", "yes")

    if not confirmed:
        state.log(action.skip_message, style="dim")
        return False, None

    # Execute
    success, error = action.execute(pr_number, action.body)
    if not success:
        return False, error

    state.log(action.success_template.format(pr=f"#{pr_number}"), style="success")
    return True, None


# ─────────────────────────────────────────────────────────────────────────────
# Worktree management
# ─────────────────────────────────────────────────────────────────────────────


def get_git_root() -> Optional[str]:
    """Get the root directory of the current git repository."""
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def get_git_common_dir() -> Optional[str]:
    """Get the git common directory (shared across worktrees)."""
    result = subprocess.run(
        ["git", "rev-parse", "--git-common-dir"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        return None
    return os.path.realpath(result.stdout.strip())


def ensure_worktrees_excluded(common_dir: str) -> None:
    """Add .worktrees/ to .git/info/exclude if not already present."""
    info_dir = os.path.join(common_dir, "info")
    exclude_path = os.path.join(info_dir, "exclude")
    os.makedirs(info_dir, exist_ok=True)
    try:
        with open(exclude_path, "r", encoding="utf-8") as handle:
            lines = handle.read().splitlines()
    except OSError:
        lines = []
    if ".worktrees/" not in lines:
        with open(exclude_path, "a", encoding="utf-8") as handle:
            handle.write(".worktrees/\n")


@contextmanager
def temporary_worktree(branch: str) -> Generator[str, None, None]:
    """
    Create a temporary worktree for the given branch and yield the path.

    The worktree is placed in .worktrees/ inside the repo root.
    On cleanup, only the worktree is removed - the branch is left intact.
    """
    # Get repo root from common dir to handle being called from a worktree
    common_dir = get_git_common_dir()
    if not common_dir:
        raise RuntimeError("Not inside a git repository")

    # The repo root is the parent of the git common dir
    repo_root = os.path.realpath(os.path.join(common_dir, ".."))

    # Ensure .worktrees/ is in .git/info/exclude
    ensure_worktrees_excluded(common_dir)

    # Create unique worktree directory name
    worktree_id = uuid.uuid4().hex[:8]
    safe_branch = branch.replace("/", "-")
    worktree_name = f"review-{safe_branch}-{worktree_id}"
    worktrees_dir = os.path.join(repo_root, ".worktrees")
    worktree_path = os.path.join(worktrees_dir, worktree_name)

    os.makedirs(worktrees_dir, exist_ok=True)

    # Create the worktree
    result = subprocess.run(
        ["git", "worktree", "add", worktree_path, branch],
        cwd=repo_root,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = result.stderr.strip()
        raise RuntimeError(f"Failed to create worktree for branch '{branch}': {stderr}")

    try:
        yield worktree_path
    finally:
        # Remove the worktree only - do NOT delete the branch
        subprocess.run(
            ["git", "worktree", "remove", "--force", worktree_path],
            cwd=repo_root,
            capture_output=True,
            text=True,
            check=False,
        )


def collect_source_indices(groups: List[FindingGroup]) -> List[int]:
    seen: set[int] = set()
    indices: List[int] = []
    for group in groups:
        sources = group.get("sources", [])
        if not isinstance(sources, list):
            continue
        for source in sources:
            if isinstance(source, int) and source not in seen:
                seen.add(source)
                indices.append(source)
    return indices


def format_raw_findings(
    aggregated: List[AggregatedFinding],
    source_indices: List[int],
    total_reviewers: int,
    include_header: bool = True,
) -> str:
    if not source_indices:
        return ""

    lines: List[str] = []
    if include_header:
        lines.append("## Raw findings (verbatim)")

    for idx, source in enumerate(source_indices, start=1):
        if source < 0 or source >= len(aggregated):
            continue
        entry = aggregated[source]
        reviewers = entry.get("reviewers", [])
        reviewer_count = len(reviewers) if isinstance(reviewers, list) else 0
        lines.append("")
        lines.append(f"{idx}. ({reviewer_count}/{total_reviewers} reviewers)")
        lines.append("```")
        lines.append(str(entry.get("text", "")).rstrip())
        lines.append("```")

    return "\n".join(lines).rstrip()


def render_lgtm_markdown(
    total_reviewers: int,
    successful_reviewers: int,
    reviewer_comments: dict[int, str] | None = None,
) -> str:
    """Render LGTM approval comment markdown."""
    lines: List[str] = []
    lines.append("## LGTM :white_check_mark:")
    lines.append("")
    lines.append(f"**{successful_reviewers} of {total_reviewers} reviewers found no issues.**")

    if reviewer_comments:
        lines.append("")
        lines.append("<details>")
        lines.append("<summary>Reviewer comments</summary>")
        lines.append("")
        for reviewer_id in sorted(reviewer_comments.keys()):
            comment = reviewer_comments[reviewer_id]
            lines.append(f"- **Reviewer {reviewer_id}:** {comment}")
        lines.append("")
        lines.append("</details>")

    return "\n".join(lines).strip()


def render_comment_markdown(
    grouped: GroupedFindings,
    total_reviewers: int,
    aggregated: List[AggregatedFinding],
) -> str:
    findings = grouped.get("findings")
    if not isinstance(findings, list):
        findings = []

    lines: List[str] = []
    lines.append("## Findings")

    for idx, finding in enumerate(findings, start=1):
        title = str(finding.get("title", "")).strip() or "Untitled"
        summary = str(finding.get("summary", "")).strip()
        messages = finding.get("messages")
        if not isinstance(messages, list):
            messages = []

        reviewer_count = finding.get("reviewer_count", finding.get("worker_count", 0))
        if reviewer_count:
            confidence = f" ({reviewer_count}/{total_reviewers} reviewers)"
        else:
            confidence = ""

        lines.append("")
        lines.append(f"{idx}. **{title}**{confidence}")

        if summary:
            lines.append("")
            lines.append(summary)

        if messages:
            lines.append("")
            lines.append("Evidence:")
            for message in messages:
                message_text = str(message).strip()
                if message_text:
                    lines.append(f"- {message_text}")

    raw_indices = collect_source_indices(findings)
    raw_section = format_raw_findings(
        aggregated, raw_indices, total_reviewers, include_header=False
    )
    if raw_section:
        lines.append("")
        lines.append("_Expand for verbatim findings._")
        lines.append("<details>")
        lines.append("<summary>Raw findings (verbatim)</summary>")
        lines.append("")
        lines.append(raw_section)
        lines.append("</details>")

    return "\n".join(lines).strip()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Run codex review in parallel, parse JSONL output, and summarize findings."
        )
    )
    parser.add_argument(
        "-r",
        "--reviewers",
        dest="reviewers",
        type=int,
        default=DEFAULT_REVIEWERS,
        help=f"Parallel review runs to execute (default: {DEFAULT_REVIEWERS})",
    )
    parser.add_argument(
        "-b",
        "--base",
        default=DEFAULT_BASE_REF,
        help=f"Base ref for review command (default: {DEFAULT_BASE_REF})",
    )
    parser.add_argument(
        "-v",
        "--verbose",
        action="store_true",
        help="Print agent_message entries as they arrive (default: false).",
    )
    parser.add_argument(
        "-t",
        "--timeout",
        type=int,
        default=DEFAULT_TIMEOUT,
        help=f"Timeout in seconds per reviewer (default: {DEFAULT_TIMEOUT})",
    )
    parser.add_argument(
        "-R",
        "--retries",
        type=int,
        default=DEFAULT_RETRIES,
        help=f"Retry failed reviewers N times (default: {DEFAULT_RETRIES})",
    )
    parser.add_argument(
        "-l",
        "--local",
        action="store_true",
        help="Skip posting findings to a PR comment",
    )
    parser.add_argument(
        "-B",
        "--worktree-branch",
        dest="worktree_branch",
        metavar="BRANCH",
        help="Review a branch in a temporary worktree (worktree is cleaned up after review)",
    )

    # Mutually exclusive auto-submit options
    submit_group = parser.add_mutually_exclusive_group()
    submit_group.add_argument(
        "-y",
        "--yes",
        action="store_true",
        help="Automatically submit review without prompting",
    )
    submit_group.add_argument(
        "-n",
        "--no",
        action="store_true",
        help="Automatically skip submitting review without prompting",
    )
    return parser.parse_args()


def build_command(base: str) -> List[str]:
    return ["codex", "exec", "--json", "--color", "never", "review", "--base", base]


async def collect_findings(
    cmd: List[str],
    reviewer_id: int,
    timeout: int,
    state: ReviewState,
    cwd: Optional[str] = None,
) -> ReviewerResult:
    """Collect findings from a single reviewer using async subprocess."""
    start_time = time.monotonic()
    parse_errors = 0
    findings: List[Finding] = []
    timed_out = False

    proc = await asyncio.create_subprocess_exec(
        *cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        start_new_session=True,
        limit=100 * 1024 * 1024,  # 100MB line limit for large JSON output
        cwd=cwd,
    )

    try:
        async def read_output() -> None:
            nonlocal parse_errors
            assert proc.stdout is not None
            async for raw_line in proc.stdout:
                line = raw_line.decode().strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    parse_errors += 1
                    continue

                item = event.get("item")
                if isinstance(item, dict) and item.get("type") == "agent_message":
                    text = item.get("text")
                    if text:
                        findings.append(Finding(text=text, iteration=reviewer_id))
                        if state.verbose:
                            # Truncate long messages for terminal display
                            display = text[:120] + "..." if len(text) > 120 else text
                            c = Colors
                            log_styled(
                                f"{c.DIM}#{reviewer_id}:{c.RESET} {c.DIM}{display}{c.RESET}",
                                style="dim",
                            )

        await asyncio.wait_for(read_output(), timeout=timeout)
        await proc.wait()
        exit_code = proc.returncode or 0

    except asyncio.TimeoutError:
        timed_out = True
        exit_code = -1
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except OSError:
            pass
        await proc.wait()

    except asyncio.CancelledError:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except OSError:
            pass
        await proc.wait()
        raise

    duration = time.monotonic() - start_time

    return ReviewerResult(
        reviewer_id=reviewer_id,
        findings=findings,
        exit_code=exit_code,
        parse_errors=parse_errors,
        timed_out=timed_out,
        duration_seconds=duration,
    )


async def collect_findings_with_retry(
    cmd: List[str],
    reviewer_id: int,
    timeout: int,
    retries: int,
    state: ReviewState,
    cwd: Optional[str] = None,
) -> ReviewerResult:
    """Collect findings with retry on failure or timeout."""
    result: ReviewerResult | None = None

    for attempt in range(retries + 1):
        if state.interrupted:
            break

        result = await collect_findings(cmd, reviewer_id, timeout, state, cwd=cwd)

        if result.exit_code == 0:
            return result

        if attempt < retries:
            delay = 2**attempt
            reason = "timed out" if result.timed_out else f"exit {result.exit_code}"
            state.log(f"Reviewer #{reviewer_id} {reason}, retry {attempt + 1}/{retries} in {delay}s", style="warning")

            try:
                await asyncio.sleep(delay)
            except asyncio.CancelledError:
                break

    assert result is not None
    return result


async def run_spinner(state: ReviewState) -> None:
    """Async spinner task."""
    if not sys.stderr.isatty():
        return

    c = Colors
    idx = 0

    while not state.spinner_stop.is_set():
        frame = SPINNER_FRAMES[idx % len(SPINNER_FRAMES)]
        progress = f"{state.completed}/{state.total_reviewers}"
        tag = f"{c.DIM}[{c.RESET}{c.CYAN}review{c.RESET}{c.DIM}]{c.RESET}"
        line = f"\r{tag} {c.CYAN}{frame}{c.RESET} Running reviewers {c.DIM}({progress}){c.RESET}"
        sys.stderr.write(line + " " * 10)
        sys.stderr.flush()
        idx += 1

        try:
            await asyncio.wait_for(
                state.spinner_stop.wait(),
                timeout=SPINNER_INTERVAL,
            )
            break
        except asyncio.TimeoutError:
            pass

    # Final state
    progress = f"{state.completed}/{state.total_reviewers}"
    tag = f"{c.DIM}[{c.RESET}{c.GREEN}review{c.RESET}{c.DIM}]{c.RESET}"
    final = f"\r{tag} {c.GREEN}✓{c.RESET} Reviewers complete {c.DIM}({progress}){c.RESET}"
    sys.stderr.write(final + " " * 10 + "\n")
    sys.stderr.flush()


async def run_phase_spinner(label: str, stop: asyncio.Event) -> None:
    """Show a spinner for a single phase with a custom label."""
    if not sys.stderr.isatty():
        return

    c = Colors
    idx = 0

    while not stop.is_set():
        frame = SPINNER_FRAMES[idx % len(SPINNER_FRAMES)]
        tag = f"{c.DIM}[{c.RESET}{c.CYAN}review{c.RESET}{c.DIM}]{c.RESET}"
        line = f"\r{tag} {c.CYAN}{frame}{c.RESET} {label}"
        sys.stderr.write(line + " " * 10)
        sys.stderr.flush()
        idx += 1

        try:
            await asyncio.wait_for(stop.wait(), timeout=SPINNER_INTERVAL)
            break
        except asyncio.TimeoutError:
            pass

    tag = f"{c.DIM}[{c.RESET}{c.GREEN}review{c.RESET}{c.DIM}]{c.RESET}"
    final = f"\r{tag} {c.GREEN}✓{c.RESET} {label}"
    sys.stderr.write(final + " " * 10 + "\n")
    sys.stderr.flush()


GROUP_PROMPT = """# Codex Review Summarizer

You are grouping results from repeated Codex review runs.

Input: a JSON array of objects, each with "id" (input identifier), "text" (the finding),
and "reviewers" (list of reviewer IDs that found it).

Task:
- Cluster messages that describe the same underlying issue.
- Create a short, precise title per group.
- Keep groups distinct; do not merge different issues.
- If something is unique, keep it as its own group.
- Sum up unique reviewer IDs across clustered messages for reviewer_count.
- Track which input ids are represented in each group via "sources".

Output format (JSON only, no extra prose):
{
  "findings": [
    {
      "title": "Short issue title",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [0, 2]
    }
  ],
  "info": [
    {
      "title": "Informational note",
      "summary": "1-2 sentence summary.",
      "messages": ["short excerpt 1", "short excerpt 2"],
      "reviewer_count": 3,
      "sources": [1]
    }
  ]
}

Rules:
- Return ONLY valid JSON.
- Keep excerpts under ~200 characters each.
- Preserve file paths, line numbers, flags, branch names, and commands in excerpts when present.
- If a message includes a file path with line numbers, keep that exact location text in the excerpt.
- "sources" must include all input ids represented in each group.
- reviewer_count = number of unique reviewers that reported any message in this cluster.
- Put non-actionable outcomes (e.g., "no diffs", "no changes to review") in "info".
- If the input is empty, return: {"findings": [], "info": []}
"""


async def summarize_findings(
    aggregated: List[AggregatedFinding],
) -> Tuple[GroupedFindings, int, str, str, float]:
    """Summarize findings using LLM (async version)."""
    if not aggregated:
        return GroupedFindings(findings=[]), 0, "", "", 0.0

    start_time = time.monotonic()
    prompt = GROUP_PROMPT.rstrip()
    payload = json.dumps(
        [{"id": idx, "text": item["text"], "reviewers": item["reviewers"]}
         for idx, item in enumerate(aggregated)],
        ensure_ascii=True,
    )
    full_prompt = f"{prompt}\n\nINPUT JSON:\n{payload}\n"

    proc = await asyncio.create_subprocess_exec(
        "codex", "exec", "--color", "never", "-",
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )

    stdout_bytes, stderr_bytes = await proc.communicate(input=full_prompt.encode())
    duration = time.monotonic() - start_time

    output = stdout_bytes.decode().strip()
    stderr = stderr_bytes.decode().strip()

    if not output:
        return GroupedFindings(findings=[]), proc.returncode or 0, stderr, output, duration

    try:
        data: GroupedFindings = json.loads(output)
    except json.JSONDecodeError:
        return (
            GroupedFindings(findings=[]),
            1,
            "Failed to parse summarizer JSON output.",
            output,
            duration,
        )

    return data, proc.returncode or 0, stderr, output, duration


def render_report(
    grouped: GroupedFindings,
    summarize_exit_code: int,
    summarize_stderr: str,
    summarize_raw: str,
    parse_errors: int,
    failed_iters: List[int],
    timed_out_iters: List[int] | None = None,
    wall_clock_duration: float | None = None,
    reviewer_durations: dict[int, float] | None = None,
    summarizer_duration: float | None = None,
    total_reviewers: int | None = None,
    include_info: bool = True,
    include_warnings: bool = True,
    include_timing: bool = True,
) -> str:
    c = Colors
    width = min(get_terminal_width(), MAX_REPORT_WIDTH)

    findings = grouped.get("findings")
    if not isinstance(findings, list):
        findings = []
    info: List[FindingGroup] = []

    lines: List[str] = []

    # Handle summarizer errors
    if summarize_exit_code != 0:
        lines.append("")
        lines.append(f"{c.RED}✗ Summarizer Error{c.RESET}")
        lines.append(get_ruler(width))
        lines.append(f"  Exit code: {summarize_exit_code}")
        if summarize_stderr:
            lines.append(f"  Stderr: {summarize_stderr}")
        if summarize_raw:
            lines.append(f"\n  {c.DIM}Raw output:{c.RESET}")
            for line in summarize_raw.splitlines()[:MAX_RAW_OUTPUT_LINES]:
                lines.append(f"  {c.DIM}{line}{c.RESET}")
        return "\n".join(lines)

    # Warnings
    warnings: List[str] = []
    if parse_errors:
        warnings.append(f"JSONL parse errors: {parse_errors}")
    if failed_iters:
        joined = ", ".join(str(i) for i in failed_iters)
        warnings.append(f"Failed reviewers: {joined}")
    if timed_out_iters:
        joined = ", ".join(str(i) for i in timed_out_iters)
        warnings.append(f"Timed out reviewers: {joined}")

    if include_warnings and warnings:
        lines.append("")
        lines.append(f"{c.YELLOW}⚠ Warnings{c.RESET}")
        lines.append(get_ruler(width))
        for warning in warnings:
            lines.append(f"  {c.YELLOW}•{c.RESET} {warning}")
        lines.append("")

    # No findings/info case
    if not findings:
        lines.append("")
        lines.append(f"{c.GREEN}{c.BOLD}LGTM{c.RESET}")
        lines.append("")
        return "\n".join(lines)

    # Findings header
    lines.append("")
    finding_word = "finding" if len(findings) == 1 else "findings"
    lines.append(f"{c.CYAN}{c.BOLD}📋 {len(findings)} {finding_word}{c.RESET}")
    lines.append(get_ruler(width, "━"))

    # Render each finding
    for idx, finding in enumerate(findings, start=1):
        title = str(finding.get("title", "")).strip() or "Untitled"
        summary = str(finding.get("summary", "")).strip()
        messages = finding.get("messages")
        if not isinstance(messages, list):
            messages = []

        lines.append("")
        reviewer_count = finding.get("reviewer_count", finding.get("worker_count", 0))
        if total_reviewers and reviewer_count:
            confidence = f" {c.DIM}({reviewer_count}/{total_reviewers} reviewers){c.RESET}"
        else:
            confidence = ""
        lines.append(f"{c.YELLOW}{c.BOLD}{idx}.{c.RESET} {c.BOLD}{title}{c.RESET}{confidence}")
        lines.append(get_ruler(width))

        if summary:
            wrapped = wrap_text(
                summary, width - 3, initial_indent="   ", subsequent_indent="   "
            )
            lines.append(wrapped)

        if messages:
            lines.append("")
            lines.append(f"   {c.DIM}Evidence:{c.RESET}")
            for message in messages:
                message_text = str(message).strip()
                if message_text:
                    wrapped = wrap_text(
                        message_text,
                        width - 5,
                        initial_indent=f"   {c.DIM}•{c.RESET} ",
                        subsequent_indent="     ",
                    )
                    lines.append(wrapped)

    lines.append("")
    lines.append(get_ruler(width, "━"))

    # Timing stats
    has_timing = (
        wall_clock_duration is not None
        or reviewer_durations
        or summarizer_duration is not None
    )
    if include_timing and has_timing:
        lines.append("")
        lines.append(f"{c.DIM}Timing:{c.RESET}")

        if wall_clock_duration is not None:
            lines.append(
                f"  {c.DIM}reviewers: {format_duration(wall_clock_duration)}{c.RESET}"
            )

        if reviewer_durations:
            durations = list(reviewer_durations.values())
            avg = sum(durations) / len(durations)
            lines.append(
                f"  {c.DIM}  min {format_duration(min(durations))} / "
                f"avg {format_duration(avg)} / "
                f"max {format_duration(max(durations))}{c.RESET}"
            )

        if summarizer_duration is not None:
            lines.append(
                f"  {c.DIM}summarizer: {format_duration(summarizer_duration)}{c.RESET}"
            )

        if wall_clock_duration is not None and summarizer_duration is not None:
            total = wall_clock_duration + summarizer_duration
            lines.append(
                f"  {c.DIM}total: {format_duration(total)}{c.RESET}"
            )

    return "\n".join(lines)


async def async_main(args: argparse.Namespace) -> int:
    """Async entry point."""
    # Handle worktree-based review
    if args.worktree_branch:
        return await run_review_in_worktree(args)
    return await run_review(args, cwd=None)


async def run_review_in_worktree(args: argparse.Namespace) -> int:
    """Run review in a temporary worktree for the specified branch."""
    branch = args.worktree_branch
    c = Colors
    log_styled(f"Creating worktree for {c.BOLD}{branch}{c.RESET}", style="info")

    try:
        with temporary_worktree(branch) as worktree_path:
            log_styled(f"Worktree ready {c.DIM}({worktree_path}){c.RESET}", style="success")
            result = await run_review(args, cwd=worktree_path)
            log_styled("Cleaning up worktree", style="dim")
            return result
    except RuntimeError as e:
        log_styled(f"Error: {e}", style="error")
        return EXIT_ERROR


async def run_review(args: argparse.Namespace, cwd: Optional[str] = None) -> int:
    """Run the review process, optionally in a specific directory."""
    state = ReviewState(
        total_reviewers=args.reviewers,
        verbose=args.verbose,
    )

    # Set up signal handlers
    loop = asyncio.get_running_loop()
    c = Colors

    def handle_interrupt() -> None:
        if state.interrupted:
            return  # Already handling
        state.interrupted = True
        state.spinner_stop.set()
        sys.stderr.write("\n")
        log_styled("Interrupted, shutting down...", style="warning")
        # Cancel all running tasks
        for task in state.tasks:
            task.cancel()

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, handle_interrupt)

    cmd = build_command(args.base)
    cmd_str = " ".join(shlex.quote(part) for part in cmd)

    state.log(f"Starting review {c.DIM}({args.reviewers} reviewers, base={args.base}){c.RESET}")
    if state.verbose:
        state.log(f"{c.DIM}Command: {cmd_str}{c.RESET}", style="dim")

    # Start spinner
    spinner_task = asyncio.create_task(run_spinner(state))

    # Track wall-clock time
    reviewers_start = time.monotonic()

    # Run reviewers concurrently
    async def run_reviewer(reviewer_id: int) -> ReviewerResult:
        result = await collect_findings_with_retry(
            cmd, reviewer_id, args.timeout, args.retries, state, cwd=cwd
        )
        state.completed += 1
        return result

    # Create tasks and store references for cancellation
    state.tasks = [
        asyncio.create_task(run_reviewer(i)) for i in range(1, args.reviewers + 1)
    ]

    try:
        results = await asyncio.gather(*state.tasks, return_exceptions=True)
    except asyncio.CancelledError:
        state.spinner_stop.set()
        await spinner_task
        return EXIT_INTERRUPTED

    reviewers_duration = time.monotonic() - reviewers_start

    state.spinner_stop.set()
    await spinner_task

    if state.interrupted:
        return EXIT_INTERRUPTED

    # Process results
    all_findings: List[Finding] = []
    parse_errors = 0
    failed_iters: List[int] = []
    timed_out_iters: List[int] = []
    reviewer_durations: dict[int, float] = {}
    exception_count = 0

    for result in results:
        if isinstance(result, Exception):
            state.log(f"Reviewer exception: {result}", style="error")
            exception_count += 1
            continue

        parse_errors += result.parse_errors
        reviewer_durations[result.reviewer_id] = result.duration_seconds

        if result.timed_out:
            timed_out_iters.append(result.reviewer_id)
        elif result.exit_code != 0:
            failed_iters.append(result.reviewer_id)

        all_findings.extend(result.findings)

    # Check if all reviewers failed
    total_failures = len(failed_iters) + len(timed_out_iters) + exception_count
    if total_failures >= args.reviewers:
        state.log("All reviewers failed", style="error")
        return EXIT_ERROR

    # Summarize
    aggregated = aggregate_findings(all_findings)
    summarizer_stop = asyncio.Event()
    summarizer_spinner = asyncio.create_task(
        run_phase_spinner("Summarizing", summarizer_stop)
    )
    (
        grouped,
        summarize_exit_code,
        summarize_stderr,
        summarize_raw,
        summarizer_duration,
    ) = await summarize_findings(aggregated)
    summarizer_stop.set()
    await summarizer_spinner

    # Output
    output = render_report(
        grouped=grouped,
        summarize_exit_code=summarize_exit_code,
        summarize_stderr=summarize_stderr,
        summarize_raw=summarize_raw,
        parse_errors=parse_errors,
        failed_iters=failed_iters,
        timed_out_iters=timed_out_iters,
        wall_clock_duration=reviewers_duration,
        reviewer_durations=reviewer_durations,
        summarizer_duration=summarizer_duration,
        total_reviewers=args.reviewers,
    )
    print(output)

    if summarize_exit_code != 0:
        return EXIT_ERROR
    findings = grouped.get("findings", [])

    # Calculate successful reviewers (exclude failed and timed out)
    successful_reviewers = args.reviewers - len(failed_iters) - len(timed_out_iters)

    if not findings:
        # LGTM flow - approve the PR
        # Build reviewer comments from findings (each reviewer's last message)
        reviewer_comments: dict[int, str] = {}
        for finding in all_findings:
            reviewer_comments[finding.iteration] = finding.text
        lgtm_body = render_lgtm_markdown(
            total_reviewers=args.reviewers,
            successful_reviewers=successful_reviewers,
            reviewer_comments=reviewer_comments,
        )

        # Check CI status before approving (skip in local mode or auto-no mode)
        if not args.local and not args.no:
            if not check_gh_available():
                return EXIT_ERROR

            pr_number = get_current_pr_number(args.worktree_branch)
            if pr_number:
                ci_status = check_ci_status(pr_number)

                if ci_status.error:
                    state.log(f"Failed to check CI status: {ci_status.error}", style="error")
                    return EXIT_ERROR

                if not ci_status.all_passed:
                    # CI has issues - report LGTM but don't approve
                    state.log(
                        f"{c.GREEN}{c.BOLD}LGTM{c.RESET} - No issues found by reviewers.",
                        style="success",
                    )
                    print("")

                    if ci_status.failed:
                        state.log(
                            f"Cannot approve PR: {len(ci_status.failed)} CI check(s) failed",
                            style="error",
                        )
                        for check in ci_status.failed[:5]:
                            state.log(f"  • {check}", style="dim")
                        if len(ci_status.failed) > 5:
                            state.log(
                                f"  ... and {len(ci_status.failed) - 5} more",
                                style="dim",
                            )
                    if ci_status.pending:
                        state.log(
                            f"Cannot approve PR: {len(ci_status.pending)} CI check(s) pending",
                            style="warning",
                        )
                        for check in ci_status.pending[:5]:
                            state.log(f"  • {check}", style="dim")
                        if len(ci_status.pending) > 5:
                            state.log(
                                f"  ... and {len(ci_status.pending) - 5} more",
                                style="dim",
                            )

                    return EXIT_NO_FINDINGS

        action = PRAction(
            body=lgtm_body,
            preview_label="Approval comment preview",
            prompt_template="Approve PR #{pr}?",
            success_template="Approved PR #{pr}.",
            skip_message="Skipped approving PR.",
            execute=approve_pr,
        )

        executed, error = confirm_and_execute_pr_action(
            action=action,
            state=state,
            local_mode=args.local,
            local_skip_message="Local mode enabled; skipping PR approval.",
            auto_yes=args.yes,
            auto_no=args.no,
            branch=args.worktree_branch,
        )

        if error:
            state.log(f"Failed to approve PR: {error}", style="error")
            return EXIT_ERROR

        return EXIT_NO_FINDINGS

    # Findings flow - post comment to PR
    comment_body = render_comment_markdown(
        grouped=grouped,
        total_reviewers=args.reviewers,
        aggregated=aggregated,
    )

    action = PRAction(
        body=comment_body,
        preview_label="PR comment preview",
        prompt_template="Post findings to PR #{pr}?",
        success_template="Posted findings to PR #{pr}.",
        skip_message="Skipped posting findings.",
        execute=post_pr_comment,
    )

    executed, error = confirm_and_execute_pr_action(
        action=action,
        state=state,
        local_mode=args.local,
        local_skip_message="Local mode enabled; skipping PR comment.",
        auto_yes=args.yes,
        auto_no=args.no,
        branch=args.worktree_branch,
    )

    if error:
        state.log(f"Failed to post comment: {error}", style="error")
        return EXIT_ERROR

    return EXIT_FINDINGS


def main() -> int:
    args = parse_args()

    if not sys.stdout.isatty():
        Colors.disable()

    if not check_dependencies():
        return EXIT_ERROR

    if args.reviewers < 1:
        log_styled("--reviewers must be >= 1", style="error")
        return EXIT_ERROR

    return asyncio.run(async_main(args))


if __name__ == "__main__":
    raise SystemExit(main())
