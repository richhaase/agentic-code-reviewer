package filter

import (
	"regexp"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type Filter struct {
	excludePatterns []*regexp.Regexp
}

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
		Info:     grouped.Info,
	}
}

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
