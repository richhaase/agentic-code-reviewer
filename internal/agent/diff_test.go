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

func TestParseDiffIntoFiles_EmptyDiff(t *testing.T) {
	files := ParseDiffIntoFiles("")
	if len(files) != 0 {
		t.Errorf("expected empty slice for empty diff, got %d files", len(files))
	}
}

func TestParseDiffIntoFiles_SingleFile(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {}`

	files := ParseDiffIntoFiles(diff)

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	if files[0].Filename != "file.go" {
		t.Errorf("expected filename 'file.go', got %q", files[0].Filename)
	}

	if !strings.Contains(files[0].Content, "diff --git") {
		t.Error("expected content to include diff header")
	}

	if files[0].Size != len(files[0].Content) {
		t.Errorf("expected size %d to match content length %d", files[0].Size, len(files[0].Content))
	}
}

func TestParseDiffIntoFiles_MultipleFiles(t *testing.T) {
	diff := `diff --git a/file1.go b/file1.go
+first file content
diff --git a/file2.go b/file2.go
+second file content
diff --git a/path/to/file3.go b/path/to/file3.go
+third file content`

	files := ParseDiffIntoFiles(diff)

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	expectedFilenames := []string{"file1.go", "file2.go", "path/to/file3.go"}
	for i, expected := range expectedFilenames {
		if files[i].Filename != expected {
			t.Errorf("file %d: expected filename %q, got %q", i, expected, files[i].Filename)
		}
	}

	// Verify each file contains only its content
	if !strings.Contains(files[0].Content, "first file") || strings.Contains(files[0].Content, "second file") {
		t.Error("file 1 should only contain first file content")
	}
	if !strings.Contains(files[1].Content, "second file") || strings.Contains(files[1].Content, "third file") {
		t.Error("file 2 should only contain second file content")
	}
	if !strings.Contains(files[2].Content, "third file") {
		t.Error("file 3 should contain third file content")
	}
}

func TestParseDiffIntoFiles_RenamedFile(t *testing.T) {
	// When a file is renamed, the a/ and b/ paths differ
	diff := `diff --git a/old_name.go b/new_name.go
similarity index 95%
rename from old_name.go
rename to new_name.go
+some content`

	files := ParseDiffIntoFiles(diff)

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	// Should use the b/ path (destination/new name)
	if files[0].Filename != "new_name.go" {
		t.Errorf("expected filename 'new_name.go' (destination), got %q", files[0].Filename)
	}
}

func TestDistributeFiles_ZeroReviewers(t *testing.T) {
	files := []FileDiff{{Filename: "test.go", Content: "content", Size: 7}}

	result := DistributeFiles(files, 0)
	if result != nil {
		t.Error("expected nil for zero reviewers")
	}

	result = DistributeFiles(files, -1)
	if result != nil {
		t.Error("expected nil for negative reviewers")
	}
}

func TestDistributeFiles_EmptyFiles(t *testing.T) {
	result := DistributeFiles([]FileDiff{}, 3)

	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}

	for i, chunk := range result {
		if chunk != "" {
			t.Errorf("chunk %d should be empty, got %q", i, chunk)
		}
	}
}

func TestDistributeFiles_RoundRobin(t *testing.T) {
	files := []FileDiff{
		{Filename: "file1.go", Content: "content1", Size: 8},
		{Filename: "file2.go", Content: "content2", Size: 8},
		{Filename: "file3.go", Content: "content3", Size: 8},
		{Filename: "file4.go", Content: "content4", Size: 8},
		{Filename: "file5.go", Content: "content5", Size: 8},
	}

	// Distribute 5 files across 3 reviewers
	result := DistributeFiles(files, 3)

	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}

	// Reviewer 0 gets files 0, 3 (indices 0, 3)
	if !strings.Contains(result[0], "content1") || !strings.Contains(result[0], "content4") {
		t.Errorf("reviewer 0 should have files 1 and 4, got %q", result[0])
	}

	// Reviewer 1 gets files 1, 4 (indices 1, 4)
	if !strings.Contains(result[1], "content2") || !strings.Contains(result[1], "content5") {
		t.Errorf("reviewer 1 should have files 2 and 5, got %q", result[1])
	}

	// Reviewer 2 gets file 2 (index 2)
	if !strings.Contains(result[2], "content3") {
		t.Errorf("reviewer 2 should have file 3, got %q", result[2])
	}
}

func TestDistributeFiles_MoreReviewersThanFiles(t *testing.T) {
	files := []FileDiff{
		{Filename: "file1.go", Content: "content1", Size: 8},
		{Filename: "file2.go", Content: "content2", Size: 8},
	}

	// 2 files distributed to 5 reviewers
	result := DistributeFiles(files, 5)

	if len(result) != 5 {
		t.Fatalf("expected 5 chunks, got %d", len(result))
	}

	// Reviewers 0, 1 get files; reviewers 2, 3, 4 get nothing
	if !strings.Contains(result[0], "content1") {
		t.Error("reviewer 0 should have file 1")
	}
	if !strings.Contains(result[1], "content2") {
		t.Error("reviewer 1 should have file 2")
	}
	for i := 2; i < 5; i++ {
		if result[i] != "" {
			t.Errorf("reviewer %d should have empty chunk, got %q", i, result[i])
		}
	}
}

func TestDistributeFiles_LargeFileTruncated(t *testing.T) {
	// Create a file larger than MaxDiffSize
	largeContent := strings.Repeat("x", MaxDiffSize+1000)
	files := []FileDiff{
		{Filename: "large.go", Content: largeContent, Size: len(largeContent)},
	}

	result := DistributeFiles(files, 1)

	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}

	// The chunk should be truncated and include the truncation notice
	if !strings.Contains(result[0], TruncationNotice) {
		t.Error("large file should be truncated")
	}

	// Result should be smaller than original
	if len(result[0]) >= len(largeContent) {
		t.Errorf("truncated content (%d bytes) should be smaller than original (%d bytes)",
			len(result[0]), len(largeContent))
	}
}
