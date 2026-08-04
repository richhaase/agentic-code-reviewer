package review

import (
	"testing"
	"time"
)

func TestScaledReviewerTimeout(t *testing.T) {
	tests := []struct {
		name       string
		configured time.Duration
		diffBytes  int
		want       time.Duration
	}{
		{name: "empty diff keeps configured timeout", configured: 15 * time.Minute, diffBytes: 0, want: 15 * time.Minute},
		{name: "below threshold keeps configured timeout", configured: 15 * time.Minute, diffBytes: reviewerTimeoutScaleThreshold / 2, want: 15 * time.Minute},
		{name: "at threshold keeps configured timeout", configured: 15 * time.Minute, diffBytes: reviewerTimeoutScaleThreshold, want: 15 * time.Minute},
		{name: "half over threshold scales by 1.5", configured: 10 * time.Minute, diffBytes: reviewerTimeoutScaleThreshold * 3 / 2, want: 15 * time.Minute},
		{name: "double threshold doubles timeout", configured: 10 * time.Minute, diffBytes: 2 * reviewerTimeoutScaleThreshold, want: 20 * time.Minute},
		{name: "655KB diff scales 15m to 98m15s", configured: 15 * time.Minute, diffBytes: 655 * 1024, want: 5895 * time.Second},
		{name: "scale is capped", configured: 10 * time.Minute, diffBytes: 50 * 1024 * 1024, want: 100 * time.Minute},
		{name: "rounding boundary scales exactly", configured: maxReviewerTimeout / 2, diffBytes: 2 * reviewerTimeoutScaleThreshold, want: maxReviewerTimeout - 1},
		{name: "overflow saturates", configured: maxReviewerTimeout, diffBytes: 2 * reviewerTimeoutScaleThreshold, want: maxReviewerTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaledReviewerTimeout(tt.configured, tt.diffBytes)
			if got != tt.want {
				t.Errorf("scaledReviewerTimeout(%v, %d) = %v, want %v", tt.configured, tt.diffBytes, got, tt.want)
			}
		})
	}
}

func TestReviewerTimeoutScaledMessage(t *testing.T) {
	tests := []struct {
		name       string
		diffBytes  int
		configured time.Duration
		effective  time.Duration
		want       string
	}{
		{
			name:       "minutes to mixed units",
			diffBytes:  655 * 1024,
			configured: 15 * time.Minute,
			effective:  5895 * time.Second,
			want:       "Diff is 655KB; scaling reviewer timeout 15m → 1h38m15s",
		},
		{
			name:       "whole hour trims zero units",
			diffBytes:  200 * 1024,
			configured: 30 * time.Minute,
			effective:  time.Hour,
			want:       "Diff is 200KB; scaling reviewer timeout 30m → 1h",
		},
		{
			name:       "fractional second rounds to whole seconds",
			diffBytes:  150 * 1024,
			configured: time.Minute,
			effective:  90*time.Second + 400*time.Millisecond,
			want:       "Diff is 150KB; scaling reviewer timeout 1m → 1m30s",
		},
		{
			name:       "subsecond timeouts retain precision",
			diffBytes:  150 * 1024,
			configured: 500 * time.Millisecond,
			effective:  750 * time.Millisecond,
			want:       "Diff is 150KB; scaling reviewer timeout 500ms → 750ms",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reviewerTimeoutScaledMessage(tt.diffBytes, tt.configured, tt.effective)
			if got != tt.want {
				t.Errorf("reviewerTimeoutScaledMessage(%d, %v, %v) = %q, want %q", tt.diffBytes, tt.configured, tt.effective, got, tt.want)
			}
		})
	}
}
