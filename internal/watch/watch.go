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
	Discussion      []Discussion
}

type DiscussionID struct {
	Kind string
	ID   int64
}

type Discussion struct {
	ID       DiscussionID
	Author   string
	Body     string
	Path     string
	Line     int
	DiffHunk string
	Revision string
}

type RoutingDecision string

const (
	RoutingNoReview       RoutingDecision = "no_review"
	RoutingReviewRequired RoutingDecision = "review_required"
	RoutingUncertain      RoutingDecision = "uncertain"
)

type UncertainPolicy string

const (
	UncertainWait   UncertainPolicy = "wait"
	UncertainReview UncertainPolicy = "review"
)

func ParseUncertainPolicy(value string) (UncertainPolicy, error) {
	switch UncertainPolicy(value) {
	case UncertainWait, UncertainReview:
		return UncertainPolicy(value), nil
	}
	return "", fmt.Errorf("invalid uncertain discussion policy %q: must be wait or review", value)
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
	Result           CycleResult
	LGTMBody         string
	HeadSHA          string
	OwnDiscussionIDs []DiscussionID
}

type Deps struct {
	State           func(ctx context.Context) (PRState, error)
	RunCycle        func(ctx context.Context, reviewNum int, trigger string, discussion []Discussion, discussionRevision string) (Cycle, error)
	RouteDiscussion func(ctx context.Context, discussion []Discussion) (RoutingDecision, error)
	CIGreen         func(ctx context.Context) (bool, error)
	Approve         func(ctx context.Context, body string) error
	Control         func(ctx context.Context, state PRState) (ControlDecision, error)
	Wait            func(ctx context.Context, duration time.Duration) (WaitResult, error)
	Emit            func(event Event)
	Clock           Clock
	Logf            func(format string, args ...any)
}

type WaitResult struct {
	ManualRequests int
	Interrupted    bool
	Finalize       func(context.Context, <-chan struct{}) (WaitResult, error)
}

type EventType string

const (
	EventManualRequestReceived  EventType = "manual_request_received"
	EventManualRequestCoalesced EventType = "manual_request_coalesced"
	EventManualReviewStarted    EventType = "manual_review_started"
	EventManualRequestRejected  EventType = "manual_request_rejected"
	EventManualInputUnsafe      EventType = "manual_input_unsafe"
	EventDiscussionDetected     EventType = "discussion_detected"
	EventDiscussionRouted       EventType = "discussion_routed"
	EventDiscussionWaiting      EventType = "discussion_waiting"
	EventControlChanged         EventType = "control_changed"
)

type Event struct {
	Type            EventType
	RequestCount    int
	DiscussionCount int
	ReviewNumber    int
	Reason          string
	Decision        RoutingDecision
	Control         ControlState
	ResumeAt        time.Time
}

type ControlState string

const (
	ControlActive   ControlState = "active"
	ControlSnoozed  ControlState = "snoozed"
	ControlReleased ControlState = "released"
	ControlOptedOut ControlState = "opted_out"
)

type ControlDecision struct {
	State    ControlState
	ResumeAt time.Time
}

type Config struct {
	Mode            PostMode
	PollInterval    time.Duration
	SettleTime      time.Duration
	MaxReviews      int
	MaxDuration     time.Duration
	UncertainPolicy UncertainPolicy
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
	ReasonReleased
	ReasonOptedOut
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
	case ReasonReleased:
		return "released"
	case ReasonOptedOut:
		return "opted out"
	default:
		return "error"
	}
}

const maxConsecutivePollErrors = 5

type loop struct {
	cfg  Config
	deps Deps

	deadline           time.Time
	reviews            int
	lastHead           string
	pendingHead        string
	settleDeadline     time.Time
	requestArmed       bool
	pendingApproval    string
	ciErrors           int
	cycleErrors        int
	retryPending       bool
	retryHead          string
	manualRequests     int
	discussionCursor   map[DiscussionID]string
	ownDiscussion      map[DiscussionID]struct{}
	pendingDiscussion  []Discussion
	discussionDeadline time.Time
	waitingDiscussion  string
	control            ControlDecision
}

func (l *loop) logf(format string, args ...any) {
	if l.deps.Logf != nil {
		l.deps.Logf(format, args...)
	}
}

func (l *loop) emit(event Event) {
	if l.deps.Emit != nil {
		l.deps.Emit(event)
	}
}

func (l *loop) wait(ctx context.Context, duration time.Duration) (WaitResult, error) {
	if l.deps.Wait != nil {
		return l.deps.Wait(ctx, duration)
	}
	return WaitResult{}, l.deps.Clock.Sleep(ctx, duration)
}

func (l *loop) finalizeWait(
	ctx context.Context,
	result WaitResult,
	stateReady <-chan struct{},
) (WaitResult, error) {
	if result.Finalize == nil {
		return result, nil
	}
	additional, err := result.Finalize(ctx, stateReady)
	result.Finalize = nil
	result.ManualRequests += additional.ManualRequests
	result.Interrupted = result.Interrupted || additional.Interrupted
	return result, err
}

type stateOutcome struct {
	state PRState
	err   error
}

func (l *loop) stateAfterWait(
	ctx context.Context,
	result WaitResult,
) (PRState, error, WaitResult, error) {
	if result.Finalize == nil {
		state, err := l.deps.State(ctx)
		return state, err, result, nil
	}
	stateCtx, cancelState := context.WithCancel(ctx)
	defer cancelState()
	stateReady := make(chan struct{})
	outcome := make(chan stateOutcome, 1)
	go func() {
		state, err := l.deps.State(stateCtx)
		outcome <- stateOutcome{state: state, err: err}
		close(stateReady)
	}()
	finalized, finalizeErr := l.finalizeWait(ctx, result, stateReady)
	if finalizeErr != nil || finalized.Interrupted {
		cancelState()
	}
	stateResult := <-outcome
	return stateResult.state, stateResult.err, finalized, finalizeErr
}

func (l *loop) handleWaitError(err error) (ExitReason, bool) {
	if err == nil {
		return 0, false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ReasonInterrupted, true
	}
	l.emit(Event{Type: EventManualInputUnsafe, Reason: err.Error()})
	l.logf("Manual input could not be handled safely: %v", err)
	return ReasonError, true
}

func (l *loop) receiveManualRequests(count int) {
	if count <= 0 {
		return
	}
	l.manualRequests += count
	l.emit(Event{Type: EventManualRequestReceived, RequestCount: count})
	l.logf("Manual review requested.")
	if l.manualRequests > 1 {
		l.emit(Event{Type: EventManualRequestCoalesced, RequestCount: l.manualRequests})
		l.logf("Coalesced %d manual review requests into one review.", l.manualRequests)
	}
}

func (l *loop) rejectManualRequests(reason string) {
	if l.manualRequests == 0 {
		return
	}
	l.emit(Event{Type: EventManualRequestRejected, RequestCount: l.manualRequests, Reason: reason})
	l.logf("Manual review request rejected: %s.", reason)
	l.manualRequests = 0
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func DiscussionRevision(items []Discussion) string {
	var signature string
	for _, item := range items {
		signature += fmt.Sprintf("%s:%d:%s\n", item.ID.Kind, item.ID.ID, item.Revision)
	}
	return signature
}

func (l *loop) initializeDiscussion(items []Discussion) {
	l.discussionCursor = make(map[DiscussionID]string, len(items))
	l.ownDiscussion = make(map[DiscussionID]struct{})
	l.consumeDiscussion(items)
}

func (l *loop) consumeDiscussion(items []Discussion) {
	for _, item := range items {
		l.discussionCursor[item.ID] = item.Revision
	}
}

func (l *loop) unprocessedDiscussion(items []Discussion) []Discussion {
	pending := make([]Discussion, 0, len(items))
	for _, item := range items {
		if _, own := l.ownDiscussion[item.ID]; own {
			continue
		}
		if l.discussionCursor[item.ID] == item.Revision {
			continue
		}
		pending = append(pending, item)
	}
	return pending
}

func (l *loop) updatePendingDiscussion(items []Discussion) {
	signature := DiscussionRevision(items)
	if signature == DiscussionRevision(l.pendingDiscussion) {
		return
	}
	l.pendingDiscussion = append(l.pendingDiscussion[:0], items...)
	l.waitingDiscussion = ""
	if len(items) == 0 {
		l.discussionDeadline = time.Time{}
		return
	}
	l.discussionDeadline = l.deps.Clock.Now().Add(l.cfg.SettleTime)
	l.emit(Event{Type: EventDiscussionDetected, DiscussionCount: len(items)})
	l.logf("New PR discussion detected; waiting %s for discussion to settle.", l.cfg.SettleTime)
}

func (l *loop) routeDiscussion(ctx context.Context, items []Discussion) (RoutingDecision, error) {
	if l.deps.RouteDiscussion == nil {
		return RoutingUncertain, errors.New("discussion router is unavailable")
	}
	return l.deps.RouteDiscussion(ctx, items)
}

func (l *loop) controlDecision(ctx context.Context, state PRState) (ControlDecision, ExitReason, bool) {
	decision := ControlDecision{State: ControlActive}
	if l.deps.Control != nil {
		var err error
		decision, err = l.deps.Control(ctx, state)
		if err != nil {
			if ctx.Err() != nil {
				return ControlDecision{}, ReasonInterrupted, true
			}
			l.logf("Failed to determine lifecycle control state: %v", err)
			return ControlDecision{}, ReasonError, true
		}
	}
	if decision.State == "" {
		decision.State = ControlActive
	}
	if decision.State == ControlSnoozed && !decision.ResumeAt.IsZero() &&
		!l.deps.Clock.Now().Before(decision.ResumeAt) {
		decision = ControlDecision{State: ControlActive}
	}
	if decision != l.control {
		l.control = decision
		l.emit(Event{Type: EventControlChanged, Control: decision.State, ResumeAt: decision.ResumeAt})
	}
	switch decision.State {
	case ControlActive, ControlSnoozed:
		return decision, 0, false
	case ControlReleased:
		l.logf("PR lifecycle released; stopping watch.")
		return decision, ReasonReleased, true
	case ControlOptedOut:
		l.logf("PR lifecycle opted out; stopping watch.")
		return decision, ReasonOptedOut, true
	default:
		l.logf("Invalid lifecycle control state %q.", decision.State)
		return decision, ReasonError, true
	}
}

func (l *loop) awaitAdmission(ctx context.Context, state PRState) (PRState, ExitReason, bool) {
	pollErrors := 0
	for {
		decision, reason, done := l.controlDecision(ctx, state)
		if done {
			return PRState{}, reason, true
		}
		now := l.deps.Clock.Now()
		if !now.Before(l.deadline) {
			l.logf("Reached maximum duration (%s) while lifecycle was snoozed; stopping.", l.cfg.MaxDuration)
			return PRState{}, ReasonMaxDuration, true
		}
		if decision.State == ControlActive {
			return state, 0, false
		}
		sleep := l.cfg.PollInterval
		if !decision.ResumeAt.IsZero() {
			if untilResume := decision.ResumeAt.Sub(now); untilResume > 0 && untilResume < sleep {
				sleep = untilResume
			}
		}
		if remaining := l.deadline.Sub(now); remaining < sleep {
			sleep = remaining
		}
		waitResult, err := l.wait(ctx, sleep)
		if reason, done := l.handleWaitError(err); done {
			return PRState{}, reason, true
		}
		if waitResult.Interrupted {
			return PRState{}, ReasonInterrupted, true
		}
		if !l.deps.Clock.Now().Before(l.deadline) {
			stateReady := make(chan struct{})
			close(stateReady)
			waitResult, err = l.finalizeWait(ctx, waitResult, stateReady)
			if reason, done := l.handleWaitError(err); done {
				return PRState{}, reason, true
			}
			if waitResult.Interrupted {
				return PRState{}, ReasonInterrupted, true
			}
			l.receiveManualRequests(waitResult.ManualRequests)
			l.rejectManualRequests(ReasonMaxDuration.String())
			l.logf("Reached maximum duration (%s) while lifecycle was snoozed; stopping.", l.cfg.MaxDuration)
			return PRState{}, ReasonMaxDuration, true
		}
		refreshedState, err, waitResult, waitErr := l.stateAfterWait(ctx, waitResult)
		if reason, done := l.handleWaitError(waitErr); done {
			return PRState{}, reason, true
		}
		if waitResult.Interrupted {
			return PRState{}, ReasonInterrupted, true
		}
		if waitResult.ManualRequests > 0 {
			l.receiveManualRequests(waitResult.ManualRequests)
			l.rejectManualRequests("lifecycle is snoozed")
		}
		if err != nil {
			if ctx.Err() != nil {
				return PRState{}, ReasonInterrupted, true
			}
			pollErrors++
			l.logf("Failed to fetch PR state while lifecycle was snoozed (%d/%d): %v", pollErrors, maxConsecutivePollErrors, err)
			if pollErrors >= maxConsecutivePollErrors {
				return PRState{}, ReasonError, true
			}
			continue
		}
		state = refreshedState
		pollErrors = 0
		if reason, done := l.checkOpen(state); done {
			return PRState{}, reason, true
		}
	}
}

func Run(ctx context.Context, cfg Config, deps Deps) ExitReason {
	return newLifecycleFromDeps(cfg, deps).Run(ctx)
}

func run(ctx context.Context, cfg Config, deps Deps) ExitReason {
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
	st, reason, done := l.awaitAdmission(ctx, st)
	if done {
		return reason
	}
	l.initializeDiscussion(st.Discussion)

	l.requestArmed = !st.ReviewRequested

	if reason, done := l.cycle(ctx, st.HeadSHA, "initial review", nil, DiscussionRevision(st.Discussion)); done {
		return reason
	}
	if reason, done := l.checkMaxReviews(); done {
		return reason
	}

	lastState := st
	pollErrors := 0
	for {
		waitResult := WaitResult{}
		now := clock.Now()
		if !now.Before(deadline) {
			l.rejectManualRequests(ReasonMaxDuration.String())
			l.logf("Reached maximum duration (%s) without a terminal LGTM; stopping.", cfg.MaxDuration)
			return ReasonMaxDuration
		}
		if l.manualRequests == 0 || pollErrors > 0 {
			sleep := cfg.PollInterval
			if l.control.State == ControlSnoozed && !l.control.ResumeAt.IsZero() {
				if untilResume := l.control.ResumeAt.Sub(now); untilResume > 0 && untilResume < sleep {
					sleep = untilResume
				}
			}
			if remaining := deadline.Sub(now); remaining < sleep {
				sleep = remaining
			}
			var err error
			waitResult, err = l.wait(ctx, sleep)
			if err != nil {
				if reason, done := l.handleWaitError(err); done {
					return reason
				}
			}
			if waitResult.Interrupted {
				return ReasonInterrupted
			}
		}
		if !clock.Now().Before(deadline) {
			var err error
			stateReady := make(chan struct{})
			close(stateReady)
			waitResult, err = l.finalizeWait(ctx, waitResult, stateReady)
			if reason, done := l.handleWaitError(err); done {
				return reason
			}
			if waitResult.Interrupted {
				return ReasonInterrupted
			}
			l.receiveManualRequests(waitResult.ManualRequests)
			l.rejectManualRequests(ReasonMaxDuration.String())
			l.logf("Reached maximum duration (%s) without a terminal LGTM; stopping.", cfg.MaxDuration)
			return ReasonMaxDuration
		}

		st, err, waitResult, waitErr := l.stateAfterWait(ctx, waitResult)
		if reason, done := l.handleWaitError(waitErr); done {
			return reason
		}
		if waitResult.Interrupted {
			return ReasonInterrupted
		}
		l.receiveManualRequests(waitResult.ManualRequests)
		if err != nil {
			if ctx.Err() != nil {
				return ReasonInterrupted
			}
			pollErrors++
			l.logf("Failed to fetch PR state (%d/%d): %v", pollErrors, maxConsecutivePollErrors, err)
			control, reason, done := l.controlDecision(ctx, lastState)
			if done {
				if l.manualRequests > 0 {
					l.rejectManualRequests(reason.String())
				}
				return reason
			}
			if control.State == ControlSnoozed && l.manualRequests > 0 {
				l.rejectManualRequests("lifecycle is snoozed")
			}
			if pollErrors >= maxConsecutivePollErrors {
				return ReasonError
			}
			continue
		}
		pollErrors = 0

		manualTrigger := l.manualRequests > 0

		if reason, done := l.checkOpen(st); done {
			if manualTrigger {
				l.rejectManualRequests(reason.String())
			}
			return reason
		}
		lastState = st
		control, reason, done := l.controlDecision(ctx, st)
		if done {
			if manualTrigger {
				l.rejectManualRequests(reason.String())
			}
			return reason
		}
		if control.State == ControlSnoozed {
			if manualTrigger {
				l.rejectManualRequests("lifecycle is snoozed")
			}
			continue
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
		var cycleDiscussion []Discussion
		unprocessed := l.unprocessedDiscussion(st.Discussion)
		l.updatePendingDiscussion(unprocessed)
		if manualTrigger {
			trigger = "manual request"
			cycleDiscussion = unprocessed
			if st.ReviewRequested && l.requestArmed {
				l.requestArmed = false
			}
		} else if st.ReviewRequested && l.requestArmed {
			trigger = "re-review requested"
			cycleDiscussion = unprocessed
			l.requestArmed = false
		}

		if trigger == "" && l.retryPending {
			trigger = "retry after transient preparation failure"
			cycleDiscussion = unprocessed
		}

		if trigger == "" && l.pendingApproval != "" {
			if st.HeadSHA == l.lastHead {
				if len(unprocessed) == 0 {
					if reason, done := l.tryApprove(ctx); done {
						return reason
					}
					continue
				}
			} else {
				l.logf("New commit %s invalidates the pending approval.", shortSHA(st.HeadSHA))
				l.pendingApproval = ""
				if reason, done := l.checkMaxReviews(); done {
					return reason
				}
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
				if len(l.pendingDiscussion) == 0 || !clock.Now().Before(l.discussionDeadline) {
					trigger = "commits settled"
					cycleDiscussion = unprocessed
				}
			}
		}

		if trigger == "" && l.pendingHead == "" && len(l.pendingDiscussion) > 0 &&
			!clock.Now().Before(l.discussionDeadline) {
			signature := DiscussionRevision(l.pendingDiscussion)
			if signature != l.waitingDiscussion {
				routeCtx, cancelRoute := context.WithTimeout(ctx, l.deadline.Sub(clock.Now()))
				decision, routeErr := l.routeDiscussion(routeCtx, l.pendingDiscussion)
				cancelRoute()
				if ctx.Err() != nil {
					return ReasonInterrupted
				}
				if !clock.Now().Before(deadline) {
					l.logf("Reached maximum duration (%s) while routing PR discussion; stopping.", cfg.MaxDuration)
					return ReasonMaxDuration
				}
				if routeErr != nil {
					decision = RoutingUncertain
					l.logf("Discussion routing failed; treating the discussion as uncertain: %v", routeErr)
				}
				l.emit(Event{
					Type:            EventDiscussionRouted,
					DiscussionCount: len(l.pendingDiscussion),
					Decision:        decision,
				})
				switch decision {
				case RoutingNoReview:
					l.consumeDiscussion(l.pendingDiscussion)
					l.pendingDiscussion = nil
					l.logf("PR discussion does not require another review.")
				case RoutingReviewRequired:
					trigger = "discussion requires reconsideration"
					cycleDiscussion = append(cycleDiscussion, l.pendingDiscussion...)
				case RoutingUncertain:
					if cfg.UncertainPolicy == UncertainReview {
						trigger = "uncertain discussion requires reconsideration"
						cycleDiscussion = append(cycleDiscussion, l.pendingDiscussion...)
					} else {
						l.waitingDiscussion = signature
						l.emit(Event{
							Type:            EventDiscussionWaiting,
							DiscussionCount: len(l.pendingDiscussion),
							Decision:        decision,
						})
						l.logf("PR discussion routing is uncertain; waiting for new discussion or an explicit review request.")
					}
				default:
					l.waitingDiscussion = signature
					l.logf("Discussion router returned %q; waiting for new discussion or an explicit review request.", decision)
				}
			}
		}

		if trigger == "" {
			continue
		}

		if l.reviews >= cfg.MaxReviews {
			if manualTrigger {
				l.rejectManualRequests(ReasonMaxReviews.String())
			}
			if l.pendingApproval != "" {
				if len(cycleDiscussion) > 0 {
					l.waitingDiscussion = DiscussionRevision(cycleDiscussion)
					l.emit(Event{
						Type:            EventDiscussionWaiting,
						DiscussionCount: len(cycleDiscussion),
						Reason:          ReasonMaxReviews.String(),
					})
				}
				l.logf("Review budget exhausted; cannot process trigger (%s), so automatic approval remains blocked.", trigger)
				continue
			}
			l.logf("Reached maximum of %d reviews without a terminal LGTM; stopping.", cfg.MaxReviews)
			return ReasonMaxReviews
		}
		if reason, done := l.cycle(ctx, st.HeadSHA, trigger, cycleDiscussion, DiscussionRevision(st.Discussion)); done {
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
	unprocessed := l.unprocessedDiscussion(st.Discussion)
	if len(unprocessed) > 0 {
		l.updatePendingDiscussion(unprocessed)
		l.logf("New PR discussion arrived before approval; deferring.")
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

func (l *loop) cycle(
	ctx context.Context,
	head,
	trigger string,
	discussion []Discussion,
	discussionRevision string,
) (ExitReason, bool) {
	l.retryPending = false
	l.retryHead = ""
	l.pendingApproval = ""
	l.reviews++
	if trigger == "manual request" {
		l.emit(Event{Type: EventManualReviewStarted, RequestCount: l.manualRequests, ReviewNumber: l.reviews})
		l.manualRequests = 0
	}
	l.logf("Review #%d/%d starting (%s)", l.reviews, l.cfg.MaxReviews, trigger)

	cycleCtx, cancel := context.WithTimeout(ctx, l.deadline.Sub(l.deps.Clock.Now()))
	defer cancel()

	c, err := l.deps.RunCycle(cycleCtx, l.reviews, trigger, discussion, discussionRevision)
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
	for _, id := range c.OwnDiscussionIDs {
		l.ownDiscussion[id] = struct{}{}
	}

	if c.Result != CycleStaleHead {
		l.lastHead = head
		if c.HeadSHA != "" {
			l.lastHead = c.HeadSHA
		}
	}
	l.pendingHead = ""
	l.pendingApproval = ""
	if c.Result != CycleStaleHead {
		l.consumeDiscussion(discussion)
		l.pendingDiscussion = nil
		l.waitingDiscussion = ""
	}

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
		l.logf("Review #%d discarded: PR changed during the review; resuming watch.", l.reviews)
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
