package watch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.now = c.now.Add(d)
	return nil
}

type harness struct {
	t     *testing.T
	clock *fakeClock

	states []PRState
	stateI int

	cycles []Cycle
	cycleI int

	ci  []bool
	ciI int

	cancelAfterCycle int
	cancel           context.CancelFunc

	triggers     []string
	approvedWith []string
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, clock: &fakeClock{now: time.Unix(1_700_000_000, 0)}}
}

func (h *harness) deps() Deps {
	return Deps{
		Clock: h.clock,
		State: func(ctx context.Context) (PRState, error) {
			if h.stateI < len(h.states)-1 {
				h.stateI++
				return h.states[h.stateI-1], nil
			}
			return h.states[len(h.states)-1], nil
		},
		RunCycle: func(ctx context.Context, reviewNum int, trigger string) (Cycle, error) {
			if h.cycleI >= len(h.cycles) {
				h.t.Fatalf("unexpected review cycle #%d (trigger %q)", reviewNum, trigger)
			}
			h.triggers = append(h.triggers, trigger)
			c := h.cycles[h.cycleI]
			h.cycleI++
			if h.cancelAfterCycle > 0 && h.cycleI >= h.cancelAfterCycle && h.cancel != nil {
				h.cancel()
			}
			return c, nil
		},
		CIGreen: func(ctx context.Context) (bool, error) {
			if h.ciI < len(h.ci)-1 {
				h.ciI++
				return h.ci[h.ciI-1], nil
			}
			if len(h.ci) == 0 {
				return false, errors.New("no CI script")
			}
			return h.ci[len(h.ci)-1], nil
		},
		Approve: func(ctx context.Context, body string) error {
			h.approvedWith = append(h.approvedWith, body)
			return nil
		},
	}
}

func defaultConfig(mode PostMode) Config {
	return Config{
		Mode:         mode,
		PollInterval: time.Minute,
		SettleTime:   10 * time.Minute,
		MaxReviews:   10,
		MaxDuration:  24 * time.Hour,
	}
}

func open(head string) PRState { return PRState{HeadSHA: head} }

func requested(head string) PRState { return PRState{HeadSHA: head, ReviewRequested: true} }

func TestInitialReviewLGTMExitsImmediately(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 1 || h.triggers[0] != "initial review" {
		t.Errorf("triggers = %v, want [initial review]", h.triggers)
	}
}

func TestCommentModeLGTMCommentIsTerminal(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMComment}}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
}

func TestInteractiveDeclinedLGTMEndsWatch(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMDeclined}}

	if reason := Run(context.Background(), defaultConfig(PostModeInteractive), h.deps()); reason != ReasonDeclined {
		t.Fatalf("reason = %v, want ReasonDeclined", reason)
	}
}

func TestReReviewRequestTriggersWithoutSettleWait(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("aaa"),
		requested("aaa"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}

	start := h.clock.now
	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "re-review requested" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if elapsed := h.clock.now.Sub(start); elapsed > 5*time.Minute {
		t.Errorf("request trigger waited %s; settle time must not apply", elapsed)
	}
}

func TestPersistentRequestIsConsumedOnce(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		requested("aaa"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleFindings},
	}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = 30 * time.Minute

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 2 {
		t.Errorf("cycles = %d, want 2 (initial + one request trigger)", len(h.triggers))
	}
}

func TestRequestPendingAtStartupIsConsumedByInitialReview(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{requested("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = 30 * time.Minute

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 1 {
		t.Errorf("cycles = %d, want 1 (startup request consumed by initial review)", len(h.triggers))
	}
}

func TestRequestClearsAndReturnsRetriggering(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		requested("aaa"),
		open("aaa"),
		requested("aaa"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 3 {
		t.Errorf("cycles = %d, want 3", len(h.triggers))
	}
}

func TestNewCommitsWaitOutSettleTime(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("bbb"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}

	start := h.clock.now
	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if elapsed := h.clock.now.Sub(start); elapsed < 10*time.Minute {
		t.Errorf("second review after %s, want >= settle time (10m)", elapsed)
	}
}

func TestAdditionalCommitRestartsSettleTimer(t *testing.T) {
	h := newHarness(t)
	states := []PRState{open("aaa")}
	for range 5 {
		states = append(states, open("bbb"))
	}
	states = append(states, open("ccc"))
	h.states = states
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}

	start := h.clock.now
	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if elapsed := h.clock.now.Sub(start); elapsed < 16*time.Minute {
		t.Errorf("second review after %s, want >= 16m (timer restarted by ccc)", elapsed)
	}
}

func TestUnchangedHeadNeverRetriggers(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = time.Hour

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 1 {
		t.Errorf("cycles = %d, want 1", len(h.triggers))
	}
}

func TestMaxReviewsBound(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		requested("aaa"),
		open("aaa"),
		requested("aaa"),
		open("aaa"),
		requested("aaa"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleFindings},
		{Result: CycleFindings},
	}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxReviews = 3

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonMaxReviews {
		t.Fatalf("reason = %v, want ReasonMaxReviews", reason)
	}
	if len(h.triggers) != 3 {
		t.Errorf("cycles = %d, want 3", len(h.triggers))
	}
}

func TestMergedPRStopsWatch(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		{HeadSHA: "aaa", Merged: true},
	}
	h.cycles = []Cycle{{Result: CycleFindings}}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), h.deps()); reason != ReasonMerged {
		t.Fatalf("reason = %v, want ReasonMerged", reason)
	}
}

func TestClosedPRBeforeFirstReview(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{{HeadSHA: "aaa", Closed: true}}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), h.deps()); reason != ReasonClosed {
		t.Fatalf("reason = %v, want ReasonClosed", reason)
	}
	if len(h.triggers) != 0 {
		t.Errorf("cycles = %d, want 0", len(h.triggers))
	}
}

func TestApproveModeWaitsForCIThenApproves(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "LGTM body"}}
	h.ci = []bool{false, false, true}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.approvedWith) != 1 || h.approvedWith[0] != "LGTM body" {
		t.Errorf("approvals = %v, want the retained LGTM body", h.approvedWith)
	}
	if len(h.triggers) != 1 {
		t.Errorf("cycles = %d, want 1 (CI wait must not consume review runs)", len(h.triggers))
	}
}

func TestNewCommitInvalidatesPendingApproval(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("aaa"),
		open("bbb"),
	}
	h.cycles = []Cycle{
		{Result: CycleLGTMCommentCIPending, LGTMBody: "stale"},
		{Result: CycleLGTMApproved},
	}
	h.ci = []bool{false}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.approvedWith) != 0 {
		t.Errorf("stale approval posted: %v", h.approvedWith)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestInterruptDuringWatch(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.cancelAfterCycle = 1
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}

	if reason := Run(ctx, defaultConfig(PostModeComment), h.deps()); reason != ReasonInterrupted {
		t.Fatalf("reason = %v, want ReasonInterrupted", reason)
	}
}

func TestCommentModeCIPendingIsTerminal(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "x"}}

	if reason := Run(context.Background(), defaultConfig(PostModeInteractive), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.approvedWith) != 0 {
		t.Errorf("no approval should be posted outside approve mode: %v", h.approvedWith)
	}
}

func TestStateErrorsAreToleratedThenFatal(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	deps := h.deps()
	stateCalls := 0
	deps.State = func(ctx context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 1 {
			return open("aaa"), nil
		}
		return PRState{}, errors.New("transient gh failure")
	}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), deps); reason != ReasonError {
		t.Fatalf("reason = %v, want ReasonError after repeated poll failures", reason)
	}
	if stateCalls != 1+maxConsecutivePollErrors {
		t.Errorf("state calls = %d, want %d", stateCalls, 1+maxConsecutivePollErrors)
	}
}

func TestParsePostMode(t *testing.T) {
	for _, valid := range []string{"interactive", "comment", "approve"} {
		if _, err := ParsePostMode(valid); err != nil {
			t.Errorf("ParsePostMode(%q) error: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "auto", "yes", "Interactive"} {
		if _, err := ParsePostMode(invalid); err == nil {
			t.Errorf("ParsePostMode(%q) should fail", invalid)
		}
	}
}

func TestExhaustedBudgetTriggerDoesNotAbandonPendingApproval(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		requested("aaa"),
	}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "promised"}}
	h.ci = []bool{false, true}
	cfg := defaultConfig(PostModeApprove)
	cfg.MaxReviews = 1

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM (trigger must not abandon the CI wait)", reason)
	}
	if len(h.approvedWith) != 1 || h.approvedWith[0] != "promised" {
		t.Errorf("approvals = %v, want the promised body", h.approvedWith)
	}
	if len(h.triggers) != 1 {
		t.Errorf("cycles = %d, want 1 (no budget for the trigger)", len(h.triggers))
	}
}

func TestCycleContextCarriesMaxDurationBound(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}
	cfg := defaultConfig(PostModeApprove)

	deps := h.deps()
	inner := deps.RunCycle
	var sawDeadline bool
	deps.RunCycle = func(ctx context.Context, n int, trigger string) (Cycle, error) {
		_, sawDeadline = ctx.Deadline()
		return inner(ctx, n, trigger)
	}

	if reason := Run(context.Background(), cfg, deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if !sawDeadline {
		t.Error("cycle context must carry a deadline bounding it by --max-duration")
	}
}

func TestReviewedHeadPreferredOverPolledHead(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("bbb"),
	}
	h.cycles = []Cycle{{Result: CycleFindings, HeadSHA: "bbb"}}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = time.Hour

	reason := Run(context.Background(), cfg, h.deps())

	if reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 1 {
		t.Errorf("cycles = %d, want 1 (bbb was already reviewed)", len(h.triggers))
	}
}

func TestStaleHeadCycleResumesWatching(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("bbb"),
	}
	h.cycles = []Cycle{
		{Result: CycleStaleHead, HeadSHA: "aaa"},
		{Result: CycleLGTMApproved},
	}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v, want stale cycle followed by a settled re-review", h.triggers)
	}
}

func TestDeferredApprovalRechecksHeadBeforePosting(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("aaa"),
		open("bbb"),
		open("bbb"),
	}
	h.cycles = []Cycle{
		{Result: CycleLGTMCommentCIPending, LGTMBody: "stale"},
		{Result: CycleLGTMApproved},
	}
	h.ci = []bool{true}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.approvedWith) != 0 {
		t.Errorf("stale approval posted for an unreviewed head: %v", h.approvedWith)
	}
	if len(h.triggers) != 2 {
		t.Errorf("cycles = %d, want 2 (new head re-reviewed)", len(h.triggers))
	}
}

func TestTerminalResultWinsOverExpiredDeadline(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}
	cfg := defaultConfig(PostModeApprove)
	cfg.MaxDuration = 30 * time.Minute

	deps := h.deps()
	inner := deps.RunCycle
	deps.RunCycle = func(ctx context.Context, n int, trigger string) (Cycle, error) {
		h.clock.now = h.clock.now.Add(time.Hour)
		return inner(ctx, n, trigger)
	}

	if reason := Run(context.Background(), cfg, deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM (a posted result beats an expired deadline)", reason)
	}
}

func TestStartupStateToleratesTransientErrors(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}

	deps := h.deps()
	inner := deps.State
	calls := 0
	deps.State = func(ctx context.Context) (PRState, error) {
		calls++
		if calls <= 2 {
			return PRState{}, errors.New("transient gh failure")
		}
		return inner(ctx)
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM after transient startup failures", reason)
	}
	if calls != 3 {
		t.Errorf("state calls = %d, want 3 (two failures then success)", calls)
	}
}
