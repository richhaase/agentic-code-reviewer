package desk

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

const DefaultPullRequestHost = "github.com"

const pullRequestRefFormat = "pull request reference %q must have the form [host/]owner/repo#number"

func ParsePullRequestRef(ref string) (store.PullRequestKeyV1, error) {
	hashIndex := strings.LastIndex(ref, "#")
	if hashIndex < 0 {
		return store.PullRequestKeyV1{}, fmt.Errorf(pullRequestRefFormat, ref)
	}
	number, err := strconv.Atoi(ref[hashIndex+1:])
	if err != nil {
		return store.PullRequestKeyV1{}, fmt.Errorf("pull request reference %q has an invalid number: %w", ref, err)
	}

	segments := strings.Split(ref[:hashIndex], "/")
	var key store.PullRequestKeyV1
	switch len(segments) {
	case 2:
		key = store.PullRequestKeyV1{Host: DefaultPullRequestHost, Owner: segments[0], Repository: segments[1], Number: number}
	case 3:
		key = store.PullRequestKeyV1{Host: segments[0], Owner: segments[1], Repository: segments[2], Number: number}
	default:
		return store.PullRequestKeyV1{}, fmt.Errorf(pullRequestRefFormat, ref)
	}
	if err := key.Validate(); err != nil {
		return store.PullRequestKeyV1{}, fmt.Errorf("pull request reference %q is invalid: %w", ref, err)
	}
	return key, nil
}
