// Package runner provides the review execution engine.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

// maxFindingPreviewLength is the maximum characters shown for a finding in
// verbose output. Longer findings are truncated with "..." to prevent
// excessive terminal output while preserving enough context for debugging.
const maxFindingPreviewLength = 120

// Config holds the runner configuration.
type Config struct {
	Reviewers   int
	Concurrency int
	BaseRef     string
	Timeout     time.Duration
	Retries     int
	Verbose     bool
	WorkDir     string
	Guidance    string
	UseRefFile  bool
	Diff        string // Pre-computed git diff (generated once, shared across reviewers)
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

		// Skip retries for auth failures — retrying won't help
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
		BaseRef:    r.config.BaseRef,
		Timeout:    r.config.Timeout,
		WorkDir:    r.config.WorkDir,
		Verbose:    r.config.Verbose,
		Guidance:   r.config.Guidance,
		ReviewerID: strconv.Itoa(reviewerID),
		UseRefFile: r.config.UseRefFile,
		Diff:       r.config.Diff,
	}

	// Execute the review
	execResult, err := selectedAgent.ExecuteReview(timeoutCtx, reviewConfig)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}
	// Ensure cleanup on all exit paths
	defer func() {
		if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
			r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
		}
	}()

	// Create parser for this agent's output
	parser, err := agent.NewReviewParser(selectedAgent.Name(), reviewerID)
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	// Configure scanner
	scanner := bufio.NewScanner(execResult)
	agent.ConfigureScanner(scanner)

	// Parse output
	for {
		// Check for timeout
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
				// Recoverable error - log and continue parsing
				// Parse error count tracked by parser.ParseErrors()
				if r.verbose() {
					r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: %v", reviewerID, err)
				}
				continue
			}
			// Fatal error - break to avoid infinite loop
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
			if len(text) > maxFindingPreviewLength {
				text = text[:maxFindingPreviewLength] + "..."
			}
			r.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
				terminal.Color(terminal.Dim), reviewerID, terminal.Color(terminal.Reset),
				terminal.Color(terminal.Dim), text, terminal.Color(terminal.Reset))
		}
	}

	// Capture parse errors tracked by the parser
	result.ParseErrors += parser.ParseErrors()

	// Close to wait for process and get exit code
	// (defer will be a no-op due to sync.Once in ExecutionResult)
	if closeErr := execResult.Close(); closeErr != nil && r.verbose() {
		r.logger.Logf(terminal.StyleWarning, "Reviewer #%d: close error (non-fatal): %v", reviewerID, closeErr)
	}
	result.ExitCode = execResult.ExitCode()

	// Detect auth failure from exit code and stderr
	if result.ExitCode != 0 {
		result.AuthFailed = agent.IsAuthFailure(selectedAgent.Name(), result.ExitCode, execResult.Stderr())
	}

	// Record duration after process fully exits
	result.Duration = time.Since(start)

	// Check for timeout after parsing — timeout takes precedence over auth failure
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

// CollectFindings collects all findings from results.
func CollectFindings(results []domain.ReviewerResult) []domain.Finding {
	var findings []domain.Finding
	for _, r := range results {
		findings = append(findings, r.Findings...)
	}
	return findings
}
