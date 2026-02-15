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

// DispositionKind describes what happened to an aggregated finding in the pipeline.
type DispositionKind int

const (
	DispositionUnmapped        DispositionKind = iota // Could not trace through pipeline (zero value)
	DispositionInfo                                   // Categorized as informational by summarizer
	DispositionFilteredFP                             // Removed by FP filter
	DispositionFilteredExclude                        // Removed by exclude pattern
	DispositionSurvived                               // Survived all filters (became a posted finding)
)

// Disposition describes the pipeline outcome of an aggregated finding.
type Disposition struct {
	Kind       DispositionKind
	FPScore    int    // Only set for DispositionFilteredFP
	Reasoning  string // Only set for DispositionFilteredFP
	GroupTitle string
}

// FPRemovedInfo captures metadata about a finding group removed by the FP filter.
type FPRemovedInfo struct {
	Sources   []int
	FPScore   int
	Reasoning string
	Title     string
}

// BuildDispositions maps each aggregated finding index to its pipeline disposition.
func BuildDispositions(
	aggregatedCount int,
	infoGroups []FindingGroup,
	fpRemoved []FPRemovedInfo,
	survivingFindings []FindingGroup,
) map[int]Disposition {
	dispositions := make(map[int]Disposition, aggregatedCount)

	// 1. Mark info groups
	for _, g := range infoGroups {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionInfo,
				GroupTitle: g.Title,
			}
		}
	}

	// 2. Mark FP-filtered
	for _, fp := range fpRemoved {
		for _, src := range fp.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionFilteredFP,
				FPScore:    fp.FPScore,
				Reasoning:  fp.Reasoning,
				GroupTitle: fp.Title,
			}
		}
	}

	// 3. Mark survivors
	for _, g := range survivingFindings {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionSurvived,
				GroupTitle: g.Title,
			}
		}
	}

	// 4. Fill remaining unmapped indices (zero value is DispositionUnmapped)
	for i := range aggregatedCount {
		if _, ok := dispositions[i]; !ok {
			dispositions[i] = Disposition{}
		}
	}

	return dispositions
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
