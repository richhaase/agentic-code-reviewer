package desk

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

const DefaultPullRequestHost = "github.com"

var pullRequestRefPattern = regexp.MustCompile(`^([^/#]+)/([^/#]+)#([0-9]+)$`)

func ParsePullRequestRef(ref string) (store.PullRequestKeyV1, error) {
	matches := pullRequestRefPattern.FindStringSubmatch(ref)
	if matches == nil {
		return store.PullRequestKeyV1{}, fmt.Errorf("pull request reference %q must have the form owner/repo#number", ref)
	}
	number, err := strconv.Atoi(matches[3])
	if err != nil {
		return store.PullRequestKeyV1{}, fmt.Errorf("pull request reference %q has an invalid number: %w", ref, err)
	}
	key := store.PullRequestKeyV1{
		Host:       DefaultPullRequestHost,
		Owner:      matches[1],
		Repository: matches[2],
		Number:     number,
	}
	if err := key.Validate(); err != nil {
		return store.PullRequestKeyV1{}, fmt.Errorf("pull request reference %q is invalid: %w", ref, err)
	}
	return key, nil
}
