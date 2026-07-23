package store

import (
	"fmt"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type FindingV1 struct {
	Text       string `json:"text"`
	ReviewerID int    `json:"reviewer_id"`
}

func ToFindingSchema(f domain.Finding) FindingV1 {
	return FindingV1{Text: f.Text, ReviewerID: f.ReviewerID}
}

func (f FindingV1) ToDomain() domain.Finding {
	return domain.Finding{Text: f.Text, ReviewerID: f.ReviewerID}
}

func toFindingSchemaSlice(findings []domain.Finding) []FindingV1 {
	out := make([]FindingV1, 0, len(findings))
	for _, f := range findings {
		out = append(out, ToFindingSchema(f))
	}
	return out
}

func toFindingDomainSlice(findings []FindingV1) []domain.Finding {
	out := make([]domain.Finding, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.ToDomain())
	}
	return out
}

type AggregatedFindingV1 struct {
	Text      string `json:"text"`
	Reviewers []int  `json:"reviewers"`
}

func ToAggregatedFindingSchema(f domain.AggregatedFinding) AggregatedFindingV1 {
	return AggregatedFindingV1{Text: f.Text, Reviewers: append([]int(nil), f.Reviewers...)}
}

func (f AggregatedFindingV1) ToDomain() domain.AggregatedFinding {
	return domain.AggregatedFinding{Text: f.Text, Reviewers: append([]int(nil), f.Reviewers...)}
}

func toAggregatedFindingSchemaSlice(findings []domain.AggregatedFinding) []AggregatedFindingV1 {
	out := make([]AggregatedFindingV1, 0, len(findings))
	for _, f := range findings {
		out = append(out, ToAggregatedFindingSchema(f))
	}
	return out
}

func toAggregatedFindingDomainSlice(findings []AggregatedFindingV1) []domain.AggregatedFinding {
	out := make([]domain.AggregatedFinding, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.ToDomain())
	}
	return out
}

type FindingGroupV1 struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Messages      []string `json:"messages"`
	ReviewerCount int      `json:"reviewer_count"`
	Sources       []int    `json:"sources"`
}

func ToFindingGroupSchema(g domain.FindingGroup) FindingGroupV1 {
	return FindingGroupV1{
		Title:         g.Title,
		Summary:       g.Summary,
		Messages:      append([]string(nil), g.Messages...),
		ReviewerCount: g.ReviewerCount,
		Sources:       append([]int(nil), g.Sources...),
	}
}

func (g FindingGroupV1) ToDomain() domain.FindingGroup {
	return domain.FindingGroup{
		Title:         g.Title,
		Summary:       g.Summary,
		Messages:      append([]string(nil), g.Messages...),
		ReviewerCount: g.ReviewerCount,
		Sources:       append([]int(nil), g.Sources...),
	}
}

func toFindingGroupSchemaSlice(groups []domain.FindingGroup) []FindingGroupV1 {
	out := make([]FindingGroupV1, 0, len(groups))
	for _, g := range groups {
		out = append(out, ToFindingGroupSchema(g))
	}
	return out
}

func toFindingGroupDomainSlice(groups []FindingGroupV1) []domain.FindingGroup {
	out := make([]domain.FindingGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ToDomain())
	}
	return out
}

type GroupedFindingsV1 struct {
	Findings []FindingGroupV1 `json:"findings"`
	Info     []FindingGroupV1 `json:"info"`
}

func ToGroupedFindingsSchema(g domain.GroupedFindings) GroupedFindingsV1 {
	return GroupedFindingsV1{
		Findings: toFindingGroupSchemaSlice(g.Findings),
		Info:     toFindingGroupSchemaSlice(g.Info),
	}
}

func (g GroupedFindingsV1) ToDomain() domain.GroupedFindings {
	return domain.GroupedFindings{
		Findings: toFindingGroupDomainSlice(g.Findings),
		Info:     toFindingGroupDomainSlice(g.Info),
	}
}

type DispositionKindV1 string

const (
	DispositionKindUnmapped        DispositionKindV1 = "unmapped"
	DispositionKindInfo            DispositionKindV1 = "info"
	DispositionKindFilteredFP      DispositionKindV1 = "filtered_false_positive"
	DispositionKindFilteredExclude DispositionKindV1 = "filtered_exclude"
	DispositionKindSurvived        DispositionKindV1 = "survived"
)

func toDispositionKindSchema(kind domain.DispositionKind) (DispositionKindV1, error) {
	switch kind {
	case domain.DispositionUnmapped:
		return DispositionKindUnmapped, nil
	case domain.DispositionInfo:
		return DispositionKindInfo, nil
	case domain.DispositionFilteredFP:
		return DispositionKindFilteredFP, nil
	case domain.DispositionFilteredExclude:
		return DispositionKindFilteredExclude, nil
	case domain.DispositionSurvived:
		return DispositionKindSurvived, nil
	default:
		return "", fmt.Errorf("unknown disposition kind %d", kind)
	}
}

func (k DispositionKindV1) ToDomain() (domain.DispositionKind, error) {
	switch k {
	case DispositionKindUnmapped:
		return domain.DispositionUnmapped, nil
	case DispositionKindInfo:
		return domain.DispositionInfo, nil
	case DispositionKindFilteredFP:
		return domain.DispositionFilteredFP, nil
	case DispositionKindFilteredExclude:
		return domain.DispositionFilteredExclude, nil
	case DispositionKindSurvived:
		return domain.DispositionSurvived, nil
	default:
		return domain.DispositionUnmapped, fmt.Errorf("unknown stored disposition kind %q", k)
	}
}

type DispositionV1 struct {
	Kind       DispositionKindV1 `json:"kind"`
	FPScore    int               `json:"fp_score"`
	Reasoning  string            `json:"reasoning"`
	GroupTitle string            `json:"group_title"`
}

func ToDispositionSchema(d domain.Disposition) (DispositionV1, error) {
	kind, err := toDispositionKindSchema(d.Kind)
	if err != nil {
		return DispositionV1{}, err
	}
	return DispositionV1{
		Kind:       kind,
		FPScore:    d.FPScore,
		Reasoning:  d.Reasoning,
		GroupTitle: d.GroupTitle,
	}, nil
}

func (d DispositionV1) ToDomain() (domain.Disposition, error) {
	kind, err := d.Kind.ToDomain()
	if err != nil {
		return domain.Disposition{}, err
	}
	return domain.Disposition{
		Kind:       kind,
		FPScore:    d.FPScore,
		Reasoning:  d.Reasoning,
		GroupTitle: d.GroupTitle,
	}, nil
}

type ReviewFindingKindV1 string

const (
	ReviewFindingKindActionable    ReviewFindingKindV1 = "actionable"
	ReviewFindingKindInformational ReviewFindingKindV1 = "informational"
)

func toReviewFindingKindSchema(kind domain.ReviewFindingKind) (ReviewFindingKindV1, error) {
	switch kind {
	case domain.ReviewFindingActionable:
		return ReviewFindingKindActionable, nil
	case domain.ReviewFindingInformational:
		return ReviewFindingKindInformational, nil
	default:
		return "", fmt.Errorf("unknown review finding kind %q", kind)
	}
}

func (k ReviewFindingKindV1) ToDomain() (domain.ReviewFindingKind, error) {
	switch k {
	case ReviewFindingKindActionable:
		return domain.ReviewFindingActionable, nil
	case ReviewFindingKindInformational:
		return domain.ReviewFindingInformational, nil
	default:
		return "", fmt.Errorf("unknown stored review finding kind %q", k)
	}
}

type ReviewFindingV1 struct {
	ID          string              `json:"id"`
	Kind        ReviewFindingKindV1 `json:"kind"`
	Group       FindingGroupV1      `json:"group"`
	Disposition DispositionV1       `json:"disposition"`
}

func ToReviewFindingSchema(f domain.ReviewFinding) (ReviewFindingV1, error) {
	kind, err := toReviewFindingKindSchema(f.Kind)
	if err != nil {
		return ReviewFindingV1{}, fmt.Errorf("finding %s: %w", f.ID, err)
	}
	disposition, err := ToDispositionSchema(f.Disposition)
	if err != nil {
		return ReviewFindingV1{}, fmt.Errorf("finding %s: %w", f.ID, err)
	}
	return ReviewFindingV1{
		ID:          f.ID,
		Kind:        kind,
		Group:       ToFindingGroupSchema(f.Group),
		Disposition: disposition,
	}, nil
}

func (f ReviewFindingV1) ToDomain() (domain.ReviewFinding, error) {
	kind, err := f.Kind.ToDomain()
	if err != nil {
		return domain.ReviewFinding{}, fmt.Errorf("finding %s: %w", f.ID, err)
	}
	disposition, err := f.Disposition.ToDomain()
	if err != nil {
		return domain.ReviewFinding{}, fmt.Errorf("finding %s: %w", f.ID, err)
	}
	return domain.ReviewFinding{
		ID:          f.ID,
		Kind:        kind,
		Group:       f.Group.ToDomain(),
		Disposition: disposition,
	}, nil
}

func toReviewFindingSchemaSlice(findings []domain.ReviewFinding) ([]ReviewFindingV1, error) {
	out := make([]ReviewFindingV1, 0, len(findings))
	for _, f := range findings {
		schema, err := ToReviewFindingSchema(f)
		if err != nil {
			return nil, err
		}
		out = append(out, schema)
	}
	return out, nil
}

func toReviewFindingDomainSlice(findings []ReviewFindingV1) ([]domain.ReviewFinding, error) {
	out := make([]domain.ReviewFinding, 0, len(findings))
	for _, f := range findings {
		d, err := f.ToDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}
