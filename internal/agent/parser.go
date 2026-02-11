package agent

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

// RecoverableParseError indicates a parse error that allows continued parsing.
// The scanner can continue to the next line/item after this error.
type RecoverableParseError struct {
	Line    int
	Message string
}

func (e *RecoverableParseError) Error() string {
	return fmt.Sprintf("parse error at line %d: %s", e.Line, e.Message)
}

// IsRecoverable returns true if err is a RecoverableParseError.
func IsRecoverable(err error) bool {
	var rpe *RecoverableParseError
	return errors.As(err, &rpe)
}

// ReviewParser parses streaming review output into findings.
// Each agent implementation provides its own parser to handle its specific output format.
type ReviewParser interface {
	// ReadFinding reads and parses the next finding from the output stream.
	//
	// Returns:
	//   - (finding, nil): finding parsed successfully
	//   - (nil, nil): stream exhausted, no more findings
	//   - (nil, RecoverableParseError): parse error, caller should continue reading
	//   - (nil, error): fatal error, stop parsing
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

	// ExtractText extracts the raw response text from agent output,
	// stripping agent-specific wrappers (JSONL events, metadata envelopes, etc.)
	// and markdown code fences. The returned text is the LLM's actual response
	// content, suitable for parsing into any type (not just GroupedFindings).
	ExtractText(data []byte) (string, error)
}

// StripMarkdownCodeFence removes markdown code fences from a string.
// Handles ```json\n...\n``` or ```\n...\n``` patterns, as well as
// single-line fences like ```json{...}```.
func StripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Find end of first line (the opening fence)
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		} else {
			// Single-line: remove opening ``` and optional language identifier
			s = strings.TrimPrefix(s, "```")
			// Skip language identifier (letters before JSON content)
			for i, c := range s {
				if c == '{' || c == '[' {
					s = s[i:]
					break
				}
			}
		}
		// Remove closing fence
		if after, found := strings.CutSuffix(s, "```"); found {
			s = strings.TrimSpace(after)
		}
	}
	return s
}
