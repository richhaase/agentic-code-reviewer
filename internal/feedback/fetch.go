// Package feedback provides PR feedback summarization for false positive filtering.
package feedback

// PRContext holds the PR description and all comments.
type PRContext struct {
	Number      string
	Description string
	Comments    []Comment
}

// Comment represents a PR comment with its replies.
type Comment struct {
	Author     string
	Body       string
	IsResolved bool
	Replies    []Reply
}

// Reply represents a reply to a comment.
type Reply struct {
	Author string
	Body   string
}

// HasContent returns true if the context has any content worth summarizing.
func (p *PRContext) HasContent() bool {
	return p.Description != "" || len(p.Comments) > 0
}
