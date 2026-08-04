package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	states     []PRState
	stateI     int
	stateCalls int

	cycles []Cycle
	cycleI int

	ci  []bool
	ciI int

	cancelAfterCycle int
	cancel           context.CancelFunc

	triggers        []string
	cycleDiscussion [][]Discussion
	routes          []RoutingDecision
	routeI          int
	routed          [][]Discussion
	approvedWith    []string
	logs            []string
	events          []Event
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, clock: &fakeClock{now: time.Unix(1_700_000_000, 0)}}
}

func (h *harness) deps() Deps {
	return Deps{
		Clock: h.clock,
		State: func(ctx context.Context) (PRState, error) {
			h.stateCalls++
			if h.stateI < len(h.states)-1 {
				h.stateI++
				return h.states[h.stateI-1], nil
			}
			return h.states[len(h.states)-1], nil
		},
		RunCycle: func(ctx context.Context, reviewNum int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
			if h.cycleI >= len(h.cycles) {
				h.t.Fatalf("unexpected review cycle #%d (trigger %q)", reviewNum, trigger)
			}
			h.triggers = append(h.triggers, trigger)
			h.cycleDiscussion = append(h.cycleDiscussion, append([]Discussion(nil), discussion...))
			c := h.cycles[h.cycleI]
			h.cycleI++
			if h.cancelAfterCycle > 0 && h.cycleI >= h.cancelAfterCycle && h.cancel != nil {
				h.cancel()
			}
			return c, nil
		},
		RouteDiscussion: func(_ context.Context, discussion []Discussion) (RoutingDecision, error) {
			h.routed = append(h.routed, append([]Discussion(nil), discussion...))
			if h.routeI >= len(h.routes) {
				h.t.Fatalf("unexpected discussion routing request: %#v", discussion)
			}
			decision := h.routes[h.routeI]
			h.routeI++
			return decision, nil
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
		Logf: func(format string, args ...any) {
			h.logs = append(h.logs, fmt.Sprintf(format, args...))
		},
		Emit: func(event Event) {
			h.events = append(h.events, event)
		},
	}
}

func defaultConfig(mode PostMode) Config {
	return Config{
		Mode:            mode,
		PollInterval:    time.Minute,
		SettleTime:      10 * time.Minute,
		MaxReviews:      10,
		MaxDuration:     24 * time.Hour,
		UncertainPolicy: UncertainWait,
	}
}

func open(head string) PRState { return PRState{HeadSHA: head} }

func requested(head string) PRState { return PRState{HeadSHA: head, ReviewRequested: true} }

func discussed(head string, items ...Discussion) PRState {
	return PRState{HeadSHA: head, Discussion: items}
}

func discussion(kind string, id int64, revision, author, body string) Discussion {
	return Discussion{
		ID:       DiscussionID{Kind: kind, ID: id},
		Author:   author,
		Body:     body,
		Revision: revision,
	}
}

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

func TestSettledDiscussionRoutesIntoFullReview(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "The nil case changes the conclusion.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}
	h.routes = []RoutingDecision{RoutingReviewRequired}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "discussion requires reconsideration" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if len(h.cycleDiscussion[1]) != 1 || h.cycleDiscussion[1][0].ID != item.ID {
		t.Fatalf("cycle discussion = %#v", h.cycleDiscussion)
	}
}

func TestRetryableDiscussionReviewPreservesEvidence(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "The nil case changes the conclusion.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.routes = []RoutingDecision{RoutingReviewRequired}
	deps := h.deps()
	var triggers []string
	var revisions []string
	var cycleDiscussion [][]Discussion
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		triggers = append(triggers, trigger)
		revisions = append(revisions, revision)
		cycleDiscussion = append(cycleDiscussion, append([]Discussion(nil), discussion...))
		switch len(triggers) {
		case 1:
			return Cycle{Result: CycleFindings}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	wantRevision := DiscussionRevision([]Discussion{item})
	if len(triggers) != 3 || triggers[1] != "discussion requires reconsideration" || triggers[2] != triggers[1] {
		t.Fatalf("triggers = %v", triggers)
	}
	if revisions[1] != wantRevision || revisions[2] != wantRevision {
		t.Fatalf("revisions = %v, want routed retries to preserve %q", revisions, wantRevision)
	}
	if len(cycleDiscussion[1]) != 1 || len(cycleDiscussion[2]) != 1 || cycleDiscussion[2][0].ID != item.ID {
		t.Fatalf("cycle discussion = %#v", cycleDiscussion)
	}
}

func TestTerminalDiscussionRetryIncludesNewDiscussionEvidence(t *testing.T) {
	first := discussion("issue_comment", 1, "v1", "reviewer", "The nil case changes the conclusion.")
	updated := discussion("issue_comment", 1, "v2", "reviewer", "The nil and empty cases change the conclusion.")
	newItem := discussion("review_comment", 2, "v1", "reviewer", "The error path also needs reconsideration.")
	h := newHarness(t)
	h.routes = []RoutingDecision{RoutingReviewRequired}
	deps := h.deps()
	attempts := 0
	stateCalls := 0
	deps.State = func(context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 1 {
			return open("aaa"), nil
		}
		if attempts >= 2 {
			return discussed("aaa", updated, newItem), nil
		}
		return discussed("aaa", first), nil
	}
	var cycleDiscussion [][]Discussion
	var revisions []string
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		cycleDiscussion = append(cycleDiscussion, append([]Discussion(nil), discussion...))
		revisions = append(revisions, revision)
		switch attempts {
		case 1:
			return Cycle{Result: CycleFindings}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if fmt.Sprint(h.triggers) != "[initial review discussion requires reconsideration discussion requires reconsideration]" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if len(cycleDiscussion[2]) != 2 || cycleDiscussion[2][0].Revision != updated.Revision || cycleDiscussion[2][1].ID != newItem.ID {
		t.Fatalf("retry discussion = %#v", cycleDiscussion[2])
	}
	wantRevision := DiscussionRevision([]Discussion{updated, newItem})
	if revisions[2] != wantRevision {
		t.Fatalf("retry revision = %q, want %q", revisions[2], wantRevision)
	}
}

func TestDiscussionRetryEvidenceIncludesSavedAndCurrentItems(t *testing.T) {
	saved := discussion("issue_comment", 1, "v1", "reviewer", "The nil case changes the conclusion.")
	current := discussion("review_comment", 2, "v1", "reviewer", "The error path also needs reconsideration.")
	h := newHarness(t)
	h.routes = []RoutingDecision{RoutingReviewRequired}
	deps := h.deps()
	attempts := 0
	stateCalls := 0
	deps.State = func(context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 1 {
			return open("aaa"), nil
		}
		if attempts >= 2 {
			return discussed("aaa", current), nil
		}
		return discussed("aaa", saved), nil
	}
	var cycleDiscussion [][]Discussion
	var revisions []string
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		cycleDiscussion = append(cycleDiscussion, append([]Discussion(nil), discussion...))
		revisions = append(revisions, revision)
		switch attempts {
		case 1:
			return Cycle{Result: CycleFindings}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(cycleDiscussion[2]) != 2 || cycleDiscussion[2][0].ID != saved.ID || cycleDiscussion[2][1].ID != current.ID {
		t.Fatalf("retry discussion = %#v", cycleDiscussion[2])
	}
	wantRevision := DiscussionRevision([]Discussion{saved, current})
	if revisions[2] != wantRevision {
		t.Fatalf("retry revision = %q, want merged evidence %q", revisions[2], wantRevision)
	}
	if revisions[2] == DiscussionRevision([]Discussion{current}) {
		t.Fatalf("retry revision retained current-only evidence: %q", revisions[2])
	}
}

func TestNoReviewDiscussionConsumesWithoutReviewSlot(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Thanks.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleFindings}}
	h.routes = []RoutingDecision{RoutingNoReview}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = 12 * time.Minute

	if reason := Run(context.Background(), cfg, h.deps()); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 1 {
		t.Fatalf("triggers = %v, want only the initial review", h.triggers)
	}
	if len(h.routed) != 1 {
		t.Fatalf("routing calls = %d, want 1", len(h.routed))
	}
}

func TestUncertainPolicyWaitsVisiblyWithoutConsumption(t *testing.T) {
	item := discussion("review", 1, "v1", "reviewer", "Maybe.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleFindings}}
	h.routes = []RoutingDecision{RoutingUncertain}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = 13 * time.Minute

	if reason := Run(context.Background(), cfg, h.deps()); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 1 || len(h.routed) != 1 {
		t.Fatalf("triggers = %v routed = %d", h.triggers, len(h.routed))
	}
	if !hasEvent(h.events, EventDiscussionWaiting) {
		t.Fatalf("events = %#v", h.events)
	}
	if !containsLog(h.logs, "routing is uncertain") {
		t.Fatalf("logs = %v", h.logs)
	}
}

func TestUncertainPolicyCanEscalateToReview(t *testing.T) {
	item := discussion("review", 1, "v1", "reviewer", "Maybe.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleFindings}, {Result: CycleLGTMApproved}}
	h.routes = []RoutingDecision{RoutingUncertain}
	cfg := defaultConfig(PostModeApprove)
	cfg.UncertainPolicy = UncertainReview

	if reason := Run(context.Background(), cfg, h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "uncertain discussion requires reconsideration" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestHeadAndDiscussionChangesCoalesceIntoOneReview(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Please reconsider.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("bbb", item)}
	h.cycles = []Cycle{{Result: CycleFindings}, {Result: CycleLGTMApproved, HeadSHA: "bbb"}}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if len(h.routed) != 0 {
		t.Fatalf("routing calls = %d, want none because the head already required review", len(h.routed))
	}
	if len(h.cycleDiscussion[1]) != 1 {
		t.Fatalf("cycle discussion = %#v", h.cycleDiscussion)
	}
}

func TestAlreadyReviewedReturningHeadPreservesNewDiscussionForRouting(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "The returning head needs another look in this context.")
	h := newHarness(t)
	h.routes = []RoutingDecision{RoutingReviewRequired}
	deps := h.deps()
	attempts := 0
	deps.State = func(context.Context) (PRState, error) {
		switch attempts {
		case 0:
			return open("aaa"), nil
		case 1:
			return open("bbb"), nil
		default:
			return discussed("aaa", item), nil
		}
	}
	var cycleDiscussion [][]Discussion
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		cycleDiscussion = append(cycleDiscussion, append([]Discussion(nil), discussion...))
		switch attempts {
		case 1, 2:
			return Cycle{Result: CycleFindings}, nil
		case 3:
			return Cycle{Result: CycleAlreadyReviewed, HeadSHA: "aaa"}, nil
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if fmt.Sprint(h.triggers) != "[initial review commits settled commits settled discussion requires reconsideration]" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if len(h.routed) != 1 || len(h.routed[0]) != 1 || h.routed[0][0].ID != item.ID {
		t.Fatalf("routed discussion = %#v", h.routed)
	}
	if len(cycleDiscussion[3]) != 1 || cycleDiscussion[3][0].ID != item.ID {
		t.Fatalf("discussion review payload = %#v", cycleDiscussion[3])
	}
}

func TestAlreadyReviewedDiscussionEvidenceConsumesDiscussion(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Please reconsider this path.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleFindings}, {Result: CycleAlreadyReviewed, HeadSHA: "aaa"}}
	h.routes = []RoutingDecision{RoutingReviewRequired}
	cfg := defaultConfig(PostModeComment)
	cfg.MaxDuration = 15 * time.Minute

	if reason := Run(context.Background(), cfg, h.deps()); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.routed) != 1 || len(h.triggers) != 2 || h.triggers[1] != "discussion requires reconsideration" {
		t.Fatalf("routes = %#v triggers = %v", h.routed, h.triggers)
	}
}

func TestHeadWaitsForLaterDiscussionSettleDeadline(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Please reconsider.")
	h := newHarness(t)
	started := h.clock.Now()
	h.states = []PRState{
		open("aaa"),
		open("bbb"),
		discussed("bbb", item),
	}
	h.cycles = []Cycle{{Result: CycleFindings}, {Result: CycleLGTMApproved, HeadSHA: "bbb"}}
	deps := h.deps()
	runCycle := deps.RunCycle
	var secondReviewStarted time.Time
	deps.RunCycle = func(
		ctx context.Context,
		reviewNum int,
		trigger string,
		items []Discussion,
		revision string,
	) (Cycle, error) {
		if reviewNum == 2 {
			secondReviewStarted = h.clock.Now()
		}
		return runCycle(ctx, reviewNum, trigger, items, revision)
	}
	cfg := defaultConfig(PostModeApprove)
	cfg.SettleTime = 2 * time.Minute

	if reason := Run(context.Background(), cfg, deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if elapsed := secondReviewStarted.Sub(started); elapsed < 4*time.Minute {
		t.Fatalf("review started after %s, before discussion settled", elapsed)
	}
	if len(h.cycleDiscussion[1]) != 1 {
		t.Fatalf("cycle discussion = %#v", h.cycleDiscussion)
	}
}

func TestDiscussionArrivingDuringReviewIsNotLost(t *testing.T) {
	first := discussion("issue_comment", 1, "v1", "reviewer", "First correction")
	second := discussion("review_comment", 2, "v1", "author", "Second correction")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", first)}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}
	h.routes = []RoutingDecision{RoutingReviewRequired, RoutingReviewRequired}
	deps := h.deps()
	runCycle := deps.RunCycle
	deps.RunCycle = func(ctx context.Context, reviewNum int, trigger string, items []Discussion, revision string) (Cycle, error) {
		cycle, err := runCycle(ctx, reviewNum, trigger, items, revision)
		if reviewNum == 2 {
			h.states = append(h.states, discussed("aaa", first, second))
		}
		return cycle, err
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.routed) != 2 || len(h.routed[1]) != 1 || h.routed[1][0].ID != second.ID {
		t.Fatalf("routed = %#v", h.routed)
	}
}

func TestOwnRecordedOutputDoesNotHideSameAccountDiscussion(t *testing.T) {
	own := discussion("review", 10, "v1", "octocat", "ACR output")
	manual := discussion("issue_comment", 11, "v1", "octocat", "Manual technical correction")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", own, manual)}
	h.cycles = []Cycle{
		{Result: CycleFindings, OwnDiscussionIDs: []DiscussionID{own.ID}},
		{Result: CycleLGTMApproved},
	}
	h.routes = []RoutingDecision{RoutingReviewRequired}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.routed) != 1 || len(h.routed[0]) != 1 || h.routed[0][0].ID != manual.ID {
		t.Fatalf("routed = %#v", h.routed)
	}
}

func TestEditedDiscussionIsEligibleAgain(t *testing.T) {
	original := discussion("issue_comment", 1, "v1", "reviewer", "Thanks.")
	edited := discussion("issue_comment", 1, "v2", "reviewer", "Actually, the error path is wrong.")
	h := newHarness(t)
	h.states = []PRState{
		discussed("aaa", original),
		discussed("aaa", edited),
	}
	h.cycles = []Cycle{{Result: CycleFindings}, {Result: CycleLGTMApproved}}
	h.routes = []RoutingDecision{RoutingReviewRequired}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.routed) != 1 || h.routed[0][0].Revision != "v2" {
		t.Fatalf("routed = %#v", h.routed)
	}
}

func TestPendingApprovalWaitsForDiscussionRouting(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Thanks.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "LGTM"}}
	h.routes = []RoutingDecision{RoutingNoReview}
	h.ci = []bool{true}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.routed) != 1 || len(h.approvedWith) != 1 {
		t.Fatalf("routed = %d approvals = %v", len(h.routed), h.approvedWith)
	}
}

func TestManualRequestWakesAndReviewsFreshState(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("bbb"),
	}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved, HeadSHA: "bbb"},
	}
	deps := h.deps()
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{ManualRequests: 1}, nil
	}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), deps)

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "manual request" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if h.stateCalls != 2 {
		t.Fatalf("state captures = %d, want 2", h.stateCalls)
	}
	if !hasEvent(h.events, EventManualRequestReceived) || !hasEvent(h.events, EventManualReviewStarted) {
		t.Fatalf("events = %#v", h.events)
	}
}

func TestManualRequestsCoalesceBeforeReviewStarts(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}
	deps := h.deps()
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{ManualRequests: 4}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 {
		t.Fatalf("cycles = %d, want initial plus one manual review", len(h.triggers))
	}
	var coalesced Event
	for _, event := range h.events {
		if event.Type == EventManualRequestCoalesced {
			coalesced = event
		}
	}
	if coalesced.RequestCount != 4 {
		t.Fatalf("coalesced event = %#v", coalesced)
	}
}

func TestManualRequestsCoalesceThroughPreReviewHandoff(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}
	deps := h.deps()
	state := deps.State
	runCycle := deps.RunCycle
	stateCalls := 0
	var order []string
	deps.State = func(ctx context.Context) (PRState, error) {
		stateCalls++
		result, err := state(ctx)
		if stateCalls == 2 {
			order = append(order, "state")
		}
		return result, err
	}
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{
			ManualRequests: 1,
			Finalize: func(_ context.Context, stateReady <-chan struct{}) (WaitResult, error) {
				<-stateReady
				order = append(order, "finalize")
				return WaitResult{ManualRequests: 2}, nil
			},
		}, nil
	}
	deps.RunCycle = func(ctx context.Context, reviewNum int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		if trigger == "manual request" {
			order = append(order, "cycle")
		}
		return runCycle(ctx, reviewNum, trigger, discussion, revision)
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if strings.Join(order, ",") != "state,finalize,cycle" {
		t.Fatalf("order = %v", order)
	}
	var started Event
	for _, event := range h.events {
		if event.Type == EventManualReviewStarted {
			started = event
		}
	}
	if started.RequestCount != 3 {
		t.Fatalf("manual review event = %#v", started)
	}
}

func TestManualRequestRejectedWhenReviewBudgetIsExhausted(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "LGTM body"}}
	h.ci = []bool{true}
	cfg := defaultConfig(PostModeApprove)
	cfg.MaxReviews = 1
	waits := 0
	deps := h.deps()
	deps.Wait = func(ctx context.Context, duration time.Duration) (WaitResult, error) {
		waits++
		if waits == 1 {
			return WaitResult{ManualRequests: 1}, nil
		}
		h.clock.now = h.clock.now.Add(duration)
		return WaitResult{}, nil
	}

	if reason := Run(context.Background(), cfg, deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 1 {
		t.Fatalf("cycles = %d, want no review beyond the initial review", len(h.triggers))
	}
	var rejected Event
	for _, event := range h.events {
		if event.Type == EventManualRequestRejected {
			rejected = event
		}
	}
	if rejected.Reason != ReasonMaxReviews.String() {
		t.Fatalf("rejected event = %#v", rejected)
	}
}

func TestManualInputSafetyFailureStopsWatcher(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	deps := h.deps()
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{}, errors.New("terminal restore failed")
	}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), deps); reason != ReasonError {
		t.Fatalf("reason = %v, want ReasonError", reason)
	}
	if !hasEvent(h.events, EventManualInputUnsafe) {
		t.Fatalf("events = %#v", h.events)
	}
}

func TestManualInputHandoffFailureStopsWatcher(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	deps := h.deps()
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{
			ManualRequests: 1,
			Finalize: func(context.Context, <-chan struct{}) (WaitResult, error) {
				return WaitResult{}, errors.New("terminal restore failed")
			},
		}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), deps); reason != ReasonError {
		t.Fatalf("reason = %v, want ReasonError", reason)
	}
	if !hasEvent(h.events, EventManualInputUnsafe) {
		t.Fatalf("events = %#v", h.events)
	}
	if len(h.triggers) != 1 {
		t.Fatalf("cycles = %d, want only the initial review", len(h.triggers))
	}
}

func TestManualInputInterruptCancelsStateFetch(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	deps := h.deps()
	state := deps.State
	stateCalls := 0
	stateStarted := make(chan struct{})
	stateCanceled := make(chan struct{})
	deps.State = func(ctx context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 1 {
			return state(ctx)
		}
		close(stateStarted)
		<-ctx.Done()
		close(stateCanceled)
		return PRState{}, ctx.Err()
	}
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		return WaitResult{
			ManualRequests: 1,
			Finalize: func(context.Context, <-chan struct{}) (WaitResult, error) {
				<-stateStarted
				return WaitResult{Interrupted: true}, nil
			},
		}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), deps); reason != ReasonInterrupted {
		t.Fatalf("reason = %v, want ReasonInterrupted", reason)
	}
	select {
	case <-stateCanceled:
	default:
		t.Fatal("state fetch was not canceled after manual input interrupted")
	}
}

func TestManualInputInterruptWinsAtDeadlineFinalization(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleFindings}}
	cfg := defaultConfig(PostModeComment)
	deps := h.deps()
	deps.Wait = func(_ context.Context, duration time.Duration) (WaitResult, error) {
		h.clock.now = h.clock.now.Add(duration)
		return WaitResult{
			ManualRequests: 1,
			Finalize: func(_ context.Context, stateReady <-chan struct{}) (WaitResult, error) {
				<-stateReady
				return WaitResult{Interrupted: true}, nil
			},
		}, nil
	}

	if reason := Run(context.Background(), cfg, deps); reason != ReasonInterrupted {
		t.Fatalf("reason = %v, want ReasonInterrupted", reason)
	}
}

func TestManualRequestSurvivesTransientStateFailureWithRetryDelay(t *testing.T) {
	h := newHarness(t)
	h.cycles = []Cycle{
		{Result: CycleFindings},
		{Result: CycleLGTMApproved},
	}
	stateCalls := 0
	waitCalls := 0
	deps := h.deps()
	deps.State = func(context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 2 {
			return PRState{}, errors.New("transient gh failure")
		}
		return open("aaa"), nil
	}
	deps.Wait = func(context.Context, time.Duration) (WaitResult, error) {
		waitCalls++
		if waitCalls == 1 {
			return WaitResult{ManualRequests: 1}, nil
		}
		return WaitResult{}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if waitCalls != 2 {
		t.Fatalf("wait calls = %d, want 2", waitCalls)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "manual request" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestManualRequestConsumesConcurrentReReviewRequest(t *testing.T) {
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
	waitCalls := 0
	deps := h.deps()
	deps.Wait = func(ctx context.Context, duration time.Duration) (WaitResult, error) {
		waitCalls++
		if waitCalls == 1 {
			return WaitResult{ManualRequests: 1}, nil
		}
		h.clock.now = h.clock.now.Add(duration)
		return WaitResult{}, nil
	}

	if reason := Run(context.Background(), cfg, deps); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.triggers) != 2 {
		t.Fatalf("triggers = %v, want initial plus one manual review", h.triggers)
	}
	if h.triggers[1] != "manual request" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func hasEvent(events []Event, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func containsLog(logs []string, fragment string) bool {
	for _, log := range logs {
		if strings.Contains(log, fragment) {
			return true
		}
	}
	return false
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

func TestRetryableCycleFailureDoesNotConsumeReviewBudget(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	deps := h.deps()
	var reviewNumbers []int
	deps.RunCycle = func(_ context.Context, reviewNum int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		reviewNumbers = append(reviewNumbers, reviewNum)
		h.triggers = append(h.triggers, trigger)
		if len(reviewNumbers) == 1 {
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		}
		return Cycle{Result: CycleLGTMApproved}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(reviewNumbers) != 2 || reviewNumbers[0] != 1 || reviewNumbers[1] != 1 {
		t.Fatalf("review numbers = %v, want [1 1]", reviewNumbers)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "initial review" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestRetryableCycleFailuresBecomeFatalAfterLimit(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	deps := h.deps()
	attempts := 0
	deps.RunCycle = func(_ context.Context, _ int, _ string, _ []Discussion, _ string) (Cycle, error) {
		attempts++
		return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
	}

	if reason := Run(context.Background(), defaultConfig(PostModeComment), deps); reason != ReasonError {
		t.Fatalf("reason = %v, want ReasonError", reason)
	}
	if attempts != maxConsecutivePollErrors {
		t.Fatalf("attempts = %d, want %d", attempts, maxConsecutivePollErrors)
	}
	var cycleLogs []string
	for _, entry := range h.logs {
		if strings.Contains(entry, "Review cycle failed") {
			cycleLogs = append(cycleLogs, entry)
		}
	}
	if len(cycleLogs) != maxConsecutivePollErrors {
		t.Fatalf("cycle logs = %v", cycleLogs)
	}
	for _, entry := range cycleLogs[:len(cycleLogs)-1] {
		if !strings.Contains(entry, "will retry") {
			t.Fatalf("retrying log = %q", entry)
		}
	}
	finalLog := cycleLogs[len(cycleLogs)-1]
	if !strings.Contains(finalLog, "stopping") || strings.Contains(finalLog, "will retry") {
		t.Fatalf("terminal log = %q", finalLog)
	}
}

func TestRetryableRequestedReviewCannotPostPriorPendingApproval(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa"), requested("aaa")}
	h.ci = []bool{true}
	deps := h.deps()
	attempts := 0
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		switch attempts {
		case 1:
			return Cycle{Result: CycleLGTMCommentCIPending, LGTMBody: "obsolete"}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.approvedWith) != 0 {
		t.Fatalf("obsolete approval posted: %v", h.approvedWith)
	}
	if len(h.triggers) != 3 || h.triggers[1] != "re-review requested" || h.triggers[2] != "re-review requested" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestRetryablePreparationFailureSettlesChangedHead(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa"), open("bbb")}
	deps := h.deps()
	attempts := 0
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		if attempts == 1 {
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		}
		return Cycle{Result: CycleLGTMApproved}, nil
	}
	startedAt := h.clock.Now()

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 || h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	if elapsed := h.clock.Now().Sub(startedAt); elapsed < defaultConfig(PostModeApprove).SettleTime {
		t.Fatalf("changed head settled for %s", elapsed)
	}
}

func TestManualRequestSupersedesSettledCommitRetry(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa"), open("bbb")}
	deps := h.deps()
	attempts := 0
	manualSent := false
	deps.RunCycle = func(_ context.Context, _ int, trigger string, _ []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		switch attempts {
		case 1:
			return Cycle{Result: CycleFindings}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}
	deps.Wait = func(ctx context.Context, duration time.Duration) (WaitResult, error) {
		if attempts == 2 && !manualSent {
			manualSent = true
			return WaitResult{ManualRequests: 1}, nil
		}
		return WaitResult{}, h.clock.Sleep(ctx, duration)
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if fmt.Sprint(h.triggers) != "[initial review commits settled manual request]" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	var started []Event
	for _, event := range h.events {
		if event.Type == EventManualReviewStarted {
			started = append(started, event)
		}
	}
	if len(started) != 1 || started[0].RequestCount != 1 || started[0].ReviewNumber != 2 {
		t.Fatalf("manual review events = %#v", started)
	}
}

func TestNewManualRequestSupersedesManualRetry(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	deps := h.deps()
	attempts := 0
	manualRequests := 0
	deps.RunCycle = func(_ context.Context, _ int, trigger string, _ []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		switch attempts {
		case 1:
			return Cycle{Result: CycleFindings}, nil
		case 2:
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		default:
			return Cycle{Result: CycleLGTMApproved}, nil
		}
	}
	deps.Wait = func(ctx context.Context, duration time.Duration) (WaitResult, error) {
		if attempts == 1 && manualRequests == 0 {
			manualRequests++
			return WaitResult{ManualRequests: 1}, nil
		}
		if attempts == 2 && manualRequests == 1 {
			manualRequests++
			return WaitResult{ManualRequests: 1}, nil
		}
		return WaitResult{}, h.clock.Sleep(ctx, duration)
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if fmt.Sprint(h.triggers) != "[initial review manual request manual request]" {
		t.Fatalf("triggers = %v", h.triggers)
	}
	var started []Event
	for _, event := range h.events {
		if event.Type == EventManualReviewStarted {
			started = append(started, event)
		}
	}
	if len(started) != 2 || started[0].RequestCount != 1 || started[1].RequestCount != 1 {
		t.Fatalf("manual review events = %#v", started)
	}
}

func TestRetryablePreparationFailuresResetWhenHeadChanges(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa"), open("bbb")}
	deps := h.deps()
	attempts := 0
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		if attempts <= maxConsecutivePollErrors {
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		}
		return Cycle{Result: CycleLGTMApproved}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if attempts != maxConsecutivePollErrors+1 {
		t.Fatalf("attempts = %d, want %d", attempts, maxConsecutivePollErrors+1)
	}
	if h.triggers[1] != "commits settled" {
		t.Fatalf("triggers = %v", h.triggers)
	}
}

func TestRetryablePreparationFailuresResetForRequestedNewHead(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{
		open("aaa"),
		open("aaa"),
		open("aaa"),
		open("aaa"),
		requested("bbb"),
	}
	deps := h.deps()
	attempts := 0
	deps.RunCycle = func(_ context.Context, _ int, trigger string, discussion []Discussion, _ string) (Cycle, error) {
		attempts++
		h.triggers = append(h.triggers, trigger)
		if attempts <= 2*(maxConsecutivePollErrors-1) {
			return Cycle{Result: CycleError}, fmt.Errorf("%w: network unavailable", ErrRetryableCycle)
		}
		return Cycle{Result: CycleLGTMApproved}, nil
	}

	if reason := Run(context.Background(), defaultConfig(PostModeApprove), deps); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if attempts != 2*maxConsecutivePollErrors-1 {
		t.Fatalf("attempts = %d, want %d", attempts, 2*maxConsecutivePollErrors-1)
	}
	if h.triggers[maxConsecutivePollErrors-1] != "re-review requested" {
		t.Fatalf("triggers = %v", h.triggers)
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

func TestExhaustedBudgetDoesNotRerouteUnchangedDiscussion(t *testing.T) {
	item := discussion("issue_comment", 1, "v1", "reviewer", "Please reconsider.")
	h := newHarness(t)
	h.states = []PRState{open("aaa"), discussed("aaa", item)}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "promised"}}
	h.routes = []RoutingDecision{RoutingReviewRequired}
	h.ci = []bool{false}
	cfg := defaultConfig(PostModeApprove)
	cfg.MaxReviews = 1
	cfg.SettleTime = time.Minute
	cfg.MaxDuration = 5 * time.Minute

	if reason := Run(context.Background(), cfg, h.deps()); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want ReasonMaxDuration", reason)
	}
	if len(h.routed) != 1 {
		t.Fatalf("routing calls = %d, want one for unchanged discussion", len(h.routed))
	}
	if len(h.approvedWith) != 0 {
		t.Fatalf("approvals = %v, want none", h.approvedWith)
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
	deps.RunCycle = func(ctx context.Context, n int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		_, sawDeadline = ctx.Deadline()
		return inner(ctx, n, trigger, discussion, revision)
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
	deps.RunCycle = func(ctx context.Context, n int, trigger string, discussion []Discussion, revision string) (Cycle, error) {
		h.clock.now = h.clock.now.Add(time.Hour)
		return inner(ctx, n, trigger, discussion, revision)
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

func TestForcePushBackToDiscardedHeadRetriggersReview(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{
		{Result: CycleStaleHead, HeadSHA: "aaa"},
		{Result: CycleLGTMApproved},
	}

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonLGTM {
		t.Fatalf("reason = %v, want ReasonLGTM", reason)
	}
	if len(h.triggers) != 2 {
		t.Errorf("cycles = %d, want 2 (discarded head must not count as reviewed)", len(h.triggers))
	}
}

func TestPersistentCIErrorsDuringApprovalWaitAreFatal(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "x"}}
	h.ci = nil

	reason := Run(context.Background(), defaultConfig(PostModeApprove), h.deps())

	if reason != ReasonError {
		t.Fatalf("reason = %v, want ReasonError after repeated CI-check failures", reason)
	}
	if len(h.approvedWith) != 0 {
		t.Errorf("no approval should post: %v", h.approvedWith)
	}
}
