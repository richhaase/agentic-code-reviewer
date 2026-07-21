package domain

import "slices"

type Finding struct {
	Text       string
	ReviewerID int
}

type AggregatedFinding struct {
	Text      string
	Reviewers []int
}

type FindingGroup struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Messages      []string `json:"messages"`
	ReviewerCount int      `json:"reviewer_count"`
	Sources       []int    `json:"sources"`
}

type GroupedFindings struct {
	Findings []FindingGroup `json:"findings"`
	Info     []FindingGroup `json:"info"`
}

func (g *GroupedFindings) HasFindings() bool {
	return len(g.Findings) > 0
}

func (g *GroupedFindings) HasInfo() bool {
	return len(g.Info) > 0
}

func (g *GroupedFindings) TotalGroups() int {
	return len(g.Findings) + len(g.Info)
}

type DispositionKind int

const (
	DispositionUnmapped DispositionKind = iota
	DispositionInfo
	DispositionFilteredFP
	DispositionFilteredExclude
	DispositionSurvived
)

type Disposition struct {
	Kind       DispositionKind
	FPScore    int
	Reasoning  string
	GroupTitle string
}

type FPRemovedInfo struct {
	Sources   []int
	FPScore   int
	Reasoning string
	Title     string
}

func BuildDispositions(
	aggregatedCount int,
	infoGroups []FindingGroup,
	fpRemoved []FPRemovedInfo,
	excludeFiltered []FindingGroup,
	survivingFindings []FindingGroup,
) map[int]Disposition {
	dispositions := make(map[int]Disposition, aggregatedCount)

	for _, g := range infoGroups {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionInfo,
				GroupTitle: g.Title,
			}
		}
	}

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

	for _, g := range excludeFiltered {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionFilteredExclude,
				GroupTitle: g.Title,
			}
		}
	}

	for _, g := range survivingFindings {
		for _, src := range g.Sources {
			dispositions[src] = Disposition{
				Kind:       DispositionSurvived,
				GroupTitle: g.Title,
			}
		}
	}

	for i := range aggregatedCount {
		if _, ok := dispositions[i]; !ok {
			dispositions[i] = Disposition{}
		}
	}

	return dispositions
}

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
