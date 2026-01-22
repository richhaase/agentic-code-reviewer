package agent

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// MaxDiffSize is the maximum size of a diff in bytes before truncation.
// Claude's context window is ~200K tokens (~800KB), but we leave room for
// the prompt template and response. Default: 400KB.
const MaxDiffSize = 400 * 1024

// TruncationNotice is appended when a diff is truncated.
const TruncationNotice = "\n\n[DIFF TRUNCATED: Review was truncated to fit within context limits. Focus on the changes shown above.]"

// GetGitDiff returns the git diff against the specified base reference.
// If workDir is empty, uses the current directory.
// The context is used to support cancellation/timeout.
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

// TruncateDiff truncates a diff to maxSize bytes, attempting to truncate
// at file boundaries to preserve complete file diffs where possible.
// Returns the truncated diff and a boolean indicating if truncation occurred.
func TruncateDiff(diff string, maxSize int) (string, bool) {
	if len(diff) <= maxSize {
		return diff, false
	}

	// Try to truncate at a file boundary (diff --git line)
	// Look backwards from maxSize to find a good break point
	searchStart := maxSize
	if searchStart > len(diff) {
		searchStart = len(diff)
	}

	// Search for "diff --git" backwards from the max size
	// This ensures we keep complete file diffs
	truncated := diff[:searchStart]
	lastFileStart := strings.LastIndex(truncated, "\ndiff --git ")

	if lastFileStart > maxSize/2 {
		// Found a file boundary in the latter half - truncate there
		truncated = diff[:lastFileStart]
	} else {
		// No good file boundary found, try to truncate at a newline
		lastNewline := strings.LastIndex(truncated, "\n")
		if lastNewline > maxSize/2 {
			truncated = diff[:lastNewline]
		}
		// Otherwise just use the hard cutoff
	}

	return truncated + TruncationNotice, true
}

// BuildPromptWithDiff combines a review prompt with the git diff.
// Large diffs are automatically truncated to fit within context limits.
func BuildPromptWithDiff(prompt, diff string) string {
	if diff == "" {
		return prompt + "\n\n(No changes detected)"
	}

	// Truncate diff if it exceeds the maximum size
	truncatedDiff, _ := TruncateDiff(diff, MaxDiffSize)

	return prompt + "\n\n```diff\n" + truncatedDiff + "\n```"
}

// FileDiff represents a single file's diff extracted from a unified diff.
type FileDiff struct {
	Filename string // The filename (e.g., "internal/agent/diff.go")
	Content  string // The full diff content for this file including the header
	Size     int    // Size of the content in bytes
}

// diffHeaderRegex matches the "diff --git a/path b/path" header line.
var diffHeaderRegex = regexp.MustCompile(`^diff --git a/(.+?) b/(.+)$`)

// ParseDiffIntoFiles splits a unified diff into per-file chunks.
// Each returned FileDiff contains the complete diff for a single file,
// including its "diff --git" header line.
func ParseDiffIntoFiles(diff string) []FileDiff {
	if diff == "" {
		return nil
	}

	var files []FileDiff
	lines := strings.Split(diff, "\n")

	var currentFile *FileDiff
	var currentLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			// Save the previous file if we have one
			if currentFile != nil {
				content := strings.Join(currentLines, "\n")
				currentFile.Content = content
				currentFile.Size = len(content)
				files = append(files, *currentFile)
			}

			// Start a new file
			currentFile = &FileDiff{}
			currentLines = []string{line}

			// Extract filename from "diff --git a/path b/path"
			if matches := diffHeaderRegex.FindStringSubmatch(line); len(matches) >= 3 {
				currentFile.Filename = matches[2] // Use the "b/" path (destination)
			}
		} else if currentFile != nil {
			currentLines = append(currentLines, line)
		}
	}

	// Don't forget the last file
	if currentFile != nil {
		content := strings.Join(currentLines, "\n")
		currentFile.Content = content
		currentFile.Size = len(content)
		files = append(files, *currentFile)
	}

	return files
}

// DistributeFiles distributes files across N reviewers using round-robin assignment.
// Returns a slice of N diff strings, one per reviewer. Each diff string contains
// the concatenated diffs of all files assigned to that reviewer.
//
// If a single file exceeds MaxDiffSize, it will be truncated to fit.
// If numReviewers is 0 or negative, returns nil.
// If files is empty, returns a slice of empty strings.
func DistributeFiles(files []FileDiff, numReviewers int) []string {
	if numReviewers <= 0 {
		return nil
	}

	// Initialize chunks for each reviewer
	chunks := make([][]string, numReviewers)
	for i := range chunks {
		chunks[i] = make([]string, 0)
	}

	// Round-robin distribute files to reviewers
	for i, file := range files {
		reviewerIdx := i % numReviewers
		content := file.Content

		// Truncate individual files that are too large
		if len(content) > MaxDiffSize {
			content, _ = TruncateDiff(content, MaxDiffSize)
		}

		chunks[reviewerIdx] = append(chunks[reviewerIdx], content)
	}

	// Join each reviewer's files into a single diff string
	result := make([]string, numReviewers)
	for i, chunk := range chunks {
		result[i] = strings.Join(chunk, "\n")
	}

	return result
}
