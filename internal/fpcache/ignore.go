// Package fpcache provides utilities for managing false positive caching and filtering.
package fpcache

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// LoadIgnoreFile loads ignore patterns from the specified file.
// Returns an empty slice if the file doesn't exist (not an error).
// Creates an empty file if it doesn't exist.
func LoadIgnoreFile(path string) ([]string, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Check if file exists
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		// Create empty file
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			return nil, fmt.Errorf("failed to create ignore file: %w", err)
		}
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open ignore file: %w", err)
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			patterns = append(patterns, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read ignore file: %w", err)
	}

	return patterns, nil
}

// SaveIgnoreFile writes patterns to the specified file, one per line.
func SaveIgnoreFile(path string, patterns []string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write patterns
	var content strings.Builder
	for _, pattern := range patterns {
		content.WriteString(pattern)
		content.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write ignore file: %w", err)
	}

	return nil
}

// MatchesIgnore returns true if the title matches any of the ignore patterns.
// Matching is done via substring search (case-sensitive).
func MatchesIgnore(title string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(title, pattern) {
			return true
		}
	}
	return false
}

// ApplyIgnoreFilter filters findings and info items, removing any that match ignore patterns.
// Returns a new GroupedFindings with filtered items and the count of filtered items.
func ApplyIgnoreFilter(findings domain.GroupedFindings, patterns []string) (domain.GroupedFindings, int) {
	if len(patterns) == 0 {
		return findings, 0
	}

	filtered := domain.GroupedFindings{
		Findings: make([]domain.FindingGroup, 0, len(findings.Findings)),
		Info:     make([]domain.FindingGroup, 0, len(findings.Info)),
	}

	ignoredCount := 0

	// Filter findings
	for _, f := range findings.Findings {
		if !MatchesIgnore(f.Title, patterns) {
			filtered.Findings = append(filtered.Findings, f)
		} else {
			ignoredCount++
		}
	}

	// Filter info
	for _, i := range findings.Info {
		if !MatchesIgnore(i.Title, patterns) {
			filtered.Info = append(filtered.Info, i)
		} else {
			ignoredCount++
		}
	}

	return filtered, ignoredCount
}
