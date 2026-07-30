package watch

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

type routingAgent struct {
	output string
	input  []byte
}

func (a *routingAgent) Name() string { return "routing" }

func (a *routingAgent) IsAvailable() error { return nil }

func (a *routingAgent) ExecuteReview(context.Context, *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	panic("unexpected review execution")
}

func (a *routingAgent) ExecuteSummary(_ context.Context, cfg *agent.SummaryConfig) (*agent.ExecutionResult, error) {
	a.input = append([]byte(nil), cfg.Input...)
	return agent.NewExecutionResult(io.NopCloser(strings.NewReader(a.output)), func() int { return 0 }, nil), nil
}

func TestRouterReturnsStrictDecisionAndIncludesDiscussion(t *testing.T) {
	fake := &routingAgent{output: "review_required\n"}
	router := &Router{agent: fake, workDir: t.TempDir()}
	items := []Discussion{discussion("issue_comment", 7, "v1", "reviewer", "This changes the error contract.")}

	decision, err := router.Route(context.Background(), items)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if decision != RoutingReviewRequired {
		t.Fatalf("decision = %q", decision)
	}
	if !strings.Contains(string(fake.input), "changes the error contract") {
		t.Fatalf("router input = %s", fake.input)
	}
}

func TestParseRoutingDecisionRejectsProse(t *testing.T) {
	if decision, err := ParseRoutingDecision("I think no_review"); err == nil || decision != RoutingUncertain {
		t.Fatalf("decision = %q, err = %v", decision, err)
	}
}

func TestParseRoutingDecisionAcceptsCodeFence(t *testing.T) {
	decision, err := ParseRoutingDecision("```\nno_review\n```")
	if err != nil || decision != RoutingNoReview {
		t.Fatalf("decision = %q, err = %v", decision, err)
	}
}
