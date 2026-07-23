package store

import (
	"encoding/json"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestFindingV1_RoundTrip(t *testing.T) {
	f := domain.Finding{Text: "possible nil deref", ReviewerID: 2}
	schema := ToFindingSchema(f)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded FindingV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := decoded.ToDomain(); got != f {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, f)
	}
}

func TestAggregatedFindingV1_RoundTrip(t *testing.T) {
	f := domain.AggregatedFinding{Text: "possible nil deref", Reviewers: []int{0, 2, 3}}
	schema := ToAggregatedFindingSchema(f)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AggregatedFindingV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := decoded.ToDomain()
	if got.Text != f.Text || len(got.Reviewers) != len(f.Reviewers) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, f)
	}
	for i := range f.Reviewers {
		if got.Reviewers[i] != f.Reviewers[i] {
			t.Fatalf("reviewers[%d]: got %d, want %d", i, got.Reviewers[i], f.Reviewers[i])
		}
	}
}

func TestFindingGroupV1_RoundTrip(t *testing.T) {
	g := domain.FindingGroup{
		Title:         "Nil pointer dereference",
		Summary:       "Two reviewers flagged a possible nil deref",
		Messages:      []string{"reviewer 1 said X", "reviewer 3 said Y"},
		ReviewerCount: 2,
		Sources:       []int{1, 3},
	}
	schema := ToFindingGroupSchema(g)
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded FindingGroupV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := decoded.ToDomain()
	if got.Title != g.Title || got.Summary != g.Summary || got.ReviewerCount != g.ReviewerCount {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, g)
	}
}

func TestDispositionV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		d    domain.Disposition
	}{
		{name: "unmapped", d: domain.Disposition{Kind: domain.DispositionUnmapped}},
		{name: "info", d: domain.Disposition{Kind: domain.DispositionInfo, GroupTitle: "note"}},
		{name: "filtered fp", d: domain.Disposition{Kind: domain.DispositionFilteredFP, FPScore: 85, Reasoning: "likely noise", GroupTitle: "FP"}},
		{name: "filtered exclude", d: domain.Disposition{Kind: domain.DispositionFilteredExclude, GroupTitle: "excluded"}},
		{name: "survived", d: domain.Disposition{Kind: domain.DispositionSurvived, GroupTitle: "real bug"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ToDispositionSchema(tt.d)
			if err != nil {
				t.Fatalf("ToDispositionSchema: %v", err)
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded DispositionV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, err := decoded.ToDomain()
			if err != nil {
				t.Fatalf("ToDomain: %v", err)
			}
			if got != tt.d {
				t.Fatalf("round trip mismatch: got %+v, want %+v", got, tt.d)
			}
		})
	}
}

func TestDispositionKindV1_ToDomain_RejectsUnknown(t *testing.T) {
	if _, err := DispositionKindV1("nonsense").ToDomain(); err == nil {
		t.Fatal("expected an error for an unknown stored disposition kind")
	}
}

func TestReviewFindingV1_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		f    domain.ReviewFinding
	}{
		{
			name: "actionable survived finding",
			f: domain.ReviewFinding{
				ID:          "finding-1",
				Kind:        domain.ReviewFindingActionable,
				Group:       domain.FindingGroup{Title: "Real bug", Sources: []int{0}},
				Disposition: domain.Disposition{Kind: domain.DispositionSurvived, GroupTitle: "Real bug"},
			},
		},
		{
			name: "informational finding",
			f: domain.ReviewFinding{
				ID:          "finding-2",
				Kind:        domain.ReviewFindingInformational,
				Group:       domain.FindingGroup{Title: "Style note", Sources: []int{1}},
				Disposition: domain.Disposition{Kind: domain.DispositionInfo, GroupTitle: "Style note"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ToReviewFindingSchema(tt.f)
			if err != nil {
				t.Fatalf("ToReviewFindingSchema: %v", err)
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded ReviewFindingV1
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, err := decoded.ToDomain()
			if err != nil {
				t.Fatalf("ToDomain: %v", err)
			}
			if got.ID != tt.f.ID || got.Kind != tt.f.Kind || got.Disposition != tt.f.Disposition {
				t.Fatalf("round trip mismatch: got %+v, want %+v", got, tt.f)
			}
		})
	}
}
