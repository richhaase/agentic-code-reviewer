package agent

import (
	"strings"
	"testing"
)

func TestTruncateDiff_NoTruncationNeeded(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\n+some changes"
	result, truncated := TruncateDiff(diff, 1000)

	if truncated {
		t.Error("expected no truncation for small diff")
	}
	if result != diff {
		t.Errorf("expected diff to be unchanged, got %q", result)
	}
}

func TestTruncateDiff_TruncatesAtFileHeader(t *testing.T) {
	// Create a diff with two files where the second file pushes us over the limit
	file1 := "diff --git a/file1.go b/file1.go\n+line 1\n+line 2\n"
	file2 := "diff --git a/file2.go b/file2.go\n+line 3\n+line 4\n"
	diff := file1 + file2

	// Set max size to just over file1's size but less than the full diff
	maxSize := len(file1) + 10

	result, truncated := TruncateDiff(diff, maxSize)

	if !truncated {
		t.Error("expected truncation")
	}
	if !strings.HasSuffix(result, TruncationNotice) {
		t.Error("expected truncation notice at end")
	}
	// Should truncate at the file boundary (exclude file2)
	if strings.Contains(result, "file2.go") {
		t.Error("expected second file to be truncated")
	}
	if !strings.Contains(result, "file1.go") {
		t.Error("expected first file to be preserved")
	}
}

func TestTruncateDiff_TruncatesAtNewlineWhenNoFileHeader(t *testing.T) {
	// Create a diff without clear file boundaries near the truncation point
	lines := []string{}
	for i := 0; i < 100; i++ {
		lines = append(lines, "+this is a line of code")
	}
	diff := strings.Join(lines, "\n")

	maxSize := 500
	result, truncated := TruncateDiff(diff, maxSize)

	if !truncated {
		t.Error("expected truncation")
	}
	if !strings.HasSuffix(result, TruncationNotice) {
		t.Error("expected truncation notice at end")
	}
	// Result (minus notice) should be less than maxSize
	resultWithoutNotice := strings.TrimSuffix(result, TruncationNotice)
	if len(resultWithoutNotice) > maxSize {
		t.Errorf("truncated result too long: %d > %d", len(resultWithoutNotice), maxSize)
	}
}

func TestTruncateDiff_ExactMaxSize(t *testing.T) {
	diff := "exactly this size"
	result, truncated := TruncateDiff(diff, len(diff))

	if truncated {
		t.Error("expected no truncation when diff equals max size")
	}
	if result != diff {
		t.Error("expected diff to be unchanged")
	}
}

func TestBuildPromptWithDiff_TruncatesLargeDiff(t *testing.T) {
	// Create a diff larger than MaxDiffSize by repeating content
	// Each iteration is about 40 bytes, so we need MaxDiffSize/40 + 1 iterations
	line := "diff --git a/file.go b/file.go\n+x\n"
	iterations := (MaxDiffSize / len(line)) + 100 // Ensure it's definitely larger
	largeDiff := strings.Repeat(line, iterations)

	if len(largeDiff) <= MaxDiffSize {
		t.Fatalf("test setup error: diff size %d should be > %d", len(largeDiff), MaxDiffSize)
	}

	result := BuildPromptWithDiff("Review this:", largeDiff)

	if !strings.Contains(result, TruncationNotice) {
		t.Error("expected truncation notice for large diff")
	}
	// The result should be reasonably sized (prompt + truncated diff + markdown)
	if len(result) > MaxDiffSize+1000 { // Allow for prompt and markdown overhead
		t.Errorf("result too large after truncation: %d", len(result))
	}
}

func TestBuildPromptWithDiff_EmptyDiff(t *testing.T) {
	result := BuildPromptWithDiff("Review this:", "")

	if !strings.Contains(result, "No changes detected") {
		t.Error("expected 'No changes detected' message for empty diff")
	}
}

func TestBuildPromptWithDiff_SmallDiff(t *testing.T) {
	diff := "diff --git a/file.go b/file.go\n+hello"
	result := BuildPromptWithDiff("Review:", diff)

	if strings.Contains(result, TruncationNotice) {
		t.Error("should not truncate small diff")
	}
	if !strings.Contains(result, diff) {
		t.Error("expected full diff in result")
	}
}
