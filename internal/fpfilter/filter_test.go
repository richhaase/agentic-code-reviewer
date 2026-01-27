package fpfilter

import (
	"encoding/json"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

func TestFindingEvaluationConstruction(t *testing.T) {
	tests := []struct {
		name          string
		reviewerCount int
	}{
		{"zero passed through", 0},
		{"negative passed through", -5},
		{"positive value passed through", 5},
		{"one passed through", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := domain.FindingGroup{
				Title:         "Test",
				Summary:       "Test summary",
				Messages:      []string{"msg1"},
				ReviewerCount: tt.reviewerCount,
			}

			// Construct FindingEvaluation the same way Apply() does
			input := FindingEvaluation{
				ID:              0,
				Title:           finding.Title,
				Summary:         finding.Summary,
				Messages:        finding.Messages,
				ReviewerCount:   finding.ReviewerCount,
				IsFalsePositive: nil,
				Reasoning:       nil,
			}

			if input.ReviewerCount != tt.reviewerCount {
				t.Errorf("ReviewerCount = %d, want %d", input.ReviewerCount, tt.reviewerCount)
			}

			// Verify it serializes correctly to JSON with null fields
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var parsed FindingEvaluation
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if parsed.ReviewerCount != tt.reviewerCount {
				t.Errorf("after JSON round-trip: ReviewerCount = %d, want %d", parsed.ReviewerCount, tt.reviewerCount)
			}

			// Verify null fields remain null after round-trip
			if parsed.IsFalsePositive != nil {
				t.Errorf("IsFalsePositive should be nil, got %v", *parsed.IsFalsePositive)
			}
			if parsed.Reasoning != nil {
				t.Errorf("Reasoning should be nil, got %q", *parsed.Reasoning)
			}
		})
	}
}

func TestFindingEvaluationJSONMarshaling(t *testing.T) {
	tests := []struct {
		name string
		eval FindingEvaluation
		want string
	}{
		{
			name: "null fields omitted",
			eval: FindingEvaluation{
				ID:              0,
				Title:           "Test Finding",
				Summary:         "Summary",
				Messages:        []string{"msg1"},
				ReviewerCount:   2,
				IsFalsePositive: nil,
				Reasoning:       nil,
			},
			want: `{"id":0,"title":"Test Finding","summary":"Summary","messages":["msg1"],"reviewer_count":2}`,
		},
		{
			name: "false positive with reasoning",
			eval: FindingEvaluation{
				ID:              1,
				Title:           "False Positive",
				Summary:         "Not a real issue",
				Messages:        []string{"msg1"},
				ReviewerCount:   1,
				IsFalsePositive: boolPtr(true),
				Reasoning:       strPtr("This is a test case"),
			},
			want: `{"id":1,"title":"False Positive","summary":"Not a real issue","messages":["msg1"],"reviewer_count":1,"is_false_positive":true,"reasoning":"This is a test case"}`,
		},
		{
			name: "real finding with reasoning",
			eval: FindingEvaluation{
				ID:              2,
				Title:           "Real Finding",
				Summary:         "Actual bug",
				Messages:        []string{"msg1"},
				ReviewerCount:   3,
				IsFalsePositive: boolPtr(false),
				Reasoning:       strPtr("This is a valid security issue"),
			},
			want: `{"id":2,"title":"Real Finding","summary":"Actual bug","messages":["msg1"],"reviewer_count":3,"is_false_positive":false,"reasoning":"This is a valid security issue"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.eval)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			if string(data) != tt.want {
				t.Errorf("JSON output mismatch\ngot:  %s\nwant: %s", string(data), tt.want)
			}

			// Verify round-trip
			var parsed FindingEvaluation
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if parsed.ID != tt.eval.ID {
				t.Errorf("ID = %d, want %d", parsed.ID, tt.eval.ID)
			}
			if parsed.Title != tt.eval.Title {
				t.Errorf("Title = %q, want %q", parsed.Title, tt.eval.Title)
			}
			if !boolPtrEqual(parsed.IsFalsePositive, tt.eval.IsFalsePositive) {
				t.Errorf("IsFalsePositive mismatch: got %v, want %v", parsed.IsFalsePositive, tt.eval.IsFalsePositive)
			}
			if !strPtrEqual(parsed.Reasoning, tt.eval.Reasoning) {
				t.Errorf("Reasoning mismatch: got %v, want %v", parsed.Reasoning, tt.eval.Reasoning)
			}
		})
	}
}

func TestInPlaceUpdateParsing(t *testing.T) {
	tests := []struct {
		name         string
		inputJSON    string
		wantFiltered int
		wantKept     int
		wantErrors   int
	}{
		{
			name: "all findings marked as false positives",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test 1",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 1,
						"is_false_positive": true,
						"reasoning": "Not a real issue"
					},
					{
						"id": 1,
						"title": "Test 2",
						"summary": "Summary 2",
						"messages": ["msg2"],
						"reviewer_count": 1,
						"is_false_positive": true,
						"reasoning": "Also not real"
					}
				]
			}`,
			wantFiltered: 2,
			wantKept:     0,
			wantErrors:   0,
		},
		{
			name: "all findings marked as real",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test 1",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 2,
						"is_false_positive": false,
						"reasoning": "This is a valid bug"
					},
					{
						"id": 1,
						"title": "Test 2",
						"summary": "Summary 2",
						"messages": ["msg2"],
						"reviewer_count": 3,
						"is_false_positive": false,
						"reasoning": "Also valid"
					}
				]
			}`,
			wantFiltered: 0,
			wantKept:     2,
			wantErrors:   0,
		},
		{
			name: "mixed false positives and real findings",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "False Positive",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 1,
						"is_false_positive": true,
						"reasoning": "Not real"
					},
					{
						"id": 1,
						"title": "Real Bug",
						"summary": "Summary 2",
						"messages": ["msg2"],
						"reviewer_count": 3,
						"is_false_positive": false,
						"reasoning": "Valid issue"
					},
					{
						"id": 2,
						"title": "Another FP",
						"summary": "Summary 3",
						"messages": ["msg3"],
						"reviewer_count": 1,
						"is_false_positive": true,
						"reasoning": "Test code"
					}
				]
			}`,
			wantFiltered: 2,
			wantKept:     1,
			wantErrors:   0,
		},
		{
			name: "missing is_false_positive field keeps finding",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test 1",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 1,
						"reasoning": "Has reasoning but no is_false_positive"
					}
				]
			}`,
			wantFiltered: 0,
			wantKept:     1,
			wantErrors:   1,
		},
		{
			name: "missing reasoning still works",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test 1",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 1,
						"is_false_positive": true
					}
				]
			}`,
			wantFiltered: 1,
			wantKept:     0,
			wantErrors:   0,
		},
		{
			name: "missing finding from response keeps it",
			inputJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test 1",
						"summary": "Summary 1",
						"messages": ["msg1"],
						"reviewer_count": 1,
						"is_false_positive": false,
						"reasoning": "Valid"
					}
				]
			}`,
			wantFiltered: 0,
			wantKept:     2,
			wantErrors:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var response EvaluationPayload
			if err := json.Unmarshal([]byte(tt.inputJSON), &response); err != nil {
				t.Fatalf("failed to parse test input: %v", err)
			}

			// Determine number of input findings based on response or test case
			numFindings := len(response.Findings)
			if tt.name == "missing finding from response keeps it" {
				numFindings = 2 // Two input findings, but only one in response
			}

			grouped := domain.GroupedFindings{
				Findings: make([]domain.FindingGroup, numFindings),
			}
			for i := 0; i < numFindings; i++ {
				grouped.Findings[i] = domain.FindingGroup{
					Title:         "Test",
					Summary:       "Summary",
					Messages:      []string{"msg"},
					ReviewerCount: 1,
				}
			}

			// Simulate the filtering logic from Apply()
			evalMap := make(map[int]FindingEvaluation)
			for _, eval := range response.Findings {
				evalMap[eval.ID] = eval
			}

			var kept []domain.FindingGroup
			var filtered []FilteredFinding
			parseErrors := 0

			for i, finding := range grouped.Findings {
				eval, ok := evalMap[i]
				if !ok {
					kept = append(kept, finding)
					parseErrors++
					continue
				}

				if eval.IsFalsePositive == nil {
					kept = append(kept, finding)
					parseErrors++
					continue
				}

				if *eval.IsFalsePositive {
					reasoning := ""
					if eval.Reasoning != nil {
						reasoning = *eval.Reasoning
					}
					filtered = append(filtered, FilteredFinding{
						Finding:   finding,
						Reasoning: reasoning,
					})
				} else {
					kept = append(kept, finding)
				}
			}

			if len(filtered) != tt.wantFiltered {
				t.Errorf("filtered count = %d, want %d", len(filtered), tt.wantFiltered)
			}
			if len(kept) != tt.wantKept {
				t.Errorf("kept count = %d, want %d", len(kept), tt.wantKept)
			}
			if parseErrors != tt.wantErrors {
				t.Errorf("parse errors = %d, want %d", parseErrors, tt.wantErrors)
			}
		})
	}
}

func TestFilteringByIsFalsePositive(t *testing.T) {
	tests := []struct {
		name            string
		isFalsePositive *bool
		wantFiltered    bool
		wantParseError  bool
	}{
		{
			name:            "true filters out",
			isFalsePositive: boolPtr(true),
			wantFiltered:    true,
			wantParseError:  false,
		},
		{
			name:            "false keeps",
			isFalsePositive: boolPtr(false),
			wantFiltered:    false,
			wantParseError:  false,
		},
		{
			name:            "nil keeps with parse error",
			isFalsePositive: nil,
			wantFiltered:    false,
			wantParseError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := FindingEvaluation{
				ID:              0,
				Title:           "Test",
				Summary:         "Summary",
				Messages:        []string{"msg"},
				ReviewerCount:   1,
				IsFalsePositive: tt.isFalsePositive,
				Reasoning:       strPtr("test reasoning"),
			}

			finding := domain.FindingGroup{
				Title:         "Test",
				Summary:       "Summary",
				Messages:      []string{"msg"},
				ReviewerCount: 1,
			}

			// Simulate the filtering decision from Apply()
			wasFiltered := false
			parseError := false

			if eval.IsFalsePositive == nil {
				// Keep and mark parse error
				parseError = true
			} else if *eval.IsFalsePositive {
				wasFiltered = true
			}

			if wasFiltered != tt.wantFiltered {
				t.Errorf("wasFiltered = %v, want %v", wasFiltered, tt.wantFiltered)
			}
			if parseError != tt.wantParseError {
				t.Errorf("parseError = %v, want %v", parseError, tt.wantParseError)
			}

			// Verify reasoning is extracted correctly
			if wasFiltered {
				reasoning := ""
				if eval.Reasoning != nil {
					reasoning = *eval.Reasoning
				}
				if reasoning != "test reasoning" {
					t.Errorf("reasoning = %q, want %q", reasoning, "test reasoning")
				}
			}

			_ = finding // Use the variable
		})
	}
}

func TestParseErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		responseJSON string
		numFindings  int
		wantKept     int
		wantFiltered int
		wantErrors   int
	}{
		{
			name:         "completely invalid JSON keeps all",
			responseJSON: `{invalid json}`,
			numFindings:  3,
			wantKept:     3,
			wantFiltered: 0,
			wantErrors:   3,
		},
		{
			name: "missing findings array keeps all",
			responseJSON: `{
				"wrong_field": []
			}`,
			numFindings:  2,
			wantKept:     2,
			wantFiltered: 0,
			wantErrors:   2,
		},
		{
			name: "partial response keeps missing",
			responseJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test",
						"summary": "Summary",
						"messages": ["msg"],
						"reviewer_count": 1,
						"is_false_positive": false,
						"reasoning": "Valid"
					}
				]
			}`,
			numFindings:  3,
			wantKept:     3,
			wantFiltered: 0,
			wantErrors:   2,
		},
		{
			name: "findings with null is_false_positive kept",
			responseJSON: `{
				"findings": [
					{
						"id": 0,
						"title": "Test",
						"summary": "Summary",
						"messages": ["msg"],
						"reviewer_count": 1,
						"is_false_positive": null,
						"reasoning": "Uncertain"
					},
					{
						"id": 1,
						"title": "Test 2",
						"summary": "Summary 2",
						"messages": ["msg2"],
						"reviewer_count": 1,
						"is_false_positive": true,
						"reasoning": "FP"
					}
				]
			}`,
			numFindings:  2,
			wantKept:     1,
			wantFiltered: 1,
			wantErrors:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test input findings
			grouped := domain.GroupedFindings{
				Findings: make([]domain.FindingGroup, tt.numFindings),
			}
			for i := 0; i < tt.numFindings; i++ {
				grouped.Findings[i] = domain.FindingGroup{
					Title:         "Test",
					Summary:       "Summary",
					Messages:      []string{"msg"},
					ReviewerCount: 1,
				}
			}

			// Simulate the parsing and filtering logic from Apply()
			var response EvaluationPayload
			parseErr := json.Unmarshal([]byte(tt.responseJSON), &response)

			var kept []domain.FindingGroup
			var filtered []FilteredFinding
			parseErrors := 0

			if parseErr != nil {
				// Complete parse failure - keep all findings
				kept = grouped.Findings
				parseErrors = len(grouped.Findings)
			} else {
				// Build map for efficient lookup
				evalMap := make(map[int]FindingEvaluation)
				for _, eval := range response.Findings {
					evalMap[eval.ID] = eval
				}

				for i, finding := range grouped.Findings {
					eval, ok := evalMap[i]
					if !ok {
						kept = append(kept, finding)
						parseErrors++
						continue
					}

					if eval.IsFalsePositive == nil {
						kept = append(kept, finding)
						parseErrors++
						continue
					}

					if *eval.IsFalsePositive {
						reasoning := ""
						if eval.Reasoning != nil {
							reasoning = *eval.Reasoning
						}
						filtered = append(filtered, FilteredFinding{
							Finding:   finding,
							Reasoning: reasoning,
						})
					} else {
						kept = append(kept, finding)
					}
				}
			}

			if len(kept) != tt.wantKept {
				t.Errorf("kept count = %d, want %d", len(kept), tt.wantKept)
			}
			if len(filtered) != tt.wantFiltered {
				t.Errorf("filtered count = %d, want %d", len(filtered), tt.wantFiltered)
			}
			if parseErrors != tt.wantErrors {
				t.Errorf("parse errors = %d, want %d", parseErrors, tt.wantErrors)
			}
		})
	}
}

// Helper functions for pointer comparisons
func boolPtr(b bool) *bool {
	return &b
}

func strPtr(s string) *string {
	return &s
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func strPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
