package watch

import (
	"context"
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
