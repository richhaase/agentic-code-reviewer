package watch

import (
	"context"
	"time"
)

type Polling struct {
	State func(ctx context.Context) (PRState, error)
	Wait  func(ctx context.Context, duration time.Duration) (WaitResult, error)
	Clock Clock
}

type ReviewExecution struct {
	RunCycle func(ctx context.Context, reviewNum int, trigger string, discussion []Discussion, discussionRevision string) (Cycle, error)
}

type ActionPolicies struct {
	RouteDiscussion func(ctx context.Context, discussion []Discussion) (RoutingDecision, error)
	CIGreen         func(ctx context.Context) (bool, error)
	Approve         func(ctx context.Context, body string) error
	Control         func(ctx context.Context, state PRState) (ControlDecision, error)
}

type Presentation struct {
	Emit func(event Event)
	Logf func(format string, args ...any)
}

type Lifecycle struct {
	cfg  Config
	deps Deps
}

func NewLifecycle(
	cfg Config,
	polling Polling,
	review ReviewExecution,
	actions ActionPolicies,
	presentation Presentation,
) *Lifecycle {
	return newLifecycleFromDeps(cfg, Deps{
		State:           polling.State,
		Wait:            polling.Wait,
		Clock:           polling.Clock,
		RunCycle:        review.RunCycle,
		RouteDiscussion: actions.RouteDiscussion,
		CIGreen:         actions.CIGreen,
		Approve:         actions.Approve,
		Control:         actions.Control,
		Emit:            presentation.Emit,
		Logf:            presentation.Logf,
	})
}

func newLifecycleFromDeps(cfg Config, deps Deps) *Lifecycle {
	return &Lifecycle{cfg: cfg, deps: deps}
}

func (l *Lifecycle) Run(ctx context.Context) ExitReason {
	return run(ctx, l.cfg, l.deps)
}
