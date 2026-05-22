package agent

// Compile-time interface check
var _ ReviewParser = (*AntigravityOutputParser)(nil)

// AntigravityOutputParser parses text output from the agy CLI.
type AntigravityOutputParser struct {
	*ClaudeOutputParser
}

// NewAntigravityOutputParser creates a new parser for agy output.
func NewAntigravityOutputParser(reviewerID int) *AntigravityOutputParser {
	return &AntigravityOutputParser{
		ClaudeOutputParser: NewClaudeOutputParser(reviewerID),
	}
}
