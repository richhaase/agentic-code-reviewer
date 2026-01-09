// Package domain provides core types for the code reviewer.
package domain

// ExitCode represents the exit status of the reviewer.
type ExitCode int

const (
	// ExitNoFindings indicates successful review with no issues found.
	ExitNoFindings ExitCode = 0
	// ExitFindings indicates successful review with issues found.
	ExitFindings ExitCode = 1
	// ExitError indicates the review failed due to an error.
	ExitError ExitCode = 2
	// ExitInterrupted indicates the review was interrupted by a signal.
	ExitInterrupted ExitCode = 130
)

// Int returns the exit code as an int for use with os.Exit.
func (e ExitCode) Int() int {
	return int(e)
}
