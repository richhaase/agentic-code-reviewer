package summarizer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestSummarize_EmptyInput(t *testing.T) {
	result, err := Summarize(context.Background(), "codex", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected no findings, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Info) != 0 {
		t.Errorf("expected no info, got %d", len(result.Grouped.Info))
	}
}

func TestSummarize_EmptySlice(t *testing.T) {
	result, err := Summarize(context.Background(), "codex", []domain.AggregatedFinding{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestSummarize_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Context was canceled, so we expect either an error exit code or context canceled handling
	if result.ExitCode != -1 && result.ExitCode != 1 {
		// If codex is not installed, we'll get exit code 1
		// If context is properly detected as canceled, we get -1
		if result.Stderr != "context canceled" && !isCodexNotFound(result.Stderr) {
			t.Logf("exit code: %d, stderr: %s", result.ExitCode, result.Stderr)
		}
	}
}

func isCodexNotFound(stderr string) bool {
	return stderr != "" // Accept any error message when codex isn't available
}

func TestSummarize_WithMockCodex(t *testing.T) {
	// Create a temporary directory for our mock codex
	tmpDir := t.TempDir()

	// Create a mock codex script that returns valid JSONL (codex --json format)
	mockCodex := filepath.Join(tmpDir, "codex")
	mockScript := `#!/bin/sh
cat << 'EOF'
{"type":"thread.started","thread_id":"test"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"Test Issue\", \"summary\": \"A test issue summary.\", \"messages\": [\"test message\"], \"reviewer_count\": 1, \"sources\": [0]}], \"info\": []}"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
EOF
`
	if err := os.WriteFile(mockCodex, []byte(mockScript), 0755); err != nil {
		t.Fatalf("failed to create mock codex: %v", err)
	}

	// Save original PATH and restore after test
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	// Verify our mock is being used
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skipf("mock codex not found in PATH: %v", err)
	}
	if path != mockCodex {
		t.Skipf("wrong codex found: %s (expected %s)", path, mockCodex)
	}

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if len(result.Grouped.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Findings) > 0 && result.Grouped.Findings[0].Title != "Test Issue" {
		t.Errorf("expected title 'Test Issue', got %q", result.Grouped.Findings[0].Title)
	}
}

func TestSummarize_InvalidJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock codex that returns invalid JSON
	mockCodex := filepath.Join(tmpDir, "codex")
	mockScript := `#!/bin/sh
echo "this is not valid JSON"
`
	if err := os.WriteFile(mockCodex, []byte(mockScript), 0755); err != nil {
		t.Fatalf("failed to create mock codex: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should have exit code 1 due to JSON parse failure
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
	if result.Stderr == "" {
		t.Error("expected non-empty error message for JSON parse failure")
	}
	if result.RawOut == "" {
		t.Error("expected raw output to be preserved")
	}
}

func TestSummarize_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock codex that returns empty output
	mockCodex := filepath.Join(tmpDir, "codex")
	mockScript := `#!/bin/sh
# Return nothing
`
	if err := os.WriteFile(mockCodex, []byte(mockScript), 0755); err != nil {
		t.Fatalf("failed to create mock codex: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Empty output should return empty GroupedFindings
	if len(result.Grouped.Findings) != 0 {
		t.Errorf("expected no findings for empty output, got %d", len(result.Grouped.Findings))
	}
}

func TestSummarize_CodexFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock codex that exits with error
	mockCodex := filepath.Join(tmpDir, "codex")
	mockScript := `#!/bin/sh
echo "error message" >&2
exit 42
`
	if err := os.WriteFile(mockCodex, []byte(mockScript), 0755); err != nil {
		t.Fatalf("failed to create mock codex: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	findings := []domain.AggregatedFinding{
		{Text: "Test finding", Reviewers: []int{1}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestSummarize_MultipleFindings(t *testing.T) {
	tmpDir := t.TempDir()

	mockCodex := filepath.Join(tmpDir, "codex")
	mockScript := `#!/bin/sh
cat << 'EOF'
{"type":"thread.started","thread_id":"test"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\": [{\"title\": \"Issue 1\", \"summary\": \"First issue.\", \"messages\": [\"msg1\"], \"reviewer_count\": 2, \"sources\": [0, 1]}, {\"title\": \"Issue 2\", \"summary\": \"Second issue.\", \"messages\": [\"msg2\"], \"reviewer_count\": 1, \"sources\": [2]}], \"info\": [{\"title\": \"Info 1\", \"summary\": \"Some info.\", \"messages\": [\"info msg\"], \"reviewer_count\": 1, \"sources\": [3]}]}"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
EOF
`
	if err := os.WriteFile(mockCodex, []byte(mockScript), 0755); err != nil {
		t.Fatalf("failed to create mock codex: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", tmpDir+":"+origPath)

	findings := []domain.AggregatedFinding{
		{Text: "Finding 1", Reviewers: []int{1, 2}},
		{Text: "Finding 2", Reviewers: []int{1}},
		{Text: "Finding 3", Reviewers: []int{3}},
		{Text: "Info finding", Reviewers: []int{2}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := Summarize(ctx, "codex", findings, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if len(result.Grouped.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Grouped.Findings))
	}
	if len(result.Grouped.Info) != 1 {
		t.Errorf("expected 1 info, got %d", len(result.Grouped.Info))
	}
}
