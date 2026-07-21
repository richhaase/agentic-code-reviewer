package agent

import "time"

type ReviewConfig struct {
	BaseRef string

	Timeout time.Duration

	WorkDir string

	Verbose bool

	Guidance string

	ReviewerID string

	UseRefFile bool

	Diff string

	DiffPrecomputed bool
}

type SummaryConfig struct {
	Prompt  string
	Input   []byte
	WorkDir string
}
