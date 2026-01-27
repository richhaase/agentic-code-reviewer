package fpcache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// LastRunFinding represents a finding from the last review run.
type LastRunFinding struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// LastRun represents the findings from the last review run.
type LastRun struct {
	Findings []LastRunFinding `json:"findings"`
}

// SaveLastRun saves the findings to a JSON file at the specified path.
// Creates the directory if it doesn't exist.
func SaveLastRun(path string, findings domain.GroupedFindings) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Convert GroupedFindings to LastRun
	lastRun := LastRun{
		Findings: make([]LastRunFinding, 0, findings.TotalGroups()),
	}

	// Add findings
	for _, f := range findings.Findings {
		lastRun.Findings = append(lastRun.Findings, LastRunFinding{
			Title:   f.Title,
			Summary: f.Summary,
		})
	}

	// Add info items
	for _, i := range findings.Info {
		lastRun.Findings = append(lastRun.Findings, LastRunFinding{
			Title:   i.Title,
			Summary: i.Summary,
		})
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(lastRun, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal findings: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write last-run file: %w", err)
	}

	return nil
}

// LoadLastRun loads the findings from the specified JSON file.
func LoadLastRun(path string) (LastRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LastRun{}, fmt.Errorf("last-run file not found: %s", path)
		}
		return LastRun{}, fmt.Errorf("failed to read last-run file: %w", err)
	}

	var lastRun LastRun
	if err := json.Unmarshal(data, &lastRun); err != nil {
		return LastRun{}, fmt.Errorf("failed to parse last-run file: %w", err)
	}

	return lastRun, nil
}
