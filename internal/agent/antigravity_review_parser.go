package agent

var _ ReviewParser = (*AntigravityOutputParser)(nil)

type AntigravityOutputParser struct {
	*ClaudeOutputParser
}

func NewAntigravityOutputParser(reviewerID int) *AntigravityOutputParser {
	return &AntigravityOutputParser{
		ClaudeOutputParser: NewClaudeOutputParser(reviewerID),
	}
}
