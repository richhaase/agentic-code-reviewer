package watch

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/richhaase/agentic-code-reviewer/internal/agent"
)

type routingAgent struct {
	name   string
	output string
	input  []byte
}

func (a *routingAgent) Name() string { return a.name }

func (a *routingAgent) IsAvailable() error { return nil }

func (a *routingAgent) ExecuteReview(context.Context, *agent.ReviewConfig) (*agent.ExecutionResult, error) {
	panic("unexpected review execution")
}

func (a *routingAgent) ExecuteSummary(_ context.Context, cfg *agent.SummaryConfig) (*agent.ExecutionResult, error) {
	a.input = append([]byte(nil), cfg.Input...)
	return agent.NewExecutionResult(io.NopCloser(strings.NewReader(a.output)), func() int { return 0 }, nil), nil
}

func TestRouterReturnsStrictDecisionAndIncludesDiscussion(t *testing.T) {
	fake := &routingAgent{name: "agy", output: `{"decision":"review_required"}`}
	router := &Router{agent: fake, parser: agent.NewAntigravitySummaryParser(), workDir: t.TempDir()}
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
	if decision, err := ParseRoutingDecision(`{"decision":"no_review"} because it agrees`); err == nil || decision != RoutingUncertain {
		t.Fatalf("decision = %q, err = %v", decision, err)
	}
}

func TestParseRoutingDecisionAcceptsCodeFence(t *testing.T) {
	decision, err := ParseRoutingDecision("```json\n{\"decision\":\"no_review\"}\n```")
	if err != nil || decision != RoutingNoReview {
		t.Fatalf("decision = %q, err = %v", decision, err)
	}
}

func TestRouterDecodesAgentSummaryEnvelopes(t *testing.T) {
	tests := []struct {
		name   string
		output string
		parser agent.SummaryParser
	}{
		{
			name:   "codex",
			output: "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{\\\"decision\\\":\\\"review_required\\\"}\"}}\n",
			parser: agent.NewCodexSummaryParser(),
		},
		{
			name:   "claude",
			output: `{"result":"{\"decision\":\"review_required\"}"}`,
			parser: agent.NewClaudeSummaryParser(),
		},
		{
			name:   "gemini",
			output: `{"response":"{\"decision\":\"review_required\"}"}`,
			parser: agent.NewGeminiSummaryParser(),
		},
		{
			name:   "agy",
			output: `{"decision":"review_required"}`,
			parser: agent.NewAntigravitySummaryParser(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &routingAgent{name: tt.name, output: tt.output}
			router := &Router{agent: fake, parser: tt.parser, workDir: t.TempDir()}

			decision, err := router.Route(context.Background(), []Discussion{discussion("issue_comment", 7, "v1", "reviewer", "Please reconsider.")})
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if decision != RoutingReviewRequired {
				t.Fatalf("decision = %q", decision)
			}
		})
	}
}
