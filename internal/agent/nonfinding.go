package agent

import "strings"

// nonFindingPhrases are common phrases indicating "no issues found" responses
// rather than actual code review findings.
var nonFindingPhrases = []string{
	"no issues",
	"no findings",
	"no bugs",
	"no problems",
	"looks good",
	"code looks clean",
	"code looks correct",
	"review complete",
}

// IsNonFindingText returns true if text appears to be a "no issues found"
// response rather than an actual finding.
func IsNonFindingText(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range nonFindingPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
