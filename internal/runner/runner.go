package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const maxFindingPreviewLength = 120

const maxAuthOutputCapture = 1 << 20

type cappedOutputCapture struct {
	buf bytes.Buffer
	max int
}

func newCappedOutputCapture(max int) *cappedOutputCapture {
	return &cappedOutputCapture{max: max}
}

func (c *cappedOutputCapture) Write(p []byte) (int, error) {
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedOutputCapture) String() string {
	return c.buf.String()
}

type Config struct {
	Reviewers       int
	Concurrency     int
	BaseRef         string
	Timeout         time.Duration
	Retries         int
	Verbose         bool
	WorkDir         string
	Guidance        string
	UseRefFile      bool
	Diff            string
	DiffPrecomputed bool
}

type Runner struct {
	config    Config
	agents    []agent.Agent
	logger    *terminal.Logger
	completed *atomic.Int32
}

func New(config Config, agents []agent.Agent, logger *terminal.Logger) (*Runner, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("at least one agent is required")
	}
	return &Runner{
		config:    config,
		agents:    agents,
		logger:    logger,
		completed: &atomic.Int32{},
	}, nil
}

func (r *Runner) Run(ctx context.Context) ([]domain.ReviewerResult, time.Duration, error) {
	spinner := terminal.NewSpinner(r.config.Reviewers)
	r.completed = spinner.Completed()

	spinnerCtx, spinnerCancel := context.WithCancel(context.Background())
	spinnerDone := make(chan struct{})
	go func() {
		spinner.Run(spinnerCtx)
		close(spinnerDone)
	}()

	start := time.Now()

	resultCh := make(chan domain.ReviewerResult, r.config.Reviewers)

	concurrency := r.config.Concurrency
	if concurrency <= 0 {
		concurrency = r.config.Reviewers
	}

	sem := make(chan struct{}, concurrency)

	for i := 1; i <= r.config.Reviewers; i++ {
		go func(id int) {

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				resultCh <- domain.ReviewerResult{
					ReviewerID: id,
					ExitCode:   -1,
				}
				return
			}

			result := r.runReviewerWithRetry(ctx, id)

			<-sem

			r.completed.Add(1)
			resultCh <- result
		}(i)
	}

	results := make([]domain.ReviewerResult, 0, r.config.Reviewers)
	for i := 0; i < r.config.Reviewers; i++ {
		select {
		case result := <-resultCh:
			results = append(results, result)
		case <-ctx.Done():
			spinnerCancel()
			<-spinnerDone
			return nil, time.Since(start), ctx.Err()
		}
	}

	spinnerCancel()
	<-spinnerDone

	return results, time.Since(start), nil
}

func (r *Runner) runReviewerWithRetry(ctx context.Context, reviewerID int) domain.ReviewerResult {
	var result domain.ReviewerResult

	for attempt := 0; attempt <= r.config.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return domain.ReviewerResult{
				ReviewerID: reviewerID,
				ExitCode:   -1,
			}
		default:
		}

		result = r.runReviewer(ctx, reviewerID)

		if result.ExitCode == 0 {
			return result
		}

		if result.AuthFailed {
			r.logger.Logf(terminal.StyleError, "Reviewer #%d (%s) authentication failed: %s",
				reviewerID, result.AgentName, agent.AuthHint(result.AgentName))
			return result
		}

		if attempt < r.config.Retries {
			base := time.Duration(1<<attempt) * time.Second
			jitter := time.Duration(rand.Int64N(int64(base / 2)))
			delay := base + jitter
			reason := "failed"
			if result.TimedOut {
				reason = "timed out"
			}
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d %s (exit %d), retry %d/%d in %v",
				reviewerID, reason, result.ExitCode, attempt+1, r.config.Retries, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return result
			}
		}
	}

	return result
}

func (r *Runner) runReviewer(ctx context.Context, reviewerID int) domain.ReviewerResult {
	start := time.Now()

	selectedAgent := agent.AgentForReviewer(r.agents, reviewerID)
	if selectedAgent == nil {

		return domain.ReviewerResult{
			ReviewerID: reviewerID,
			ExitCode:   -1,
			Duration:   time.Since(start),
		}
	}

	result := domain.ReviewerResult{
		ReviewerID: reviewerID,
		AgentName:  selectedAgent.Name(),
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	reviewConfig := &agent.ReviewConfig{
		BaseRef:         r.config.BaseRef,
		Timeout:         r.config.Timeout,
		WorkDir:         r.config.WorkDir,
		Verbose:         r.config.Verbose,
		Guidance:        r.config.Guidance,
		ReviewerID:      strconv.Itoa(reviewerID),
		UseRefFile:      r.config.UseRefFile,
		Diff:            r.config.Diff,
		DiffPrecomputed: r.config.DiffPrecomputed,
	}

	execResult, err := selectedAgent.ExecuteReview(timeoutCtx, reviewConfig)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	defer func() {
		if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
		}
	}()

	parser, err := agent.NewReviewParser(selectedAgent.Name(), reviewerID)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	stdoutCapture := newCappedOutputCapture(maxAuthOutputCapture)
	scanner := bufio.NewScanner(io.TeeReader(execResult, stdoutCapture))
	agent.ConfigureScanner(scanner)

	for {

		if timeoutCtx.Err() == context.DeadlineExceeded {
			result.ParseErrors += parser.ParseErrors()
			result.TimedOut = true
			result.ExitCode = -1
			result.Duration = time.Since(start)
			return result
		}

		finding, err := parser.ReadFinding(scanner)
		if err != nil {
			if agent.IsRecoverable(err) {

				if r.verbose() {
					r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: %v", reviewerID, err)
				}
				continue
			}

			result.ParseErrors++
			break
		}

		if finding == nil {

			break
		}

		result.Findings = append(result.Findings, *finding)

		if r.verbose() {
			text := finding.Text
			if len(text) > maxFindingPreviewLength {
				text = text[:maxFindingPreviewLength] + "..."
			}
			r.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
				terminal.Color(terminal.Dim), reviewerID, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), text, terminal.Color(terminal.Reset))
		}
	}

	result.ParseErrors += parser.ParseErrors()

	if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
		r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
	}
	result.ExitCode = execResult.ExitCode()

	if result.ExitCode != 0 {
		result.AuthFailed = agent.IsAuthFailure(selectedAgent.Name(), result.ExitCode, execResult.Stderr(), stdoutCapture.String())
		if result.AuthFailed {
			result.Findings = nil
		}
	}

	result.Duration = time.Since(start)

	if timeoutCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.AuthFailed = false
		result.ExitCode = -1
		return result
	}

	return result
}

func (r *Runner) verbose() bool {
	return r.config.Verbose
}

func BuildStats(results []domain.ReviewerResult, totalReviewers int, wallClock time.Duration) domain.ReviewStats {
	stats := domain.ReviewStats{
		TotalReviewers:     totalReviewers,
		ReviewerDurations:  make(map[int]time.Duration),
		ReviewerAgentNames: make(map[int]string),
		WallClockDuration:  wallClock,
	}

	for _, r := range results {
		stats.ReviewerDurations[r.ReviewerID] = r.Duration
		stats.ReviewerAgentNames[r.ReviewerID] = r.AgentName
		stats.ParseErrors += r.ParseErrors

		if r.TimedOut {
			stats.TimedOutReviewers = append(stats.TimedOutReviewers, r.ReviewerID)
		} else if r.AuthFailed {
			stats.AuthFailedReviewers = append(stats.AuthFailedReviewers, r.ReviewerID)
		} else if r.ExitCode != 0 {
			stats.FailedReviewers = append(stats.FailedReviewers, r.ReviewerID)
		} else {
			stats.SuccessfulReviewers++
		}
	}

	return stats
}

func CollectFindings(results []domain.ReviewerResult) []domain.Finding {
	var findings []domain.Finding
	for _, r := range results {
		findings = append(findings, r.Findings...)
	}
	return findings
}
