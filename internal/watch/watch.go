package watch

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrRetryableCycle = errors.New("retryable watch cycle failure")

type PostMode string

const (
	PostModeInteractive PostMode = "interactive"
	PostModeComment     PostMode = "comment"
	PostModeApprove     PostMode = "approve"
)

func ParsePostMode(s string) (PostMode, error) {
	switch PostMode(s) {
	case PostModeInteractive, PostModeComment, PostModeApprove:
		return PostMode(s), nil
	}
	return "", fmt.Errorf("invalid post mode %q: must be interactive, comment, or approve", s)
}

type PRState struct {
	HeadSHA         string
	Closed          bool
	Merged          bool
	ReviewRequested bool
}

type CycleResult int

const (
	CycleError CycleResult = iota
	CycleNoChanges
	CycleFindings
	CycleLGTMApproved
	CycleLGTMComment
	CycleLGTMCommentCIPending
	CycleLGTMDeclined
	CycleLGTMSkipped
	CycleStaleHead
)

type Cycle struct {
	Result   CycleResult
	LGTMBody string
	HeadSHA  string
}

type Deps struct {
	State    func(ctx context.Context) (PRState, error)
	RunCycle func(ctx context.Context, reviewNum int, trigger string) (Cycle, error)
	CIGreen  func(ctx context.Context) (bool, error)
	Approve  func(ctx context.Context, body string) error
	Clock    Clock
	Logf     func(format string, args ...any)
}

type Config struct {
	Mode         PostMode
	PollInterval time.Duration
	SettleTime   time.Duration
	MaxReviews   int
	MaxDuration  time.Duration
}

type ExitReason int

const (
	ReasonLGTM ExitReason = iota
	ReasonDeclined
	ReasonMerged
	ReasonClosed
	ReasonMaxReviews
	ReasonMaxDuration
	ReasonInterrupted
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

const maxConsecutivePollErrors = 5

type loop struct {
	cfg  Config
	deps Deps

	deadline        time.Time
	reviews         int
	lastHead        string
	pendingHead     string
	settleDeadline  time.Time
	requestArmed    bool
	pendingApproval string
	ciErrors        int
	cycleErrors     int
	retryPending    bool
	retryHead       string
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

func Run(ctx context.Context, cfg Config, deps Deps) ExitReason {
	l := &loop{cfg: cfg, deps: deps, requestArmed: true}
	clock := deps.Clock
	l.deadline = clock.Now().Add(cfg.MaxDuration)
	deadline := l.deadline

	st, err := l.fetchInitialState(ctx)
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
		if l.retryPending && st.HeadSHA != l.retryHead {
			l.retryPending = false
			l.retryHead = ""
			l.cycleErrors = 0
		}

		if !st.ReviewRequested {
			l.requestArmed = true
		}

		trigger := ""
		if st.ReviewRequested && l.requestArmed {
			trigger = "re-review requested"
			l.requestArmed = false
		}

		if trigger == "" && l.retryPending {
			trigger = "retry after transient preparation failure"
		}

		if trigger == "" && l.pendingApproval != "" {
			if st.HeadSHA == l.lastHead {
				if reason, done := l.tryApprove(ctx); done {
					return reason
				}
				continue
			}
			l.logf("New commit %s invalidates the pending approval.", shortSHA(st.HeadSHA))
			l.pendingApproval = ""
			if reason, done := l.checkMaxReviews(); done {
				return reason
			}
		}

		if trigger == "" {
			switch {
			case st.HeadSHA == l.lastHead:
				l.pendingHead = ""
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

func (l *loop) fetchInitialState(ctx context.Context) (PRState, error) {
	for attempt := 1; ; attempt++ {
		st, err := l.deps.State(ctx)
		if err == nil {
			return st, nil
		}
		if ctx.Err() != nil || attempt >= maxConsecutivePollErrors {
			return PRState{}, err
		}
		l.logf("Failed to fetch PR state (%d/%d): %v", attempt, maxConsecutivePollErrors, err)
		if err := l.deps.Clock.Sleep(ctx, l.cfg.PollInterval); err != nil {
			return PRState{}, err
		}
	}
}

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

func (l *loop) checkMaxReviews() (ExitReason, bool) {
	if l.reviews >= l.cfg.MaxReviews && l.pendingApproval == "" {
		l.logf("Reached maximum of %d reviews without a terminal LGTM; stopping.", l.cfg.MaxReviews)
		return ReasonMaxReviews, true
	}
	return 0, false
}

func (l *loop) tryApprove(ctx context.Context) (ExitReason, bool) {
	green, err := l.deps.CIGreen(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ReasonInterrupted, true
		}
		l.ciErrors++
		l.logf("CI status check failed (%d/%d): %v", l.ciErrors, maxConsecutivePollErrors, err)
		if l.ciErrors >= maxConsecutivePollErrors {
			return ReasonError, true
		}
		return 0, false
	}
	l.ciErrors = 0
	if !green {
		return 0, false
	}
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

func (l *loop) cycle(ctx context.Context, head, trigger string) (ExitReason, bool) {
	l.retryPending = false
	l.retryHead = ""
	l.pendingApproval = ""
	l.reviews++
	l.logf("Review #%d/%d starting (%s)", l.reviews, l.cfg.MaxReviews, trigger)

	cycleCtx, cancel := context.WithTimeout(ctx, l.deadline.Sub(l.deps.Clock.Now()))
	defer cancel()

	c, err := l.deps.RunCycle(cycleCtx, l.reviews, trigger)
	if ctx.Err() != nil {
		return ReasonInterrupted, true
	}
	if err != nil {
		if cycleCtx.Err() == context.DeadlineExceeded {
			l.logf("Reached maximum duration (%s) during review #%d; stopping.", l.cfg.MaxDuration, l.reviews)
			return ReasonMaxDuration, true
		}
		if errors.Is(err, ErrRetryableCycle) {
			l.reviews--
			l.cycleErrors++
			if l.cycleErrors >= maxConsecutivePollErrors {
				l.logf("Review preparation failed (%d/%d); stopping: %v", l.cycleErrors, maxConsecutivePollErrors, err)
				return ReasonError, true
			}
			l.logf("Review preparation failed (%d/%d); will retry: %v", l.cycleErrors, maxConsecutivePollErrors, err)
			l.retryPending = true
			l.retryHead = head
			return 0, false
		}
		l.logf("Review #%d failed: %v", l.reviews, err)
		return ReasonError, true
	}
	l.cycleErrors = 0

	if c.Result != CycleStaleHead {
		l.lastHead = head
		if c.HeadSHA != "" {
			l.lastHead = c.HeadSHA
		}
	}
	l.pendingHead = ""
	l.pendingApproval = ""

	switch c.Result {
	case CycleLGTMApproved:
		l.logf("Approval posted; watch complete.")
		return ReasonLGTM, true
	case CycleLGTMComment:
		l.logf("LGTM comment posted; watch complete.")
		return ReasonLGTM, true
	case CycleLGTMCommentCIPending:
		if l.cfg.Mode == PostModeApprove {
			l.pendingApproval = c.LGTMBody
			l.logf("LGTM comment posted; waiting for CI to go green before approving.")
			return 0, false
		}
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
