package agent

import (
	"bufio"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// ReviewParser parses streaming review output into findings.
// Each agent implementation provides its own parser to handle its specific output format.
type ReviewParser interface {
	// ReadFinding reads and parses the next finding from the output stream.
	// Returns nil when the stream is exhausted or if no finding is available.
	// Returns an error if parsing fails.
	ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error)

	// ParseErrors returns the number of recoverable parse errors encountered.
	// This count includes lines that failed to parse but were skipped to continue processing.
	ParseErrors() int
}

// SummaryParser parses summary output into grouped findings.
// Each agent implementation provides its own parser to handle its specific output format.
type SummaryParser interface {
	// Parse parses the complete summary output and returns grouped findings.
	// The data parameter contains the raw output from ExecuteSummary.
	// Returns an error if parsing fails.
	Parse(data []byte) (*domain.GroupedFindings, error)
}
