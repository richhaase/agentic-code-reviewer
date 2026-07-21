package agent

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type RecoverableParseError struct {
	Line    int
	Message string
}

func (e *RecoverableParseError) Error() string {
	return fmt.Sprintf("parse error at line %d: %s", e.Line, e.Message)
}

func IsRecoverable(err error) bool {
	var rpe *RecoverableParseError
	return errors.As(err, &rpe)
}

type ReviewParser interface {
	ReadFinding(scanner *bufio.Scanner) (*domain.Finding, error)

	ParseErrors() int
}

type SummaryParser interface {
	Parse(data []byte) (*domain.GroupedFindings, error)

	ExtractText(data []byte) (string, error)
}

func ExtractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)

	stripped := StripMarkdownCodeFence(s)

	for _, candidate := range []string{stripped, s} {
		candidate = strings.TrimSpace(candidate)
		if len(candidate) == 0 {
			continue
		}

		if candidate[0] == '{' || candidate[0] == '[' {
			if result, ok := extractBalanced(candidate, 0); ok {
				return result, nil
			}
		}
	}

	braceIdx := strings.Index(s, "{")
	bracketIdx := strings.Index(s, "[")

	idx := braceIdx
	if idx == -1 || (bracketIdx != -1 && bracketIdx < idx) {
		idx = bracketIdx
	}

	if idx != -1 {
		if result, ok := extractBalanced(s, idx); ok {
			return result, nil
		}
	}

	return "", fmt.Errorf("no JSON object or array found in text")
}

func extractBalanced(s string, idx int) (string, bool) {
	open := s[idx]
	var close byte
	switch open {
	case '{':
		close = '}'
	case '[':
		close = ']'
	default:
		return "", false
	}

	depth := 0
	inString := false
	escape := false
	for i := idx; i < len(s); i++ {
		if escape {
			escape = false
			continue
		}
		ch := s[i]
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[idx : i+1], true
			}
		}
	}
	return "", false
}

func StripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {

		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		} else {

			s = strings.TrimPrefix(s, "```")

			for i, c := range s {
				if c == '{' || c == '[' {
					s = s[i:]
					break
				}
			}
		}

		if after, found := strings.CutSuffix(s, "```"); found {
			s = strings.TrimSpace(after)
		}
	}
	return s
}
