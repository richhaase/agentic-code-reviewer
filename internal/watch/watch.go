// Package watch implements the acr watch loop: it runs review cycles against
// a single PR, re-reviewing on re-review requests or settled commits, until a
// terminal LGTM is posted or a safety bound is reached. All effects (GitHub
// state, review cycles, CI checks, time) are injected so the loop is
// deterministic under test.
package watch

import (
	"context"
	"fmt"
	"time"
)

// PostMode controls how watch cycles post results.
type PostMode string

const (
	// PostModeInteractive preserves the human-in-the-loop submission flow.
	PostModeInteractive PostMode = "interactive"
	// PostModeComment posts every result as a comment review only.
	PostModeComment PostMode = "comment"
	// PostModeApprove runs unattended with the automated approval flow.
	PostModeApprove PostMode = "approve"
)

// ParsePostMode validates a --post-mode flag value.
func ParsePostMode(s string) (PostMode, error) {
	switch PostMode(s) {
	case PostModeInteractive, PostModeComment, PostModeApprove:
		return PostMode(s), nil
	}
	return "", fmt.Errorf("invalid post mode %q: must be interactive, comment, or approve", s)
}

// PRState is a snapshot of the watched PR.
type PRState struct {
	HeadSHA         string
	Closed          bool // closed without merging
	Merged          bool
	ReviewRequested bool // pending re-review request for the authenticated user
}

// CycleResult classifies what a review cycle produced and posted.
type CycleResult int

const (
	// CycleError means the cycle failed.
	CycleError CycleResult = iota
	// CycleNoChanges means the diff was empty; nothing was reviewed.
	CycleNoChanges
	// CycleFindings means findings were posted or handled; keep watching.
	CycleFindings
	// CycleLGTMApproved means an approval was posted.
	CycleLGTMApproved
	// CycleLGTMComment means an LGTM comment was posted (not a CI downgrade).
	CycleLGTMComment
	// CycleLGTMCommentCIPending means an intended approval was posted as a
	// comment because CI was not green; the approval is still wanted.
	CycleLGTMCommentCIPending
	// CycleLGTMDeclined means the user chose not to post an LGTM result.
	CycleLGTMDeclined
	// CycleLGTMSkipped means an LGTM result could not be posted at all.
	CycleLGTMSkipped
	// CycleStaleHead means nothing was posted because the PR head moved while
	// the review ran; the new head re-enters normal watching.
	CycleStaleHead
)

// Cycle is the outcome of one review cycle.
type Cycle struct {
	Result   CycleResult
	LGTMBody string // rendered LGTM body, for a deferred approval
	// HeadSHA is the commit the cycle actually reviewed (the worktree HEAD),
	// which can be newer than the SHA observed at poll time if commits landed
	// in between. Empty means unknown; the loop falls back to the polled SHA.
	HeadSHA string
}

// Deps are the injected effects the watch loop drives.
type Deps struct {
	// State fetches the current PR state.
	State func(ctx context.Context) (PRState, error)
	// RunCycle runs one full review cycle (worktree, review, post) and
	// reports what it did.
	RunCycle func(ctx context.Context, reviewNum int, trigger string) (Cycle, error)
	// CIGreen reports whether all CI checks on the PR pass.
	CIGreen func(ctx context.Context) (bool, error)
	// Approve posts an approval with the given body.
	Approve func(ctx context.Context, body string) error
	Clock   Clock
	// Logf emits a status transition; may be nil.
	Logf func(format string, args ...any)
}

// Config bounds and paces the watch loop.
type Config struct {
	Mode         PostMode
	PollInterval time.Duration
	SettleTime   time.Duration
	MaxReviews   int
	MaxDuration  time.Duration
}

// ExitReason says why the watch loop stopped.
type ExitReason int

const (
	// ReasonLGTM means the terminal LGTM for the post mode was posted.
	ReasonLGTM ExitReason = iota
	// ReasonDeclined means the user declined to post an LGTM (interactive).
	ReasonDeclined
	// ReasonMerged means the PR was merged.
	ReasonMerged
	// ReasonClosed means the PR was closed without merging.
	ReasonClosed
	// ReasonMaxReviews means the review-count bound was reached.
	ReasonMaxReviews
	// ReasonMaxDuration means the wall-clock bound was reached.
	ReasonMaxDuration
	// ReasonInterrupted means the process was signaled or canceled.
	ReasonInterrupted
	// ReasonError means a cycle or GitHub operation failed.
	ReasonError
)

func (r ExitReason) String() string {
	switch r {
	case ReasonLGTM:
		return "LGTM posted"
	case ReasonDeclined:
		return "LGTM declined by user"
	case ReasonMerged:
		return "PR merged"
	case ReasonClosed:
		return "PR closed"
	case ReasonMaxReviews:
		return "maximum reviews reached"
	case ReasonMaxDuration:
		return "maximum duration reached"
	case ReasonInterrupted:
		return "interrupted"
	default:
		return "error"
	}
}

// maxConsecutivePollErrors bounds transient PR-state fetch failures before
// the loop gives up.
const maxConsecutivePollErrors = 5

type loop struct {
	cfg  Config
	deps Deps

	deadline        time.Time // wall-clock bound from MaxDuration
	reviews         int
	lastHead        string    // head SHA covered by the last review
	pendingHead     string    // head SHA waiting out the settle period
	settleDeadline  time.Time // when pendingHead is considered settled
	requestArmed    bool      // rising-edge detector for re-review requests
	pendingApproval string    // LGTM body awaiting green CI (approve mode)
}

func (l *loop) logf(format string, args ...any) {
	if l.deps.Logf != nil {
		l.deps.Logf(format, args...)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Run drives the watch loop until a terminal state and returns why it stopped.
func Run(ctx context.Context, cfg Config, deps Deps) ExitReason {
	l := &loop{cfg: cfg, deps: deps, requestArmed: true}
	clock := deps.Clock
	l.deadline = clock.Now().Add(cfg.MaxDuration)
	deadline := l.deadline

	st, err := deps.State(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ReasonInterrupted
		}
		l.logf("Failed to fetch PR state: %v", err)
		return ReasonError
	}
	if reason, done := l.checkOpen(st); done {
		return reason
	}

	// A review request pending at startup is consumed by the initial review.
	l.requestArmed = !st.ReviewRequested

	if reason, done := l.cycle(ctx, st.HeadSHA, "initial review"); done {
		return reason
	}
	if reason, done := l.checkMaxReviews(); done {
		return reason
	}

	pollErrors := 0
	for {
		now := clock.Now()
		if !now.Before(deadline) {
			l.logf("Reached maximum duration (%s) without a terminal LGTM; stopping.", cfg.MaxDuration)
			return ReasonMaxDuration
		}
		sleep := cfg.PollInterval
		if remaining := deadline.Sub(now); remaining < sleep {
			sleep = remaining
		}
		if err := clock.Sleep(ctx, sleep); err != nil {
			return ReasonInterrupted
		}
		if !clock.Now().Before(deadline) {
			l.logf("Reached maximum duration (%s) without a terminal LGTM; stopping.", cfg.MaxDuration)
			return ReasonMaxDuration
		}

		st, err := deps.State(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ReasonInterrupted
			}
			pollErrors++
			l.logf("Failed to fetch PR state (%d/%d): %v", pollErrors, maxConsecutivePollErrors, err)
			if pollErrors >= maxConsecutivePollErrors {
				return ReasonError
			}
			continue
		}
		pollErrors = 0

		if reason, done := l.checkOpen(st); done {
			return reason
		}

		// Rising-edge detection: a serviced request must clear (GitHub clears
		// it when the review posts) before a request can trigger again.
		if !st.ReviewRequested {
			l.requestArmed = true
		}

		trigger := ""
		if st.ReviewRequested && l.requestArmed {
			trigger = "re-review requested"
			l.requestArmed = false
		}

		if trigger == "" && l.pendingApproval != "" {
			if st.HeadSHA == l.lastHead {
				if reason, done := l.tryApprove(ctx); done {
					return reason
				}
				continue
			}
			// New commits invalidate the pending approval; fall through to
			// normal settle tracking for the new head.
			l.logf("New commit %s invalidates the pending approval.", shortSHA(st.HeadSHA))
			l.pendingApproval = ""
			if reason, done := l.checkMaxReviews(); done {
				return reason
			}
		}

		if trigger == "" {
			switch {
			case st.HeadSHA == l.lastHead:
				l.pendingHead = "" // head is back to the reviewed state
			case st.HeadSHA != l.pendingHead:
				l.pendingHead = st.HeadSHA
				l.settleDeadline = clock.Now().Add(cfg.SettleTime)
				l.logf("New head %s; waiting %s for commits to settle.", shortSHA(st.HeadSHA), cfg.SettleTime)
			case !clock.Now().Before(l.settleDeadline):
				trigger = "commits settled"
			}
		}

		if trigger == "" {
			continue
		}

		if l.reviews >= cfg.MaxReviews {
			// With an approval pending on CI, dropping the wait because a
			// trigger arrived would make the trigger strictly worse than
			// doing nothing; ignore it and keep waiting.
			if l.pendingApproval != "" {
				l.logf("Review budget exhausted; ignoring trigger (%s) and continuing to wait for CI.", trigger)
				continue
			}
			l.logf("Reached maximum of %d reviews without a terminal LGTM; stopping.", cfg.MaxReviews)
			return ReasonMaxReviews
		}
		if reason, done := l.cycle(ctx, st.HeadSHA, trigger); done {
			return reason
		}
		if reason, done := l.checkMaxReviews(); done {
			return reason
		}
	}
}

// checkOpen maps a merged or closed PR to its exit reason.
func (l *loop) checkOpen(st PRState) (ExitReason, bool) {
	if st.Merged {
		l.logf("PR merged; stopping watch.")
		return ReasonMerged, true
	}
	if st.Closed {
		l.logf("PR closed; stopping watch.")
		return ReasonClosed, true
	}
	return 0, false
}

// checkMaxReviews stops the loop once the review budget is spent, unless an
// approval is still pending on CI (the CI wait consumes no review runs).
func (l *loop) checkMaxReviews() (ExitReason, bool) {
	if l.reviews >= l.cfg.MaxReviews && l.pendingApproval == "" {
		l.logf("Reached maximum of %d reviews without a terminal LGTM; stopping.", l.cfg.MaxReviews)
		return ReasonMaxReviews, true
	}
	return 0, false
}

// tryApprove checks CI and posts the pending approval when green.
func (l *loop) tryApprove(ctx context.Context) (ExitReason, bool) {
	green, err := l.deps.CIGreen(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ReasonInterrupted, true
		}
		l.logf("CI status check failed: %v", err)
		return 0, false
	}
	if !green {
		return 0, false
	}
	// Re-check the head immediately before approving: a commit landing after
	// the poll must not receive an approval it was never reviewed for. On a
	// mismatch the next poll invalidates the pending approval.
	st, err := l.deps.State(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ReasonInterrupted, true
		}
		l.logf("PR state check before approval failed: %v", err)
		return 0, false
	}
	if st.HeadSHA != l.lastHead {
		l.logf("New commit %s arrived before the approval could post; deferring.", shortSHA(st.HeadSHA))
		return 0, false
	}
	if err := l.deps.Approve(ctx, l.pendingApproval); err != nil {
		if ctx.Err() != nil {
			return ReasonInterrupted, true
		}
		l.logf("Failed to post approval: %v", err)
		return ReasonError, true
	}
	l.logf("CI green; approval posted. Watch complete.")
	return ReasonLGTM, true
}

// cycle runs one review cycle and maps its outcome. The bool reports whether
// the loop should stop with the returned reason.
func (l *loop) cycle(ctx context.Context, head, trigger string) (ExitReason, bool) {
	l.reviews++
	l.logf("Review #%d/%d starting (%s)", l.reviews, l.cfg.MaxReviews, trigger)

	// Bound the cycle by the remaining max-duration budget so a hung reviewer
	// cannot exceed the advertised wall-clock bound. The timeout is relative
	// (not a deadline) so it composes with an injected test clock.
	cycleCtx, cancel := context.WithTimeout(ctx, l.deadline.Sub(l.deps.Clock.Now()))
	defer cancel()

	c, err := l.deps.RunCycle(cycleCtx, l.reviews, trigger)
	if ctx.Err() != nil {
		return ReasonInterrupted, true
	}
	if err != nil {
		// The deadline only wins when the cycle actually failed: a result
		// posted just before the deadline is still a result.
		if cycleCtx.Err() == context.DeadlineExceeded {
			l.logf("Reached maximum duration (%s) during review #%d; stopping.", l.cfg.MaxDuration, l.reviews)
			return ReasonMaxDuration, true
		}
		l.logf("Review #%d failed: %v", l.reviews, err)
		return ReasonError, true
	}

	// Prefer the head the cycle actually reviewed (commits may have landed
	// between the poll and the worktree fetch).
	l.lastHead = head
	if c.HeadSHA != "" {
		l.lastHead = c.HeadSHA
	}
	l.pendingHead = ""
	l.pendingApproval = ""

	switch c.Result {
	case CycleLGTMApproved:
		l.logf("Approval posted; watch complete.")
		return ReasonLGTM, true
	case CycleLGTMComment:
		// Terminal in comment and interactive modes; in approve mode this
		// only happens when approval is impossible (self-review).
		l.logf("LGTM comment posted; watch complete.")
		return ReasonLGTM, true
	case CycleLGTMCommentCIPending:
		if l.cfg.Mode == PostModeApprove {
			l.pendingApproval = c.LGTMBody
			l.logf("LGTM comment posted; waiting for CI to go green before approving.")
			return 0, false
		}
		// Interactive: the user chose the comment downgrade — a posted LGTM.
		l.logf("LGTM comment posted; watch complete.")
		return ReasonLGTM, true
	case CycleLGTMDeclined:
		l.logf("LGTM result declined by user; ending watch without posting.")
		return ReasonDeclined, true
	case CycleLGTMSkipped:
		l.logf("Review #%d produced an LGTM that could not be posted; stopping.", l.reviews)
		return ReasonError, true
	case CycleStaleHead:
		l.logf("Review #%d discarded: PR head moved during the review; resuming watch.", l.reviews)
		return 0, false
	case CycleNoChanges:
		l.logf("Review #%d: no changes to review; resuming watch.", l.reviews)
		return 0, false
	case CycleFindings:
		l.logf("Review #%d complete (findings); resuming watch.", l.reviews)
		return 0, false
	default:
		return ReasonError, true
	}
}
