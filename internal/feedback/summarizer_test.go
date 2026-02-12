package feedback

import (
	"context"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

func TestSummarizer_EmptyPRNumber(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	_, err := s.Summarize(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty PR number")
	}
}

func TestNewSummarizer(t *testing.T) {
	s := NewSummarizer("claude", true, terminal.NewLogger())
	if s == nil {
		t.Fatal("NewSummarizer returned nil")
	}
}

func TestBuildInput_DescriptionOnly(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	prCtx := &PRContext{
		Description: "This PR fixes the login bug",
	}

	result := s.buildInput(prCtx)

	if !strings.Contains(result, "## PR Description") {
		t.Error("should contain PR Description header")
	}
	if !strings.Contains(result, "This PR fixes the login bug") {
		t.Error("should contain the description text")
	}
	if strings.Contains(result, "## Comments") {
		t.Error("should not contain Comments section when no comments")
	}
}

func TestBuildInput_CommentsOnly(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	prCtx := &PRContext{
		Comments: []Comment{
			{Author: "alice", Body: "LGTM"},
			{Author: "bob", Body: "Please add tests"},
		},
	}

	result := s.buildInput(prCtx)

	if !strings.Contains(result, "(No description)") {
		t.Error("should contain '(No description)' placeholder")
	}
	if !strings.Contains(result, "## Comments") {
		t.Error("should contain Comments header")
	}
	if !strings.Contains(result, "**alice**: LGTM") {
		t.Error("should contain alice's comment")
	}
	if !strings.Contains(result, "**bob**: Please add tests") {
		t.Error("should contain bob's comment")
	}
}

func TestBuildInput_WithReplies(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	prCtx := &PRContext{
		Description: "Fix issue",
		Comments: []Comment{
			{
				Author: "alice",
				Body:   "What about edge cases?",
				Replies: []Reply{
					{Author: "bob", Body: "Good point, will add tests"},
					{Author: "alice", Body: "Thanks!"},
				},
			},
		},
	}

	result := s.buildInput(prCtx)

	if !strings.Contains(result, "**alice**: What about edge cases?") {
		t.Error("should contain the comment")
	}
	if !strings.Contains(result, "> **bob**: Good point, will add tests") {
		t.Error("should contain bob's reply with quote prefix")
	}
	if !strings.Contains(result, "> **alice**: Thanks!") {
		t.Error("should contain alice's reply with quote prefix")
	}
}

func TestBuildInput_EmptyContext(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	prCtx := &PRContext{}

	result := s.buildInput(prCtx)

	if !strings.Contains(result, "## PR Description") {
		t.Error("should always contain PR Description header")
	}
	if !strings.Contains(result, "(No description)") {
		t.Error("should contain no description placeholder")
	}
	if strings.Contains(result, "## Comments") {
		t.Error("should not contain Comments section when empty")
	}
}

func TestBuildInput_FullContext(t *testing.T) {
	s := NewSummarizer("codex", false, terminal.NewLogger())
	prCtx := &PRContext{
		Number:      "42",
		Description: "Refactor auth module to use JWT tokens",
		Comments: []Comment{
			{Author: "reviewer1", Body: "Looks good overall"},
			{
				Author: "reviewer2",
				Body:   "What about token expiry?",
				Replies: []Reply{
					{Author: "author", Body: "Added refresh token logic"},
				},
			},
			{Author: "reviewer3", Body: "LGTM 🎉"},
		},
	}

	result := s.buildInput(prCtx)

	// Check all sections present
	if !strings.Contains(result, "## PR Description") {
		t.Error("should contain PR Description header")
	}
	if !strings.Contains(result, "Refactor auth module") {
		t.Error("should contain description")
	}
	if !strings.Contains(result, "## Comments") {
		t.Error("should contain Comments header")
	}
	if !strings.Contains(result, "**reviewer1**: Looks good overall") {
		t.Error("should contain reviewer1's comment")
	}
	if !strings.Contains(result, "**reviewer2**: What about token expiry?") {
		t.Error("should contain reviewer2's comment")
	}
	if !strings.Contains(result, "> **author**: Added refresh token logic") {
		t.Error("should contain author's reply")
	}
	if !strings.Contains(result, "**reviewer3**: LGTM 🎉") {
		t.Error("should contain reviewer3's comment with emoji")
	}
}
