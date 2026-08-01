package watch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type lifecycleFixture struct {
	name         string
	states       []PRState
	cycles       []Cycle
	controls     []ControlDecision
	wantReason   ExitReason
	wantTriggers []string
}

func TestReusableLifecycleTransitionFixtures(t *testing.T) {
	fixtures := []lifecycleFixture{
		{
			name:         "terminal initial review",
			states:       []PRState{open("aaa")},
			cycles:       []Cycle{{Result: CycleLGTMApproved}},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review"},
		},
		{
			name: "settled replacement head",
			states: []PRState{
				open("aaa"),
				open("bbb"),
				open("bbb"),
			},
			cycles: []Cycle{
				{Result: CycleFindings},
				{Result: CycleLGTMComment},
			},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review", "commits settled"},
		},
		{
			name: "stale result returns to eligibility",
			states: []PRState{
				open("aaa"),
				open("bbb"),
				open("bbb"),
			},
			cycles: []Cycle{
				{Result: CycleStaleHead},
				{Result: CycleLGTMComment},
			},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review", "commits settled"},
		},
		{
			name:         "merged pull request is not admitted",
			states:       []PRState{{HeadSHA: "aaa", Merged: true}},
			wantReason:   ReasonMerged,
			wantTriggers: nil,
		},
		{
			name:         "released pull request is not admitted",
			states:       []PRState{open("aaa")},
			controls:     []ControlDecision{{State: ControlReleased}},
			wantReason:   ReasonReleased,
			wantTriggers: nil,
		},
		{
			name:         "opted out pull request is not admitted",
			states:       []PRState{open("aaa")},
			controls:     []ControlDecision{{State: ControlOptedOut}},
			wantReason:   ReasonOptedOut,
			wantTriggers: nil,
		},
		{
			name: "explicit resume admits snoozed pull request",
			states: []PRState{
				open("aaa"),
				open("aaa"),
			},
			cycles: []Cycle{{Result: CycleLGTMApproved}},
			controls: []ControlDecision{
				{State: ControlSnoozed},
				{State: ControlActive},
			},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review"},
		},
		{
			name: "snooze expiry resumes eligibility",
			states: []PRState{
				open("aaa"),
				open("aaa"),
				open("aaa"),
			},
			cycles: []Cycle{{Result: CycleLGTMApproved}},
			controls: []ControlDecision{
				{State: ControlSnoozed, ResumeAt: time.Unix(1_700_000_000, 0).Add(2 * time.Minute)},
			},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review"},
		},
		{
			name: "release stops an active lifecycle",
			states: []PRState{
				open("aaa"),
				open("aaa"),
			},
			cycles: []Cycle{{Result: CycleFindings}},
			controls: []ControlDecision{
				{State: ControlActive},
				{State: ControlReleased},
			},
			wantReason:   ReasonReleased,
			wantTriggers: []string{"initial review"},
		},
		{
			name: "review request rearmed during snooze",
			states: []PRState{
				requested("aaa"),
				open("aaa"),
				requested("aaa"),
				requested("aaa"),
			},
			cycles: []Cycle{
				{Result: CycleFindings},
				{Result: CycleLGTMApproved},
			},
			controls: []ControlDecision{
				{State: ControlActive},
				{State: ControlSnoozed},
				{State: ControlSnoozed},
				{State: ControlActive},
			},
			wantReason:   ReasonLGTM,
			wantTriggers: []string{"initial review", "re-review requested"},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			h := newHarness(t)
			h.states = fixture.states
			h.cycles = fixture.cycles
			deps := h.deps()
			controlI := 0
			control := func(context.Context, PRState) (ControlDecision, error) {
				if controlI < len(fixture.controls)-1 {
					controlI++
					return fixture.controls[controlI-1], nil
				}
				return fixture.controls[len(fixture.controls)-1], nil
			}
			if len(fixture.controls) == 0 {
				control = nil
			}
			lifecycle := NewLifecycle(
				defaultConfig(PostModeApprove),
				Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
				ReviewExecution{RunCycle: deps.RunCycle},
				ActionPolicies{
					RouteDiscussion: deps.RouteDiscussion,
					CIGreen:         deps.CIGreen,
					Approve:         deps.Approve,
					Control:         control,
				},
				Presentation{Emit: deps.Emit, Logf: deps.Logf},
			)

			if reason := lifecycle.Run(context.Background()); reason != fixture.wantReason {
				t.Fatalf("reason = %v, want %v", reason, fixture.wantReason)
			}
			if len(h.triggers) != len(fixture.wantTriggers) {
				t.Fatalf("triggers = %v, want %v", h.triggers, fixture.wantTriggers)
			}
			for i := range fixture.wantTriggers {
				if h.triggers[i] != fixture.wantTriggers[i] {
					t.Fatalf("triggers = %v, want %v", h.triggers, fixture.wantTriggers)
				}
			}
		})
	}
}

func TestLifecyclePresentationDoesNotControlTransitions(t *testing.T) {
	build := func(presentation Presentation) (*Lifecycle, *harness) {
		h := newHarness(t)
		h.states = []PRState{open("aaa")}
		h.cycles = []Cycle{{Result: CycleLGTMApproved}}
		deps := h.deps()
		return NewLifecycle(
			defaultConfig(PostModeApprove),
			Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
			ReviewExecution{RunCycle: deps.RunCycle},
			ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve},
			presentation,
		), h
	}

	silent, silentHarness := build(Presentation{})
	visible, visibleHarness := build(Presentation{
		Emit: func(Event) {},
		Logf: func(string, ...any) {},
	})

	if reason := silent.Run(context.Background()); reason != ReasonLGTM {
		t.Fatalf("silent reason = %v, want %v", reason, ReasonLGTM)
	}
	if reason := visible.Run(context.Background()); reason != ReasonLGTM {
		t.Fatalf("visible reason = %v, want %v", reason, ReasonLGTM)
	}
	if len(silentHarness.triggers) != 1 || len(visibleHarness.triggers) != 1 ||
		silentHarness.triggers[0] != visibleHarness.triggers[0] {
		t.Fatalf("silent triggers = %v, visible triggers = %v", silentHarness.triggers, visibleHarness.triggers)
	}
}

func TestLifecycleAdmissionUsesLatestSuccessfulState(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa"), open("bbb")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}
	deps := h.deps()
	var controlledHeads []string
	controlCalls := 0
	control := func(_ context.Context, state PRState) (ControlDecision, error) {
		controlledHeads = append(controlledHeads, state.HeadSHA)
		controlCalls++
		if controlCalls == 1 {
			return ControlDecision{State: ControlSnoozed}, nil
		}
		return ControlDecision{State: ControlActive}, nil
	}
	lifecycle := NewLifecycle(
		defaultConfig(PostModeApprove),
		Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
		ReviewExecution{RunCycle: deps.RunCycle},
		ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve, Control: control},
		Presentation{Emit: deps.Emit, Logf: deps.Logf},
	)

	if reason := lifecycle.Run(context.Background()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want %v", reason, ReasonLGTM)
	}
	if len(controlledHeads) != 2 || controlledHeads[1] != "bbb" {
		t.Fatalf("controlled heads = %v, want [aaa bbb]", controlledHeads)
	}
}

func TestLifecycleAdmissionPreservesStateAfterFailedRefresh(t *testing.T) {
	h := newHarness(t)
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}
	deps := h.deps()
	stateCalls := 0
	deps.State = func(context.Context) (PRState, error) {
		stateCalls++
		if stateCalls == 1 {
			return open("aaa"), nil
		}
		return open("bbb"), errors.New("transient gh failure")
	}
	var controlledHeads []string
	controlCalls := 0
	control := func(_ context.Context, state PRState) (ControlDecision, error) {
		controlledHeads = append(controlledHeads, state.HeadSHA)
		controlCalls++
		if controlCalls == 1 {
			return ControlDecision{State: ControlSnoozed}, nil
		}
		return ControlDecision{State: ControlActive}, nil
	}
	lifecycle := NewLifecycle(
		defaultConfig(PostModeApprove),
		Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
		ReviewExecution{RunCycle: deps.RunCycle},
		ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve, Control: control},
		Presentation{Emit: deps.Emit, Logf: deps.Logf},
	)

	if reason := lifecycle.Run(context.Background()); reason != ReasonLGTM {
		t.Fatalf("reason = %v, want %v", reason, ReasonLGTM)
	}
	if len(controlledHeads) != 2 || controlledHeads[1] != "aaa" {
		t.Fatalf("controlled heads = %v, want [aaa aaa]", controlledHeads)
	}
}

func TestLifecycleSnoozeExpiryRetainsPollIntervalAfterStateError(t *testing.T) {
	h := newHarness(t)
	h.cycles = []Cycle{{Result: CycleFindings}}
	deps := h.deps()
	stateCalls := 0
	deps.State = func(context.Context) (PRState, error) {
		stateCalls++
		switch stateCalls {
		case 1, 2:
			return open("aaa"), nil
		case 3:
			return PRState{}, errors.New("transient gh failure")
		default:
			return PRState{HeadSHA: "aaa", Merged: true}, nil
		}
	}
	var waits []time.Duration
	deps.Wait = func(_ context.Context, duration time.Duration) (WaitResult, error) {
		waits = append(waits, duration)
		h.clock.now = h.clock.now.Add(duration)
		return WaitResult{}, nil
	}
	controlCalls := 0
	control := func(context.Context, PRState) (ControlDecision, error) {
		controlCalls++
		if controlCalls == 1 {
			return ControlDecision{State: ControlActive}, nil
		}
		return ControlDecision{State: ControlSnoozed, ResumeAt: h.clock.Now().Add(time.Minute)}, nil
	}
	lifecycle := NewLifecycle(
		defaultConfig(PostModeComment),
		Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
		ReviewExecution{RunCycle: deps.RunCycle},
		ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve, Control: control},
		Presentation{Emit: deps.Emit, Logf: deps.Logf},
	)

	if reason := lifecycle.Run(context.Background()); reason != ReasonMerged {
		t.Fatalf("reason = %v, want %v", reason, ReasonMerged)
	}
	if len(waits) != 3 || waits[2] != time.Minute {
		t.Fatalf("waits = %v, want three one-minute waits", waits)
	}
}

func TestLifecycleResumeAtDeadlineDoesNotAdmitReview(t *testing.T) {
	h := newHarness(t)
	h.states = []PRState{open("aaa")}
	h.cycles = []Cycle{{Result: CycleLGTMApproved}}
	deps := h.deps()
	cfg := defaultConfig(PostModeApprove)
	cfg.MaxDuration = time.Minute
	resumeAt := h.clock.Now().Add(cfg.MaxDuration)
	lifecycle := NewLifecycle(
		cfg,
		Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
		ReviewExecution{RunCycle: deps.RunCycle},
		ActionPolicies{
			CIGreen: deps.CIGreen,
			Approve: deps.Approve,
			Control: func(context.Context, PRState) (ControlDecision, error) {
				return ControlDecision{State: ControlSnoozed, ResumeAt: resumeAt}, nil
			},
		},
		Presentation{Emit: deps.Emit, Logf: deps.Logf},
	)

	if reason := lifecycle.Run(context.Background()); reason != ReasonMaxDuration {
		t.Fatalf("reason = %v, want %v", reason, ReasonMaxDuration)
	}
	if len(h.triggers) != 0 {
		t.Fatalf("triggers = %v, want none", h.triggers)
	}
	if h.stateCalls != 1 {
		t.Fatalf("state calls = %d, want only the initial state fetch", h.stateCalls)
	}
}

func TestLifecycleControlStopsDuringStatePollingFailures(t *testing.T) {
	tests := []struct {
		name       string
		control    ControlState
		wantReason ExitReason
	}{
		{name: "released", control: ControlReleased, wantReason: ReasonReleased},
		{name: "opted out", control: ControlOptedOut, wantReason: ReasonOptedOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.cycles = []Cycle{{Result: CycleFindings}}
			deps := h.deps()
			stateCalls := 0
			deps.State = func(context.Context) (PRState, error) {
				stateCalls++
				if stateCalls == 1 {
					return open("aaa"), nil
				}
				return PRState{}, errors.New("transient gh failure")
			}
			controlCalls := 0
			var controlledStates []PRState
			control := func(_ context.Context, state PRState) (ControlDecision, error) {
				controlledStates = append(controlledStates, state)
				controlCalls++
				if controlCalls == 1 {
					return ControlDecision{State: ControlActive}, nil
				}
				return ControlDecision{State: test.control}, nil
			}
			lifecycle := NewLifecycle(
				defaultConfig(PostModeComment),
				Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
				ReviewExecution{RunCycle: deps.RunCycle},
				ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve, Control: control},
				Presentation{Emit: deps.Emit, Logf: deps.Logf},
			)

			if reason := lifecycle.Run(context.Background()); reason != test.wantReason {
				t.Fatalf("reason = %v, want %v", reason, test.wantReason)
			}
			if stateCalls != 2 {
				t.Fatalf("state calls = %d, want initial success and one failure", stateCalls)
			}
			if len(controlledStates) != 2 || controlledStates[1].HeadSHA != "aaa" {
				t.Fatalf("controlled states = %#v, want last successful state", controlledStates)
			}
		})
	}
}

func TestLifecycleControlPreventsDeferredApproval(t *testing.T) {
	tests := []struct {
		name       string
		control    ControlState
		wantReason ExitReason
	}{
		{name: "snoozed", control: ControlSnoozed, wantReason: ReasonMerged},
		{name: "released", control: ControlReleased, wantReason: ReasonReleased},
		{name: "opted out", control: ControlOptedOut, wantReason: ReasonOptedOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.states = []PRState{
				open("aaa"),
				open("aaa"),
				open("aaa"),
				{HeadSHA: "aaa", Merged: true},
			}
			h.cycles = []Cycle{{Result: CycleLGTMCommentCIPending, LGTMBody: "LGTM"}}
			h.ci = []bool{true}
			deps := h.deps()
			controlCalls := 0
			control := func(context.Context, PRState) (ControlDecision, error) {
				controlCalls++
				if controlCalls < 3 {
					return ControlDecision{State: ControlActive}, nil
				}
				return ControlDecision{State: test.control}, nil
			}
			lifecycle := NewLifecycle(
				defaultConfig(PostModeApprove),
				Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
				ReviewExecution{RunCycle: deps.RunCycle},
				ActionPolicies{CIGreen: deps.CIGreen, Approve: deps.Approve, Control: control},
				Presentation{Emit: deps.Emit, Logf: deps.Logf},
			)

			if reason := lifecycle.Run(context.Background()); reason != test.wantReason {
				t.Fatalf("reason = %v, want %v", reason, test.wantReason)
			}
			if len(h.approvedWith) != 0 {
				t.Fatalf("approvals = %v, want none", h.approvedWith)
			}
			if controlCalls != 3 {
				t.Fatalf("control calls = %d, want final pre-approval check", controlCalls)
			}
		})
	}
}

func TestLifecycleControlPreventsRoutedReview(t *testing.T) {
	tests := []struct {
		name       string
		control    ControlState
		wantReason ExitReason
	}{
		{name: "snoozed", control: ControlSnoozed, wantReason: ReasonMerged},
		{name: "released", control: ControlReleased, wantReason: ReasonReleased},
		{name: "opted out", control: ControlOptedOut, wantReason: ReasonOptedOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := discussion("issue_comment", 1, "v1", "reviewer", "Please reconsider.")
			h := newHarness(t)
			h.states = []PRState{
				open("aaa"),
				discussed("aaa", item),
				{HeadSHA: "aaa", Merged: true},
			}
			h.cycles = []Cycle{{Result: CycleFindings}}
			h.routes = []RoutingDecision{RoutingReviewRequired}
			deps := h.deps()
			controlCalls := 0
			control := func(context.Context, PRState) (ControlDecision, error) {
				controlCalls++
				if controlCalls < 3 {
					return ControlDecision{State: ControlActive}, nil
				}
				return ControlDecision{State: test.control}, nil
			}
			cfg := defaultConfig(PostModeComment)
			cfg.SettleTime = 0
			lifecycle := NewLifecycle(
				cfg,
				Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
				ReviewExecution{RunCycle: deps.RunCycle},
				ActionPolicies{
					RouteDiscussion: deps.RouteDiscussion,
					CIGreen:         deps.CIGreen,
					Approve:         deps.Approve,
					Control:         control,
				},
				Presentation{Emit: deps.Emit, Logf: deps.Logf},
			)

			if reason := lifecycle.Run(context.Background()); reason != test.wantReason {
				t.Fatalf("reason = %v, want %v", reason, test.wantReason)
			}
			if len(h.triggers) != 1 {
				t.Fatalf("triggers = %v, want only initial review", h.triggers)
			}
			if len(h.routed) != 1 || controlCalls != 3 {
				t.Fatalf("routing calls = %d, control calls = %d", len(h.routed), controlCalls)
			}
		})
	}
}
