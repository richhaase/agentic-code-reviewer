package watch

import (
	"context"
	"testing"
)

type lifecycleFixture struct {
	name         string
	states       []PRState
	cycles       []Cycle
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
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			h := newHarness(t)
			h.states = fixture.states
			h.cycles = fixture.cycles
			deps := h.deps()
			lifecycle := NewLifecycle(
				defaultConfig(PostModeApprove),
				Polling{State: deps.State, Wait: deps.Wait, Clock: deps.Clock},
				ReviewExecution{RunCycle: deps.RunCycle},
				ActionPolicies{
					RouteDiscussion: deps.RouteDiscussion,
					CIGreen:         deps.CIGreen,
					Approve:         deps.Approve,
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
