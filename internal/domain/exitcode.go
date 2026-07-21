package domain

type ExitCode int

const (
	ExitNoFindings ExitCode = 0

	ExitFindings ExitCode = 1

	ExitError ExitCode = 2

	ExitInterrupted ExitCode = 130
)

func (e ExitCode) Int() int {
	return int(e)
}
