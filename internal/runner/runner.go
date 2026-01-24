// Package runner provides the review execution engine.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// Config holds the runner configuration.
type Config struct {
	Reviewers    int
	Concurrency  int
	BaseRef      string
	Timeout      time.Duration
	Retries      int
	FetchRemote  bool
	Verbose      bool
	WorkDir      string
	CustomPrompt string
	UseRefFile   bool
}

// Runner executes parallel code reviews.
type Runner struct {
	config    Config
	agents    []agent.Agent
	logger    *terminal.Logger
	completed *atomic.Int32
}

// New creates a new runner with one or more agents for round-robin assignment.
// Returns an error if agents slice is empty.
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

// Run executes the review process and returns the results.
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

	// Create result channel
	resultCh := make(chan domain.ReviewerResult, r.config.Reviewers)

	// Determine concurrency limit (default to reviewers if not set)
	concurrency := r.config.Concurrency
	if concurrency <= 0 {
		concurrency = r.config.Reviewers
	}

	// Create semaphore to limit concurrent reviewers
	sem := make(chan struct{}, concurrency)

	// Launch reviewers
	for i := 1; i <= r.config.Reviewers; i++ {
		go func(id int) {
			// Acquire semaphore
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

			// Release semaphore
			<-sem

			r.completed.Add(1)
			resultCh <- result
		}(i)
	}

	// Collect results
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

		if attempt < r.config.Retries {
			delay := time.Duration(1<<attempt) * time.Second
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

	// Select agent via round-robin
	selectedAgent := agent.AgentForReviewer(r.agents, reviewerID)
	if selectedAgent == nil {
		// Should never happen: New() validates non-empty agents, IDs start at 1
		// Defensive check prevents panic if invariants change
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

	// Create review configuration
	reviewConfig := &agent.ReviewConfig{
		BaseRef:      r.config.BaseRef,
		Timeout:      r.config.Timeout,
		WorkDir:      r.config.WorkDir,
		Verbose:      r.config.Verbose,
		CustomPrompt: r.config.CustomPrompt,
		ReviewerID:   strconv.Itoa(reviewerID),
		FetchRemote:  r.config.FetchRemote,
		UseRefFile:   r.config.UseRefFile,
	}

	// Execute the review
	reader, err := selectedAgent.ExecuteReview(timeoutCtx, reviewConfig)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	// closeReader closes the reader and returns the process exit code if available
	closeReader := func() int {
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		if exitCoder, ok := reader.(agent.ExitCoder); ok {
			return exitCoder.ExitCode()
		}
		return 0
	}

	// Create parser for this agent's output
	parser, err := agent.NewReviewParser(selectedAgent.Name(), reviewerID)
	if err != nil {
		closeReader()
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	// Configure scanner
	scanner := bufio.NewScanner(reader)
	agent.ConfigureScanner(scanner)

	// Parse output
	for {
		// Check for timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			closeReader()
			result.TimedOut = true
			result.ExitCode = -1
			result.Duration = time.Since(start)
			return result
		}

		finding, err := parser.ReadFinding(scanner)
		if err != nil {
			// Scanner error is permanent - break to avoid infinite loop
			result.ParseErrors++
			break
		}

		if finding == nil {
			// End of stream
			break
		}

		result.Findings = append(result.Findings, *finding)

		if r.verbose() {
			text := finding.Text
			if len(text) > 120 {
				text = text[:120] + "..."
			}
			r.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
				terminal.Color(terminal.Dim), reviewerID, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), text, terminal.Color(terminal.Reset))
		}
	}

	// Capture parse errors tracked by the parser
	result.ParseErrors += parser.ParseErrors()

	// Close reader and capture exit code
	exitCode := closeReader()

	// Record duration after process fully exits
	result.Duration = time.Since(start)

	// Check for timeout after parsing
	if timeoutCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result
	}

	// Use the actual agent exit code
	result.ExitCode = exitCode

	return result
}

func (r *Runner) verbose() bool {
	return r.config.Verbose
}

// BuildStats builds review statistics from results.
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
		} else if r.ExitCode != 0 {
			stats.FailedReviewers = append(stats.FailedReviewers, r.ReviewerID)
		} else {
			stats.SuccessfulReviewers++
		}
	}

	return stats
}

// CollectFindings collects all findings from results.
func CollectFindings(results []domain.ReviewerResult) []domain.Finding {
	var findings []domain.Finding
	for _, r := range results {
		findings = append(findings, r.Findings...)
	}
	return findings
}
