package main

import (
	"context"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
	reviewpkg "github.com/richhaase/agentic-code-reviewer/internal/review"
	"github.com/richhaase/agentic-code-reviewer/internal/terminal"
)

const reviewerOutputPreviewLength = 120

type runningReviewSpinner struct {
	cancel    context.CancelFunc
	done      chan struct{}
	completed func()
}

func (s *runningReviewSpinner) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel = nil
}

type cliReviewEvents struct {
	opts              ReviewOpts
	logger            *terminal.Logger
	reviewers         *runningReviewSpinner
	summarization     *runningReviewSpinner
	falsePositivePass *runningReviewSpinner
}

func newCLIReviewEvents(opts ReviewOpts, logger *terminal.Logger) *cliReviewEvents {
	return &cliReviewEvents{opts: opts, logger: logger}
}

func (e *cliReviewEvents) HandleReviewEvent(event reviewpkg.Event) {
	switch event.Kind {
	case reviewpkg.EventPhaseStarted:
		e.startPhase(event.Phase)
	case reviewpkg.EventPhaseCompleted:
		e.completePhase(event)
	case reviewpkg.EventReviewerOutput:
		e.reviewerOutput(event)
	case reviewpkg.EventReviewerRetrying:
		e.logger.Logf(terminal.StyleWarning, "Reviewer #%d %s", event.ReviewerID, event.Message)
	case reviewpkg.EventReviewerCompleted:
		if e.reviewers != nil && e.reviewers.completed != nil {
			e.reviewers.completed()
		}
	case reviewpkg.EventWarning:
		e.warning(event)
	case reviewpkg.EventRunCompleted:
		e.Close()
	}
}

func (e *cliReviewEvents) startPhase(phase domain.ReviewPhase) {
	switch phase {
	case domain.ReviewPhaseFeedback:
		if e.opts.DetectedPR != "" {
			e.logger.Logf(terminal.StyleInfo, "Summarizing PR #%s feedback %s(in parallel)%s",
				e.opts.DetectedPR, terminal.Color(terminal.Dim), terminal.Color(terminal.Reset))
		}
	case domain.ReviewPhaseReviewers:
		spinner := terminal.NewSpinner(e.opts.Reviewers)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		e.reviewers = &runningReviewSpinner{
			cancel: cancel,
			done:   done,
			completed: func() {
				spinner.Completed().Add(1)
			},
		}
		go func() {
			spinner.Run(ctx)
			close(done)
		}()
	case domain.ReviewPhaseSummarization:
		e.summarization = startPhaseSpinner("Summarizing")
	case domain.ReviewPhaseFalsePositiveFilter:
		e.falsePositivePass = startPhaseSpinner("Filtering false positives")
	}
}

func (e *cliReviewEvents) completePhase(event reviewpkg.Event) {
	switch event.Phase {
	case domain.ReviewPhaseFeedback:
		switch event.Message {
		case "summarized":
			e.logger.Log("PR feedback summarized", terminal.StyleSuccess)
		case "empty":
			e.logger.Log("No relevant PR feedback found", terminal.StyleDim)
		}
	case domain.ReviewPhaseReviewers:
		e.reviewers.Stop()
	case domain.ReviewPhaseSummarization:
		e.summarization.Stop()
	case domain.ReviewPhaseFalsePositiveFilter:
		e.falsePositivePass.Stop()
	}
}

func (e *cliReviewEvents) reviewerOutput(event reviewpkg.Event) {
	if !e.opts.Verbose {
		return
	}
	message := event.Message
	if len(message) > reviewerOutputPreviewLength {
		message = message[:reviewerOutputPreviewLength] + "..."
	}
	e.logger.Logf(terminal.StyleDim, "%s#%d:%s %s%s%s",
		terminal.Color(terminal.Dim), event.ReviewerID, terminal.Color(terminal.Reset),
		terminal.Color(terminal.Dim), message, terminal.Color(terminal.Reset))
}

func (e *cliReviewEvents) warning(event reviewpkg.Event) {
	switch event.Phase {
	case domain.ReviewPhaseReviewers:
		if event.ReviewerID != 0 {
			e.logger.Logf(terminal.StyleWarning, "Reviewer #%d: %s", event.ReviewerID, event.Message)
		}
	case domain.ReviewPhaseFeedback:
		message := strings.TrimPrefix(event.Message, "prior feedback unavailable: ")
		message = strings.TrimPrefix(message, "prior feedback warning: ")
		if strings.Contains(message, context.DeadlineExceeded.Error()) {
			e.logger.Logf(terminal.StyleWarning, "PR feedback summarizer timed out after %s", e.opts.SummarizerTimeout)
			return
		}
		e.logger.Logf(terminal.StyleWarning, "PR feedback summarizer failed: %s", message)
	case domain.ReviewPhaseFalsePositiveFilter:
		if strings.HasPrefix(event.Message, "false-positive filter skipped: ") {
			reason := strings.TrimPrefix(event.Message, "false-positive filter skipped: ")
			e.logger.Logf(terminal.StyleWarning, "FP filter skipped (%s): showing all findings", reason)
			return
		}
		e.logger.Log(event.Message, terminal.StyleWarning)
	case domain.ReviewPhaseSummarization:
		e.logger.Log(event.Message, terminal.StyleWarning)
	}
}

func (e *cliReviewEvents) Close() {
	if e == nil {
		return
	}
	e.reviewers.Stop()
	e.summarization.Stop()
	e.falsePositivePass.Stop()
}

func startPhaseSpinner(label string) *runningReviewSpinner {
	spinner := terminal.NewPhaseSpinner(label)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		spinner.Run(ctx)
		close(done)
	}()
	return &runningReviewSpinner{cancel: cancel, done: done}
}
