// Package runner provides the review execution engine.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anthropics/agentic-code-reviewer/internal/domain"
	"github.com/anthropics/agentic-code-reviewer/internal/terminal"
)

// Config holds the runner configuration.
type Config struct {
	Reviewers   int
	Concurrency int
	BaseRef     string
	Timeout     time.Duration
	Retries     int
	Verbose     bool
	WorkDir     string
}

// Runner executes parallel code reviews.
type Runner struct {
	config    Config
	logger    *terminal.Logger
	completed *atomic.Int32
}

// New creates a new runner.
func New(config Config, logger *terminal.Logger) *Runner {
	return &Runner{
		config:    config,
		logger:    logger,
		completed: &atomic.Int32{},
	}
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
	result := domain.ReviewerResult{
		ReviewerID: reviewerID,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "codex", "exec", "--json", "--color", "never", "review", "--base", r.config.BaseRef) //nolint:gosec // BaseRef is validated CLI input
	if r.config.WorkDir != "" {
		cmd.Dir = r.config.WorkDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	if err := cmd.Start(); err != nil {
		result.ExitCode = -1
		result.Duration = time.Since(start)
		return result
	}

	// Read output
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 100*1024*1024) // 100MB max line

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event struct {
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}

		if err := json.Unmarshal([]byte(line), &event); err != nil {
			result.ParseErrors++
			continue
		}

		if event.Item.Type == "agent_message" && event.Item.Text != "" {
			result.Findings = append(result.Findings, domain.Finding{
				Text:       event.Item.Text,
				ReviewerID: reviewerID,
			})

			if r.verbose() {
				text := event.Item.Text
				if len(text) > 120 {
					text = text[:120] + "..."
				}
				r.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
					terminal.Color(terminal.Dim), reviewerID, terminal.Color(terminal.Reset),
					terminal.Color(terminal.Dim), text, terminal.Color(terminal.Reset))
			}
		}
	}

	err = cmd.Wait()
	result.Duration = time.Since(start)

	if timeoutCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		// Kill process group
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return result
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

func (r *Runner) verbose() bool {
	return r.config.Verbose
}

// BuildStats builds review statistics from results.
func BuildStats(results []domain.ReviewerResult, totalReviewers int, wallClock time.Duration) domain.ReviewStats {
	stats := domain.ReviewStats{
		TotalReviewers:    totalReviewers,
		ReviewerDurations: make(map[int]time.Duration),
		WallClockDuration: wallClock,
	}

	for _, r := range results {
		stats.ReviewerDurations[r.ReviewerID] = r.Duration
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
