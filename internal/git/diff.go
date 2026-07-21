package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type UpdateBranchResult struct {
	BranchName     string
	Updated        bool
	AlreadyCurrent bool
	Skipped        bool
	SkipReason     string
	Error          error
}

func UpdateCurrentBranch(ctx context.Context, workDir string) UpdateBranchResult {

	branchCmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "HEAD")
	if workDir != "" {
		branchCmd.Dir = workDir
	}
	branchOutput, err := branchCmd.Output()
	if err != nil {
		return UpdateBranchResult{
			Skipped:    true,
			SkipReason: "detached HEAD",
		}
	}
	branch := strings.TrimSpace(string(branchOutput))
	if branch == "" || strings.HasPrefix(branch, "-") {
		return UpdateBranchResult{
			Skipped:    true,
			SkipReason: "detached HEAD",
		}
	}

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	if workDir != "" {
		fetchCmd.Dir = workDir
	}
	if err := fetchCmd.Run(); err != nil {
		return UpdateBranchResult{
			BranchName: branch,
			Error:      fmt.Errorf("fetch failed: %w", err),
		}
	}

	mergeCmd := exec.CommandContext(ctx, "git", "merge", "--ff-only", "origin/"+branch)
	if workDir != "" {
		mergeCmd.Dir = workDir
	}
	mergeOutput, err := mergeCmd.CombinedOutput()
	if err != nil {
		return UpdateBranchResult{
			BranchName: branch,
			Error:      fmt.Errorf("fast-forward failed: %w", err),
		}
	}

	output := strings.TrimSpace(string(mergeOutput))
	if strings.Contains(output, "Already up to date") {
		return UpdateBranchResult{
			BranchName:     branch,
			AlreadyCurrent: true,
		}
	}

	return UpdateBranchResult{
		BranchName: branch,
		Updated:    true,
	}
}

func IsRelativeRef(ref string) bool {
	return ref == "HEAD" ||
		strings.Contains(ref, "~") ||
		strings.Contains(ref, "^") ||
		strings.Contains(ref, "@{") ||
		IsLikelyCommitSHA(ref)
}

type FetchResult struct {
	ResolvedRef string

	RefResolved bool

	FetchAttempted bool
}

func FetchRemoteRef(ctx context.Context, baseRef, workDir string) FetchResult {

	if strings.HasPrefix(baseRef, "origin/") {
		return FetchResult{
			ResolvedRef:    baseRef,
			RefResolved:    true,
			FetchAttempted: false,
		}
	}

	if strings.HasPrefix(baseRef, "-") ||
		strings.Contains(baseRef, "~") ||
		strings.Contains(baseRef, "^") ||
		baseRef == "HEAD" ||
		IsLikelyCommitSHA(baseRef) ||
		strings.HasPrefix(baseRef, "refs/") {
		return FetchResult{
			ResolvedRef:    baseRef,
			RefResolved:    true,
			FetchAttempted: false,
		}
	}

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", baseRef)
	if workDir != "" {
		fetchCmd.Dir = workDir
	}

	if err := fetchCmd.Run(); err == nil {

		if IsTag(ctx, baseRef, workDir) {
			return FetchResult{
				ResolvedRef:    baseRef,
				RefResolved:    true,
				FetchAttempted: true,
			}
		}

		return FetchResult{
			ResolvedRef:    "origin/" + baseRef,
			RefResolved:    true,
			FetchAttempted: true,
		}
	}

	return FetchResult{
		ResolvedRef:    baseRef,
		RefResolved:    false,
		FetchAttempted: true,
	}
}

func IsLikelyCommitSHA(ref string) bool {
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

func IsTag(ctx context.Context, ref, workDir string) bool {

	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}

	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/tags/"+ref)
	if workDir != "" {
		cmd.Dir = workDir
	}
	return cmd.Run() == nil
}

func GetDiff(ctx context.Context, baseRef, workDir string) (string, error) {

	if baseRef == "" {
		return "", fmt.Errorf("base ref cannot be empty")
	}

	if strings.HasPrefix(baseRef, "-") {
		return "", fmt.Errorf("invalid base ref %q: must not start with -", baseRef)
	}

	args := []string{"diff", baseRef, "--"}
	cmd := exec.CommandContext(ctx, "git", args...)

	cmd.Env = filterEnv(os.Environ(), "GIT_EXTERNAL_DIFF")

	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git diff: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func filterEnv(env []string, name string) []string {
	prefix := name + "="
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			result = append(result, e)
		}
	}
	return result
}
