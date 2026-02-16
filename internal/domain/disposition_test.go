package domain

import "testing"

func TestBuildDispositions_InfoGroups(t *testing.T) {
	dispositions := BuildDispositions(
		3,
		[]FindingGroup{
			{Title: "Style note", Sources: []int{0, 2}},
		},
		nil, nil, nil,
	)

	if d := dispositions[0]; d.Kind != DispositionInfo || d.GroupTitle != "Style note" {
		t.Errorf("index 0: got %+v, want Info with title 'Style note'", d)
	}
	if d := dispositions[2]; d.Kind != DispositionInfo {
		t.Errorf("index 2: got %+v, want Info", d)
	}
	if d := dispositions[1]; d.Kind != DispositionUnmapped {
		t.Errorf("index 1: got %+v, want Unmapped", d)
	}
}

func TestBuildDispositions_FPFiltered(t *testing.T) {
	dispositions := BuildDispositions(
		2,
		nil,
		[]FPRemovedInfo{
			{Sources: []int{1}, FPScore: 85, Reasoning: "likely false positive", Title: "FP Group"},
		},
		nil, nil,
	)

	d := dispositions[1]
	if d.Kind != DispositionFilteredFP {
		t.Errorf("index 1: got kind %d, want DispositionFilteredFP", d.Kind)
	}
	if d.FPScore != 85 {
		t.Errorf("index 1: got FPScore %d, want 85", d.FPScore)
	}
	if d.Reasoning != "likely false positive" {
		t.Errorf("index 1: got Reasoning %q, want 'likely false positive'", d.Reasoning)
	}
	if d.GroupTitle != "FP Group" {
		t.Errorf("index 1: got GroupTitle %q, want 'FP Group'", d.GroupTitle)
	}
}

func TestBuildDispositions_Survived(t *testing.T) {
	dispositions := BuildDispositions(
		3,
		nil, nil, nil,
		[]FindingGroup{
			{Title: "Real bug", Sources: []int{0, 2}},
		},
	)

	if d := dispositions[0]; d.Kind != DispositionSurvived {
		t.Errorf("index 0: got %+v, want Survived", d)
	}
	if d := dispositions[2]; d.Kind != DispositionSurvived {
		t.Errorf("index 2: got %+v, want Survived", d)
	}
	if d := dispositions[1]; d.Kind != DispositionUnmapped {
		t.Errorf("index 1: got %+v, want Unmapped", d)
	}
}

func TestBuildDispositions_ExcludeFiltered(t *testing.T) {
	dispositions := BuildDispositions(
		3,
		nil, nil,
		[]FindingGroup{
			{Title: "Excluded finding", Sources: []int{0, 2}},
		},
		nil,
	)

	if d := dispositions[0]; d.Kind != DispositionFilteredExclude || d.GroupTitle != "Excluded finding" {
		t.Errorf("index 0: got %+v, want FilteredExclude with title 'Excluded finding'", d)
	}
	if d := dispositions[2]; d.Kind != DispositionFilteredExclude {
		t.Errorf("index 2: got %+v, want FilteredExclude", d)
	}
	if d := dispositions[1]; d.Kind != DispositionUnmapped {
		t.Errorf("index 1: got %+v, want Unmapped", d)
	}
}

func TestBuildDispositions_UnmappedRemainUnmapped(t *testing.T) {
	dispositions := BuildDispositions(2, nil, nil, nil, nil)

	if d := dispositions[0]; d.Kind != DispositionUnmapped {
		t.Errorf("index 0: got %+v, want Unmapped", d)
	}
	if d := dispositions[1]; d.Kind != DispositionUnmapped {
		t.Errorf("index 1: got %+v, want Unmapped", d)
	}
}

func TestBuildDispositions_PriorityOverride(t *testing.T) {
	// Later steps override earlier ones: FP overrides Info, Survived overrides FP
	dispositions := BuildDispositions(
		3,
		[]FindingGroup{
			{Title: "Info", Sources: []int{0, 1, 2}},
		},
		[]FPRemovedInfo{
			{Sources: []int{1}, FPScore: 90, Title: "FP"},
		},
		nil,
		[]FindingGroup{
			{Title: "Survived", Sources: []int{2}},
		},
	)

	if d := dispositions[0]; d.Kind != DispositionInfo {
		t.Errorf("index 0: got %+v, want Info (not overridden)", d)
	}
	if d := dispositions[1]; d.Kind != DispositionFilteredFP {
		t.Errorf("index 1: got %+v, want FilteredFP (overrides Info)", d)
	}
	if d := dispositions[2]; d.Kind != DispositionSurvived {
		t.Errorf("index 2: got %+v, want Survived (overrides Info)", d)
	}
}

func TestBuildDispositions_Empty(t *testing.T) {
	dispositions := BuildDispositions(0, nil, nil, nil, nil)
	if len(dispositions) != 0 {
		t.Errorf("expected empty map for 0 aggregated, got %d entries", len(dispositions))
	}
}

func TestBuildDispositions_AllKindsCombined(t *testing.T) {
	dispositions := BuildDispositions(
		6,
		[]FindingGroup{
			{Title: "Info note", Sources: []int{0}},
		},
		[]FPRemovedInfo{
			{Sources: []int{1}, FPScore: 82, Reasoning: "noise", Title: "FP finding"},
		},
		[]FindingGroup{
			{Title: "Excluded thing", Sources: []int{3}},
		},
		[]FindingGroup{
			{Title: "Real issue", Sources: []int{2}},
		},
	)

	tests := []struct {
		index    int
		wantKind DispositionKind
	}{
		{0, DispositionInfo},
		{1, DispositionFilteredFP},
		{2, DispositionSurvived},
		{3, DispositionFilteredExclude},
		{4, DispositionUnmapped},
		{5, DispositionUnmapped},
	}

	for _, tt := range tests {
		if d := dispositions[tt.index]; d.Kind != tt.wantKind {
			t.Errorf("index %d: got kind %d, want %d", tt.index, d.Kind, tt.wantKind)
		}
	}
}
