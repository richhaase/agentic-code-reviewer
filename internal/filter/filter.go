// Package filter provides filtering capabilities for code review findings.
package filter

import (
	"regexp"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

// Filter holds compiled regex patterns for excluding findings.
type Filter struct {
	excludePatterns []*regexp.Regexp
}

// New creates a Filter from pattern strings.
// Returns an error if any pattern is an invalid regex.
func New(patterns []string) (*Filter, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}
	return &Filter{excludePatterns: compiled}, nil
}

// Apply returns a new GroupedFindings with excluded findings removed.
// Matches patterns against finding Messages[] (original reviewer content).
// Only filters Findings, not Info.
// Does not mutate the original.
func (f *Filter) Apply(grouped domain.GroupedFindings) domain.GroupedFindings {
	if len(f.excludePatterns) == 0 {
		return grouped
	}

	filtered := make([]domain.FindingGroup, 0, len(grouped.Findings))
	for _, finding := range grouped.Findings {
		if !f.shouldExclude(finding) {
			filtered = append(filtered, finding)
		}
	}

	return domain.GroupedFindings{
		Findings: filtered,
		Info:     grouped.Info, // Info is never filtered
	}
}

// shouldExclude returns true if any exclude pattern matches any message in the finding.
func (f *Filter) shouldExclude(finding domain.FindingGroup) bool {
	for _, msg := range finding.Messages {
		for _, re := range f.excludePatterns {
			if re.MatchString(msg) {
				return true
			}
		}
	}
	return false
}
