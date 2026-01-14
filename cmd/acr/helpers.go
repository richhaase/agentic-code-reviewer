package main

import (
	"fmt"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
)

// filterFindingsByIndices returns findings at the specified indices.
func filterFindingsByIndices(findings []domain.FindingGroup, indices []int) []domain.FindingGroup {
	indexSet := make(map[int]bool, len(indices))
	for _, i := range indices {
		indexSet[i] = true
	}

	result := make([]domain.FindingGroup, 0, len(indices))
	for i, f := range findings {
		if indexSet[i] {
			result = append(result, f)
		}
	}
	return result
}

// exitCodeError is a wrapper type for returning exit codes via error interface.
type exitCodeError struct {
	code domain.ExitCode
}

func (e exitCodeError) Error() string {
	switch e.code {
	case domain.ExitFindings:
		return "findings were reported"
	case domain.ExitError:
		return "review failed with error"
	case domain.ExitInterrupted:
		return "review was interrupted"
	default:
		return fmt.Sprintf("exit code %d", e.code)
	}
}

func exitCode(code domain.ExitCode) error {
	if code == domain.ExitNoFindings {
		return nil
	}
	return exitCodeError{code: code}
}
