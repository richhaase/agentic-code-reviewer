package store

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestReviewEconomicsV1_RoundTrip(t *testing.T) {
	economics := ReviewEconomicsV1{
		SchemaVersion:     CurrentSchemaVersion,
		RunID:             "run-1",
		ReviewerCallCount: 3,
		ModelCallCount:    5,
		Duration:          12 * time.Second,
		ProviderUsage: []ProviderUsageRecordV1{
			{
				Provider: "anthropic",
				Model:    "claude-opus",
				Usage:    ProviderUsageV1{Known: true, InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200, CostUSD: 0.42},
			},
			{
				Provider: "openai",
				Model:    "gpt",
				Usage:    ProviderUsageV1{Known: false},
			},
		},
	}

	if err := economics.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	data, err := json.Marshal(economics)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReviewEconomicsV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, economics) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", decoded, economics)
	}
}

func TestProviderUsageV1_KnownZeroDistinguishableFromUnknown(t *testing.T) {
	known := ProviderUsageV1{Known: true, InputTokens: 0, OutputTokens: 0, TotalTokens: 0, CostUSD: 0}
	unknown := ProviderUsageV1{Known: false}

	if err := known.Validate(); err != nil {
		t.Fatalf("Validate(known zero usage): %v", err)
	}
	if err := unknown.Validate(); err != nil {
		t.Fatalf("Validate(unknown usage): %v", err)
	}

	knownData, err := json.Marshal(known)
	if err != nil {
		t.Fatalf("marshal known: %v", err)
	}
	unknownData, err := json.Marshal(unknown)
	if err != nil {
		t.Fatalf("marshal unknown: %v", err)
	}
	if string(knownData) == string(unknownData) {
		t.Fatalf("known-zero and unknown usage must serialize differently: %s", knownData)
	}

	var decodedKnown, decodedUnknown ProviderUsageV1
	if err := json.Unmarshal(knownData, &decodedKnown); err != nil {
		t.Fatalf("unmarshal known: %v", err)
	}
	if err := json.Unmarshal(unknownData, &decodedUnknown); err != nil {
		t.Fatalf("unmarshal unknown: %v", err)
	}
	if !decodedKnown.Known {
		t.Fatal("expected decoded known usage to remain Known=true")
	}
	if decodedUnknown.Known {
		t.Fatal("expected decoded unknown usage to remain Known=false")
	}
}

func TestProviderUsageV1_ValidateRejectsUnknownWithNonzeroMeasurements(t *testing.T) {
	tests := []struct {
		name  string
		usage ProviderUsageV1
	}{
		{name: "nonzero input tokens", usage: ProviderUsageV1{Known: false, InputTokens: 1}},
		{name: "nonzero output tokens", usage: ProviderUsageV1{Known: false, OutputTokens: 1}},
		{name: "nonzero total tokens", usage: ProviderUsageV1{Known: false, TotalTokens: 1}},
		{name: "nonzero cost", usage: ProviderUsageV1{Known: false, CostUSD: 0.01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.usage.Validate(); err == nil {
				t.Fatalf("expected an error for unknown usage with a nonzero measurement: %+v", tt.usage)
			}
		})
	}
}

func TestProviderUsageV1_ValidateRejectsNegativeKnownMeasurements(t *testing.T) {
	tests := []struct {
		name  string
		usage ProviderUsageV1
	}{
		{name: "negative input tokens", usage: ProviderUsageV1{Known: true, InputTokens: -1}},
		{name: "negative output tokens", usage: ProviderUsageV1{Known: true, OutputTokens: -1}},
		{name: "negative total tokens", usage: ProviderUsageV1{Known: true, TotalTokens: -1}},
		{name: "negative cost", usage: ProviderUsageV1{Known: true, CostUSD: -0.01}},
		{name: "NaN cost", usage: ProviderUsageV1{Known: true, CostUSD: math.NaN()}},
		{name: "infinite cost", usage: ProviderUsageV1{Known: true, CostUSD: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.usage.Validate(); err == nil {
				t.Fatalf("expected an error for known usage with a negative measurement: %+v", tt.usage)
			}
		})
	}
}

func TestReviewEconomicsV1_ValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(e *ReviewEconomicsV1)
	}{
		{name: "unsupported schema version", mutate: func(e *ReviewEconomicsV1) { e.SchemaVersion = 99 }},
		{name: "negative reviewer call count", mutate: func(e *ReviewEconomicsV1) { e.ReviewerCallCount = -1 }},
		{name: "negative model call count", mutate: func(e *ReviewEconomicsV1) { e.ModelCallCount = -1 }},
		{name: "negative duration", mutate: func(e *ReviewEconomicsV1) { e.Duration = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			economics := ReviewEconomicsV1{SchemaVersion: CurrentSchemaVersion, RunID: "run-1"}
			tt.mutate(&economics)
			if err := economics.Validate(); err == nil {
				t.Fatalf("expected an error, got nil")
			}
		})
	}
}
