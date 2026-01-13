package domain

import "slices"

// Finding represents a single review finding from a reviewer iteration.
type Finding struct {
	Text       string
	ReviewerID int
}

// AggregatedFinding represents a finding with the list of reviewers who found it.
type AggregatedFinding struct {
	Text      string
	Reviewers []int
}

// FindingGroup represents a grouped/clustered finding from the summarizer.
type FindingGroup struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Messages      []string `json:"messages"`
	ReviewerCount int      `json:"reviewer_count"`
	Sources       []int    `json:"sources"`
}

// GroupedFindings represents the output from the summarizer.
type GroupedFindings struct {
	Findings []FindingGroup `json:"findings"`
	Info     []FindingGroup `json:"info"`
}

// HasFindings returns true if there are any findings.
func (g *GroupedFindings) HasFindings() bool {
	return len(g.Findings) > 0
}

// HasInfo returns true if there are any informational notes.
func (g *GroupedFindings) HasInfo() bool {
	return len(g.Info) > 0
}

// TotalGroups returns the total count of finding groups and info groups.
func (g *GroupedFindings) TotalGroups() int {
	return len(g.Findings) + len(g.Info)
}

// AggregateFindings aggregates findings by text, tracking which reviewers found each.
func AggregateFindings(findings []Finding) []AggregatedFinding {
	seen := make(map[string][]int)
	order := make([]string, 0)

	for _, f := range findings {
		normalized := f.Text
		if normalized == "" {
			continue
		}

		reviewers, exists := seen[normalized]
		if !exists {
			order = append(order, normalized)
			reviewers = nil
		}

		found := false
		for _, r := range reviewers {
			if r == f.ReviewerID {
				found = true
				break
			}
		}
		if !found {
			seen[normalized] = append(reviewers, f.ReviewerID)
		}
	}

	result := make([]AggregatedFinding, 0, len(order))
	for _, text := range order {
		reviewers := seen[text]
		sortedReviewers := slices.Clone(reviewers)
		slices.Sort(sortedReviewers)
		result = append(result, AggregatedFinding{
			Text:      text,
			Reviewers: sortedReviewers,
		})
	}

	return result
}
