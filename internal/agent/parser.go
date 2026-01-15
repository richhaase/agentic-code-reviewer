package agent

import (
	"bufio"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// OutputParser is responsible for parsing agent output and converting it to findings.
// Each agent implementation provides its own parser to handle its specific output format.
type OutputParser interface {
	// ReadFinding reads and parses the next finding from the output stream.
	// Returns nil when the stream is exhausted or if no finding is available.
	// Returns an error if parsing fails.
	ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error)

	// Close releases any resources held by the parser.
	Close() error
}
