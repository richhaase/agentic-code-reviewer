package fpcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestSaveLastRun_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "nested", "last-run.json")

	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Finding 1", Summary: "Summary 1"},
		},
	}

	if err := SaveLastRun(path, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}

func TestSaveLastRun_WritesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Finding 1", Summary: "Summary 1"},
			{Title: "Finding 2", Summary: "Summary 2"},
		},
		Info: []domain.FindingGroup{
			{Title: "Info 1", Summary: "Info summary"},
		},
	}

	if err := SaveLastRun(path, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var lastRun LastRun
	if err := json.Unmarshal(data, &lastRun); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(lastRun.Findings) != 3 {
		t.Fatalf("expected 3 findings (2 findings + 1 info), got %d", len(lastRun.Findings))
	}

	// Verify findings
	expected := []struct {
		title   string
		summary string
	}{
		{"Finding 1", "Summary 1"},
		{"Finding 2", "Summary 2"},
		{"Info 1", "Info summary"},
	}

	for i, want := range expected {
		if lastRun.Findings[i].Title != want.title {
			t.Errorf("finding[%d].Title = %q, want %q", i, lastRun.Findings[i].Title, want.title)
		}
		if lastRun.Findings[i].Summary != want.summary {
			t.Errorf("finding[%d].Summary = %q, want %q", i, lastRun.Findings[i].Summary, want.summary)
		}
	}
}

func TestSaveLastRun_EmptyFindings(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{},
		Info:     []domain.FindingGroup{},
	}

	if err := SaveLastRun(path, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var lastRun LastRun
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if err := json.Unmarshal(data, &lastRun); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(lastRun.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(lastRun.Findings))
	}
}

func TestSaveLastRun_PrettyPrintsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Finding", Summary: "Summary"},
		},
	}

	if err := SaveLastRun(path, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	// Verify it's pretty-printed by checking for indentation
	content := string(data)
	if !containsString(content, "  \"findings\"") {
		t.Error("expected pretty-printed JSON with indentation")
	}
}

func TestLoadLastRun_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	_, err := LoadLastRun(path)
	if err == nil {
		t.Error("expected error for missing file")
	}

	// Verify error message is helpful
	if !containsString(err.Error(), "last-run file not found") {
		t.Errorf("expected helpful error message, got: %v", err)
	}
}

func TestLoadLastRun_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := LoadLastRun(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	if !containsString(err.Error(), "failed to parse") {
		t.Errorf("expected parse error message, got: %v", err)
	}
}

func TestLoadLastRun_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	// Create valid JSON file
	content := `{
  "findings": [
    {
      "title": "Finding 1",
      "summary": "Summary 1"
    },
    {
      "title": "Finding 2",
      "summary": "Summary 2"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	lastRun, err := LoadLastRun(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(lastRun.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(lastRun.Findings))
	}

	expected := []struct {
		title   string
		summary string
	}{
		{"Finding 1", "Summary 1"},
		{"Finding 2", "Summary 2"},
	}

	for i, want := range expected {
		if lastRun.Findings[i].Title != want.title {
			t.Errorf("finding[%d].Title = %q, want %q", i, lastRun.Findings[i].Title, want.title)
		}
		if lastRun.Findings[i].Summary != want.summary {
			t.Errorf("finding[%d].Summary = %q, want %q", i, lastRun.Findings[i].Summary, want.summary)
		}
	}
}

func TestSaveLastRun_LoadLastRun_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	// Create input
	input := domain.GroupedFindings{
		Findings: []domain.FindingGroup{
			{Title: "Security issue", Summary: "SQL injection risk"},
			{Title: "Performance issue", Summary: "N+1 query detected"},
		},
		Info: []domain.FindingGroup{
			{Title: "Code style", Summary: "Consider using const"},
		},
	}

	// Save
	if err := SaveLastRun(path, input); err != nil {
		t.Fatalf("SaveLastRun failed: %v", err)
	}

	// Load
	lastRun, err := LoadLastRun(path)
	if err != nil {
		t.Fatalf("LoadLastRun failed: %v", err)
	}

	// Verify round-trip
	if len(lastRun.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(lastRun.Findings))
	}

	expected := []struct {
		title   string
		summary string
	}{
		{"Security issue", "SQL injection risk"},
		{"Performance issue", "N+1 query detected"},
		{"Code style", "Consider using const"},
	}

	for i, want := range expected {
		if lastRun.Findings[i].Title != want.title {
			t.Errorf("finding[%d].Title = %q, want %q", i, lastRun.Findings[i].Title, want.title)
		}
		if lastRun.Findings[i].Summary != want.summary {
			t.Errorf("finding[%d].Summary = %q, want %q", i, lastRun.Findings[i].Summary, want.summary)
		}
	}
}

func TestLoadLastRun_EmptyFindingsArray(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "last-run.json")

	content := `{"findings": []}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	lastRun, err := LoadLastRun(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(lastRun.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(lastRun.Findings))
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexString(s, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
