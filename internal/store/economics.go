package store

import (
	"fmt"
	"time"
)

type ProviderUsageV1 struct {
	Known        bool    `json:"known"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

func (u ProviderUsageV1) Validate() error {
	if u.Known {
		return nil
	}
	if u.InputTokens != 0 || u.OutputTokens != 0 || u.TotalTokens != 0 || u.CostUSD != 0 {
		return fmt.Errorf("provider usage marked unknown must not carry nonzero measurements")
	}
	return nil
}

type ProviderUsageRecordV1 struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Usage    ProviderUsageV1 `json:"usage"`
}

func (r ProviderUsageRecordV1) Validate() error {
	if err := validateNonEmpty("provider usage record provider", r.Provider); err != nil {
		return err
	}
	return r.Usage.Validate()
}

type ReviewEconomicsV1 struct {
	SchemaVersion     int                     `json:"schema_version"`
	RunID             string                  `json:"run_id"`
	ReviewerCallCount int                     `json:"reviewer_call_count"`
	ModelCallCount    int                     `json:"model_call_count"`
	Duration          time.Duration           `json:"duration"`
	ProviderUsage     []ProviderUsageRecordV1 `json:"provider_usage,omitempty"`
}

func (e ReviewEconomicsV1) Validate() error {
	if err := validateSchemaVersion("review economics", e.SchemaVersion); err != nil {
		return err
	}
	if err := validateNonEmpty("review economics run_id", e.RunID); err != nil {
		return err
	}
	if e.ReviewerCallCount < 0 {
		return fmt.Errorf("review economics reviewer_call_count must not be negative")
	}
	if e.ModelCallCount < 0 {
		return fmt.Errorf("review economics model_call_count must not be negative")
	}
	if e.Duration < 0 {
		return fmt.Errorf("review economics duration must not be negative")
	}
	for i, usage := range e.ProviderUsage {
		if err := usage.Validate(); err != nil {
			return fmt.Errorf("provider usage %d: %w", i, err)
		}
	}
	return nil
}
