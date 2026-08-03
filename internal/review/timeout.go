package review

import (
	"fmt"
	"strings"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

func scaledReviewerTimeout(configured time.Duration, diffBytes int) time.Duration {
	if diffBytes <= agent.RefFileSizeThreshold {
		return configured
	}
	factor := float64(diffBytes) / float64(agent.RefFileSizeThreshold)
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
