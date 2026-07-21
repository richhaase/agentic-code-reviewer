package agent

import "strings"

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

func IsNonFindingText(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range nonFindingPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
