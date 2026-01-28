package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// FetchResult contains the result of a FetchRemoteRef operation.
type FetchResult struct {
	// ResolvedRef is the ref to use for diffing (either "origin/<baseRef>" or "<baseRef>")
	ResolvedRef string
	// RefResolved indicates whether the ref was successfully resolved (true if fetch succeeded or was skipped)
	RefResolved bool
	// FetchAttempted indicates whether a fetch was attempted (false if baseRef already has origin/ prefix or is a non-branch ref)
	FetchAttempted bool
}

// FetchRemoteRef fetches the base ref from origin and returns the resolved ref to use.
// If fetch succeeds, returns "origin/<baseRef>". If fetch fails, returns "<baseRef>".
// This function should be called once before launching parallel reviewers to ensure
// all reviewers use the same ref for comparison.
//
// For non-branch refs (relative refs like HEAD~3, commit SHAs, or refs starting with -),
// the function skips fetching and returns the ref as-is since these cannot be fetched
// from a remote or would be invalid with the origin/ prefix.
func FetchRemoteRef(ctx context.Context, baseRef, workDir string) FetchResult {
	// If already has origin/ prefix, no fetch needed
	if strings.HasPrefix(baseRef, "origin/") {
		return FetchResult{
			ResolvedRef:    baseRef,
			RefResolved:    true,
			FetchAttempted: false,
		}
	}

	// Skip fetch for refs that can't be fetched or shouldn't have origin/ prefix:
	// - Refs starting with - (potential flag injection, also not valid branch names)
	// - Relative refs containing ~ or ^ (e.g., HEAD~3, main^2)
	// - HEAD (special ref that doesn't have a remote tracking branch)
	// - Commit SHAs (40-char hex strings can't be fetched by ref name)
	// - Fully qualified refs (refs/heads/..., refs/tags/..., refs/remotes/...)
	if strings.HasPrefix(baseRef, "-") ||
		strings.Contains(baseRef, "~") ||
		strings.Contains(baseRef, "^") ||
		baseRef == "HEAD" ||
		isLikelyCommitSHA(baseRef) ||
		strings.HasPrefix(baseRef, "refs/") {
		return FetchResult{
			ResolvedRef:    baseRef,
			RefResolved:    true,
			FetchAttempted: false,
		}
	}

	// Try to fetch the latest base ref from origin
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", baseRef)
	if workDir != "" {
		fetchCmd.Dir = workDir
	}

	if err := fetchCmd.Run(); err == nil {
		// Fetch succeeded - check if this is a tag before prefixing with origin/
		// Tags are fetched into refs/tags/, not refs/remotes/origin/, so they
		// should not be prefixed with origin/
		if isTag(ctx, baseRef, workDir) {
			return FetchResult{
				ResolvedRef:    baseRef,
				RefResolved:    true,
				FetchAttempted: true,
			}
		}
		// It's a branch, use the remote ref
		return FetchResult{
			ResolvedRef:    "origin/" + baseRef,
			RefResolved:    true,
			FetchAttempted: true,
		}
	}

	// Fetch failed, fall back to local ref
	return FetchResult{
		ResolvedRef:    baseRef,
		RefResolved:    false,
		FetchAttempted: true,
	}
}

// isLikelyCommitSHA returns true if the ref looks like a git commit SHA.
// We check for hex strings of 7-40 characters (short and full SHAs).
func isLikelyCommitSHA(ref string) bool {
	if len(ref) < 7 || len(ref) > 40 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// isTag checks if the given ref is a tag in the repository.
// Tags are stored in refs/tags/ and should not be prefixed with origin/.
func isTag(ctx context.Context, ref, workDir string) bool {
	// Validate ref to prevent command injection
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	// #nosec G204 - ref is validated above and used with exec.CommandContext (no shell interpretation)
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/tags/"+ref)
	if workDir != "" {
		cmd.Dir = workDir
	}
	return cmd.Run() == nil
}

// GetGitDiff returns the git diff against the specified base reference.
// If workDir is empty, uses the current directory.
// The context is used to support cancellation/timeout.
//
// Note: For remote refs, call FetchRemoteRef once upfront before launching
// parallel reviewers to ensure all reviewers use the same ref for comparison.
func GetGitDiff(ctx context.Context, baseRef, workDir string) (string, error) {
	// Validate baseRef
	if baseRef == "" {
		return "", fmt.Errorf("base ref cannot be empty")
	}
	// Prevent flag injection (refs starting with - would be interpreted as git flags).
	// The -- must come AFTER baseRef so git treats baseRef as a revision, not a pathspec.
	if strings.HasPrefix(baseRef, "-") {
		return "", fmt.Errorf("invalid base ref %q: must not start with -", baseRef)
	}

	args := []string{"diff", baseRef, "--"}
	cmd := exec.CommandContext(ctx, "git", args...)

	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git diff: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// BuildPromptWithDiff combines a review prompt with the git diff.
func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}
	return prompt + "\n\n```diff\n" + diff + "\n```"
}
