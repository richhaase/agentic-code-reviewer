package review

import (
	"fmt"
	"strings"
	"time"
)

const reviewerTimeoutScaleThreshold = 100 * 1024
const maxReviewerTimeoutScale = 10
const maxReviewerTimeout = time.Duration(1<<63 - 1)

func scaledReviewerTimeout(configured time.Duration, diffBytes int) time.Duration {
	if diffBytes <= reviewerTimeoutScaleThreshold {
		return configured
	}
	factor := float64(diffBytes) / float64(reviewerTimeoutScaleThreshold)
	if factor > maxReviewerTimeoutScale {
		factor = maxReviewerTimeoutScale
	}
	if float64(configured) > float64(maxReviewerTimeout)/factor {
		return maxReviewerTimeout
	}
	return time.Duration(float64(configured) * factor)
}

func reviewerTimeoutScaledMessage(diffBytes int, configured, effective time.Duration) string {
	return fmt.Sprintf("Diff is %dKB; scaling reviewer timeout %s → %s",
		diffBytes/1024, formatReviewerTimeout(configured), formatReviewerTimeout(effective))
}

func formatReviewerTimeout(d time.Duration) string {
	rendered := d.Round(time.Second).String()
	if strings.HasSuffix(rendered, "m0s") {
		rendered = strings.TrimSuffix(rendered, "0s")
	}
	if strings.HasSuffix(rendered, "h0m") {
		rendered = strings.TrimSuffix(rendered, "0m")
	}
	return rendered
}
