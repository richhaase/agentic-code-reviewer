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
	scaledDiffBytes := diffBytes
	if scaledDiffBytes > reviewerTimeoutScaleThreshold*maxReviewerTimeoutScale {
		scaledDiffBytes = reviewerTimeoutScaleThreshold * maxReviewerTimeoutScale
	}
	whole := time.Duration(scaledDiffBytes / reviewerTimeoutScaleThreshold)
	remainder := time.Duration(scaledDiffBytes % reviewerTimeoutScaleThreshold)
	if configured > maxReviewerTimeout/whole {
		return maxReviewerTimeout
	}
	fraction := configured/time.Duration(reviewerTimeoutScaleThreshold)*remainder +
		configured%time.Duration(reviewerTimeoutScaleThreshold)*remainder/time.Duration(reviewerTimeoutScaleThreshold)
	scaled := configured * whole
	if scaled > maxReviewerTimeout-fraction {
		return maxReviewerTimeout
	}
	return scaled + fraction
}

func reviewerTimeoutScaledMessage(diffBytes int, configured, effective time.Duration) string {
	return fmt.Sprintf("Diff is %dKB; scaling reviewer timeout %s → %s",
		diffBytes/1024, formatReviewerTimeout(configured), formatReviewerTimeout(effective))
}

func formatReviewerTimeout(d time.Duration) string {
	renderedDuration := d
	if d >= time.Second || d <= -time.Second {
		renderedDuration = d.Round(time.Second)
	}
	rendered := renderedDuration.String()
	if strings.HasSuffix(rendered, "m0s") {
		rendered = strings.TrimSuffix(rendered, "0s")
	}
	if strings.HasSuffix(rendered, "h0m") {
		rendered = strings.TrimSuffix(rendered, "0m")
	}
	return rendered
}
